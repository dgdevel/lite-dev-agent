package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	Config    *config.AgentConfig
	LLMConfig *config.LLMConfig
	LLM       *llm.Client
	Registry  *ToolRegistry
	Writer    io.Writer
	Filter    *protocol.OutputFilter
	Timeouts  *config.TimeoutConfig

	History []llm.Message
	IsMain  bool
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

	systemPrompt := interpolatePrompt(a.Config.SystemPrompt)

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

		for i, itc := range a.Config.InitialToolCalls {
			argsJSON, _ := json.Marshal(itc.Arguments)
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

			result, err := a.Registry.CallTool(ctx, itc.Tool, argsStr)
			if err != nil {
				result = ToolResult{Content: fmt.Sprintf("tool error: %v", err), IsError: true}
			}

			content := result.Content
			if content == "" {
				content = "(no output)"
			}

			toolResultMsgs = append(toolResultMsgs, llm.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: callID,
			})

			if a.Filter.Enabled(protocol.BlockToolsOutput) {
				protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsOutput, FormatToolOutput(itc.Tool, content))
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
				toolName := tc.Function.Name
				toolArgs := tc.Function.Arguments

				if a.Filter.Enabled(protocol.BlockToolsInput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsInput, FormatToolInput(toolName, toolArgs))
				}

				result, err := a.Registry.CallTool(ctx, toolName, toolArgs)
				if err != nil {
					result = ToolResult{
						Content: fmt.Sprintf("tool error: %v", err),
						IsError: true,
					}
				}

				toolContent := result.Content
				if toolContent == "" {
					toolContent = "(no output)"
				}

				messages = append(messages, llm.Message{
					Role:       "tool",
					Content:    toolContent,
					ToolCallID: tc.ID,
				})

				if a.Filter.Enabled(protocol.BlockToolsOutput) {
					protocol.WriteBlock(a.Writer, a.Config.Name, opts.Level, protocol.BlockToolsOutput, FormatToolOutput(toolName, toolContent))
				}

				if result.Usage != nil {
					usage.Children = append(usage.Children, result.Usage)
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
		calls[i] = llm.ToolCall{
			ID:   d.ID,
			Type: "function",
			Function: llm.FunctionCall{
				Name:      d.Function.Name,
				Arguments: d.Function.Arguments,
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

func interpolatePrompt(prompt string) string {
	now := time.Now()
	prompt = strings.ReplaceAll(prompt, "{current_date}", now.Format("2006-01-02"))
	prompt = strings.ReplaceAll(prompt, "{current_time}", now.Format("2006-01-02T15:04:05"))
	return prompt
}
