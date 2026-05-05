package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type contextKey string

const levelContextKey contextKey = "agent_level"

func LevelFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(levelContextKey).(int); ok {
		return v
	}
	return 0
}

type InterruptionError struct{}

func (e *InterruptionError) Error() string {
	return "agent interrupted by user"
}

type Agent struct {
	Config          *config.AgentConfig
	LLMConfig       *config.LLMConfig
	LLM             *llm.Client
	Registry        *ToolRegistry
	Writer          io.Writer
	Filter          *protocol.OutputFilter
	Timeouts        *config.TimeoutConfig
	AgentsMdContent string

	History []llm.Message
	IsMain  bool

	// OnPauseCheck is called between LLM iterations to check for pause.
	// If nil, no pause check is performed.
	OnPauseCheck func(ctx context.Context) error
}

type TokenUsage struct {
	AgentName        string
	PromptTokens     int64
	CompletionTokens int64
	Children         []*TokenUsage
}

type RunOptions struct {
	UserMessage string
	History     []llm.Message
	Level       int
}

type RunResult struct {
	Response string
	Duration time.Duration
	Usage    *TokenUsage
}

func (a *Agent) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	start := time.Now()

	ctx = context.WithValue(ctx, levelContextKey, opts.Level)

	systemPrompt := InterpolatePrompt(a.Config.SystemPrompt)
	if a.AgentsMdContent != "" {
		systemPrompt += "\n\n# AGENTS.md\n\n" + a.AgentsMdContent
	}

	messages := make([]llm.Message, 0, len(opts.History)+2)
	messages = append(messages, llm.Message{
		Role:    "system",
		Content: systemPrompt,
	})
	messages = append(messages, opts.History...)
	if opts.UserMessage != "" {
		messages = append(messages, llm.Message{
			Role:    "user",
			Content: opts.UserMessage,
		})
	}

	if a.Filter.Enabled(protocol.BlockSystemPrompt) {
		protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockSystemPrompt, systemPrompt)
	}
	if opts.UserMessage != "" && a.Filter.Enabled(protocol.BlockUserMessage) {
		protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockUserMessage, opts.UserMessage)
	}

	toolDefs := a.Registry.ToolDefinitions()

	if len(toolDefs) > 0 && a.Filter.Enabled(protocol.BlockToolsDefinition) {
		protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsDefinition, FormatToolDefinitions(toolDefs))
	}

	if len(opts.History) == 0 && opts.UserMessage != "" && len(a.Config.InitialToolCalls) > 0 {
		var toolCalls []llm.ToolCall
		var toolResultMsgs []llm.Message

		type initialCallResult struct {
			index   int
			content string
		}
		initialResults := make([]initialCallResult, len(a.Config.InitialToolCalls))
		var wg sync.WaitGroup

		for i, itc := range a.Config.InitialToolCalls {
			expandedArgs := ReplacePlaceholdersInArgs(itc.Arguments, opts.UserMessage, "")
			argsJSON, _ := json.Marshal(expandedArgs)
			argsStr := string(argsJSON)
			callID := fmt.Sprintf("initial_%d", i)

			tc := llm.ToolCall{
				ID:   callID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      itc.Tool,
					Arguments: argsStr,
				},
			}
			toolCalls = append(toolCalls, tc)

			if a.Filter.Enabled(protocol.BlockToolsInput) {
				protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsInput, FormatToolInput(itc.Tool, argsStr))
			}

			wg.Add(1)
			go func(idx int, toolName, arguments string) {
				defer wg.Done()
				result, err := a.Registry.CallTool(ctx, toolName, arguments)
				if err != nil {
					result = ToolResult{Content: fmt.Sprintf("tool error: %v", err), IsError: true}
				}
				content := result.Content
				if content == "" {
					content = "(no output)"
				}
				initialResults[idx] = initialCallResult{index: idx, content: content}
			}(i, itc.Tool, argsStr)
		}
		wg.Wait()

		for i, res := range initialResults {
			toolResultMsgs = append(toolResultMsgs, llm.Message{
				Role:       "tool",
				Content:    res.content,
				ToolCallID: fmt.Sprintf("initial_%d", i),
			})

			if a.Filter.Enabled(protocol.BlockToolsOutput) {
				protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsOutput, FormatToolOutput(a.Config.InitialToolCalls[i].Tool, res.content))
			}
		}

		messages = append(messages, llm.Message{
			Role:      "assistant",
			ToolCalls: toolCalls,
		})
		messages = append(messages, toolResultMsgs...)
	}

	var fullResponse strings.Builder
	usage := &TokenUsage{AgentName: a.Config.Name}

	for {
		llmTimeout := a.Timeouts.LLMRequestDuration()
		reqCtx, cancel := context.WithTimeout(ctx, llmTimeout)

		var textBuf strings.Builder
		var thinkingBuf strings.Builder
		var toolCalls []llm.ToolCallDelta
		var thinkingHeaderWritten bool
		var callHasUsage bool

		err := a.LLM.ChatCompletionStream(reqCtx, messages, toolDefs, func(e llm.StreamEvent) {
			switch e.Type {
			case llm.EventText:
				textBuf.WriteString(e.DeltaContent)
			case llm.EventThinking:
				thinkingBuf.WriteString(e.DeltaReasoningContent)
				if a.Filter.Enabled(protocol.BlockThinking) {
					if !thinkingHeaderWritten {
						protocol.WriteHeader(a.Writer, a.Config.Name, opts.Level, protocol.BlockThinking)
						thinkingHeaderWritten = true
					}
					io.WriteString(a.Writer, e.DeltaReasoningContent)
				}
			case llm.EventToolCall:
				toolCalls = e.ToolCalls
			case llm.EventUsage:
				if !callHasUsage {
					if e.UsagePromptTokens > 0 {
						usage.PromptTokens = e.UsagePromptTokens
					}
					if e.UsageCompletionTokens > 0 {
						usage.CompletionTokens += e.UsageCompletionTokens
					}
				}
			case llm.EventDone:
				if e.UsagePromptTokens > 0 || e.UsageCompletionTokens > 0 {
					callHasUsage = true
					if e.UsagePromptTokens > 0 {
						usage.PromptTokens = e.UsagePromptTokens
					}
					if e.UsageCompletionTokens > 0 {
						usage.CompletionTokens += e.UsageCompletionTokens
					}
				}
				if a.Filter.Enabled(protocol.BlockTokenStats) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockTokenStats, usage.String())
				}
			}
		})
		cancel()

		if err != nil {
			if ctx.Err() != nil && !a.IsMain {
				return nil, &InterruptionError{}
			}
			if ctx.Err() != nil && a.IsMain {
				return nil, &InterruptionError{}
			}
			return nil, fmt.Errorf("LLM request failed: %w", err)
		}

		// Pause check: wait if paused
		if a.OnPauseCheck != nil {
			if err := a.OnPauseCheck(ctx); err != nil {
				return nil, &InterruptionError{}
			}
		}

		if thinkingBuf.Len() > 0 && a.Filter.Enabled(protocol.BlockThinking) {
			io.WriteString(a.Writer, "\n")
			thinkingHeaderWritten = false
		}

		if len(toolCalls) > 0 {
			assistantMsg := llm.Message{
				Role:      "assistant",
				Content:   textBuf.String(),
				ToolCalls: convertDeltasToToolCalls(toolCalls),
			}
			messages = append(messages, assistantMsg)

			for i := range toolCalls {
				tc := toolCalls[i]
				if a.Filter.Enabled(protocol.BlockToolsInput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsInput, FormatToolInput(tc.Function.Name, tc.Function.Arguments))
				}
			}

			type callResult struct {
				content string
				usage   *TokenUsage
			}
			callResults := make([]callResult, len(toolCalls))
			var wg sync.WaitGroup
			for i := range toolCalls {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					tc := toolCalls[idx]
					result, err := a.Registry.CallTool(ctx, tc.Function.Name, tc.Function.Arguments)
					if err != nil {
						result = ToolResult{
							Content: fmt.Sprintf("tool error: %v", err),
							IsError: true,
						}
					}
					content := result.Content
					if content == "" {
						content = "(no output)"
					}
					callResults[idx] = callResult{content: content, usage: result.Usage}
				}(i)
			}
			wg.Wait()

			for i, tc := range toolCalls {
				res := callResults[i]
				messages = append(messages, llm.Message{
					Role:       "tool",
					Content:    res.content,
					ToolCallID: tc.ID,
				})

				if a.Filter.Enabled(protocol.BlockToolsOutput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsOutput, FormatToolOutput(tc.Function.Name, res.content))
				}

				if res.usage != nil {
					usage.Children = append(usage.Children, res.usage)
				}
			}

			if a.Filter.Enabled(protocol.BlockTokenStats) {
				protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockTokenStats, usage.String())
			}

			continue
		}

		response := textBuf.String()
		if response == "" && thinkingBuf.Len() > 0 {
			response = thinkingBuf.String()
		}
		if response == "" {
			response = "(no response)"
		}
		fullResponse.WriteString(response)

		if a.Filter.Enabled(protocol.BlockAgentResponse) {
			protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockAgentResponse, response)
		}

		if a.Filter.Enabled(protocol.BlockAgentResponse) {
			protocol.WriteFooter(a.Writer, time.Since(start))
		}

		if a.Filter.Enabled(protocol.BlockTokenStats) {
			protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockTokenStats, usage.String())
		}

		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: response,
		})

		// Execute final tool calls (after LLM response, before returning to user)
		if len(a.Config.FinalToolCalls) > 0 {
			conversationLog := formatConversationLog(messages)
			var finalToolCalls []llm.ToolCall
			var finalResultMsgs []llm.Message

			type finalCallResult struct {
				index   int
				content string
			}
			finalResults := make([]finalCallResult, len(a.Config.FinalToolCalls))
			var wg sync.WaitGroup

			for i, ftc := range a.Config.FinalToolCalls {
				expandedArgs := ReplacePlaceholdersInArgs(ftc.Arguments, opts.UserMessage, conversationLog)
				argsJSON, _ := json.Marshal(expandedArgs)
				argsStr := string(argsJSON)
				callID := fmt.Sprintf("final_%d", i)

				tc := llm.ToolCall{
					ID:   callID,
					Type: "function",
					Function: llm.FunctionCall{
						Name:      ftc.Tool,
						Arguments: argsStr,
					},
				}
				finalToolCalls = append(finalToolCalls, tc)

				if a.Filter.Enabled(protocol.BlockToolsInput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsInput, FormatToolInput(ftc.Tool, argsStr))
				}

				wg.Add(1)
				go func(idx int, toolName, arguments string) {
					defer wg.Done()
					result, err := a.Registry.CallTool(ctx, toolName, arguments)
					if err != nil {
						result = ToolResult{Content: fmt.Sprintf("tool error: %v", err), IsError: true}
					}
					content := result.Content
					if content == "" {
						content = "(no output)"
					}
					finalResults[idx] = finalCallResult{index: idx, content: content}
				}(i, ftc.Tool, argsStr)
			}
			wg.Wait()

			for i, res := range finalResults {
				finalResultMsgs = append(finalResultMsgs, llm.Message{
					Role:       "tool",
					Content:    res.content,
					ToolCallID: fmt.Sprintf("final_%d", i),
				})

				if a.Filter.Enabled(protocol.BlockToolsOutput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsOutput, FormatToolOutput(a.Config.FinalToolCalls[i].Tool, res.content))
				}
			}

			messages = append(messages, llm.Message{
				Role:      "assistant",
				ToolCalls: finalToolCalls,
			})
			messages = append(messages, finalResultMsgs...)
		}

		break
	}

	a.History = messages[1:]

	return &RunResult{
		Response: fullResponse.String(),
		Duration: time.Since(start),
		Usage:    usage,
	}, nil
}

func convertDeltasToToolCalls(deltas []llm.ToolCallDelta) []llm.ToolCall {
	calls := make([]llm.ToolCall, len(deltas))
	for i, d := range deltas {
		args := d.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		calls[i] = llm.ToolCall{
			ID:   d.ID,
			Type: "function",
			Function: llm.FunctionCall{
				Name:      d.Function.Name,
				Arguments: args,
			},
		}
	}
	return calls
}

func (u *TokenUsage) String() string {
	var b strings.Builder
	ts := protocol.FormatPrefix()
	fmt.Fprintf(&b, "%s%-16s prompt: %-8d completion: %d\n", ts, u.AgentName, u.PromptTokens, u.CompletionTokens)
	u.writeChildren(&b, "", ts)
	return b.String()
}

func (u *TokenUsage) writeChildren(b *strings.Builder, prefix string, ts string) {
	for i, child := range u.Children {
		isLast := i == len(u.Children)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		fmt.Fprintf(b, "%s%s%s%-16s prompt: %-8d completion: %d\n", ts, prefix, connector, child.AgentName, child.PromptTokens, child.CompletionTokens)
		childPrefix := prefix
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
		child.writeChildren(b, childPrefix, ts)
	}
}

func ReplacePlaceholdersInArgs(args map[string]interface{}, prompt string, conversation string) map[string]interface{} {
	result := make(map[string]interface{}, len(args))
	for k, v := range args {
		result[k] = replacePlaceholdersInValue(v, prompt, conversation)
	}
	return result
}

func replacePlaceholdersInValue(v interface{}, prompt string, conversation string) interface{} {
	switch val := v.(type) {
	case string:
		s := strings.ReplaceAll(val, "%p", prompt)
		s = strings.ReplaceAll(s, "%c", conversation)
		return s
	case map[string]interface{}:
		return ReplacePlaceholdersInArgs(val, prompt, conversation)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, elem := range val {
			result[i] = replacePlaceholdersInValue(elem, prompt, conversation)
		}
		return result
	default:
		return v
	}
}

func formatConversationLog(messages []llm.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			fmt.Fprintf(&b, "## User\n%s\n\n", msg.Content)
		case "assistant":
			if msg.Content != "" {
				fmt.Fprintf(&b, "## Agent\n%s\n\n", msg.Content)
			}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					fmt.Fprintf(&b, "## Tool Call: %s\n%s\n\n", tc.Function.Name, tc.Function.Arguments)
				}
			}
		case "tool":
			fmt.Fprintf(&b, "## Tool Response\n%s\n\n", msg.Content)
		}
	}
	return b.String()
}

func InterpolatePrompt(prompt string) string {
	now := time.Now()
	prompt = strings.ReplaceAll(prompt, "{current_date}", now.Format("2006-01-02"))
	prompt = strings.ReplaceAll(prompt, "{current_time}", now.Format("2006-01-02T15:04:05"))
	return prompt
}
