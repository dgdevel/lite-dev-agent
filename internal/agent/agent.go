package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

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

type RunOptions struct {
	UserMessage string
	History     []llm.Message
}

type RunResult struct {
	Response        string
	PromptTokens    int64
	CompletionTokens int64
	Duration        time.Duration
}

func (a *Agent) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	start := time.Now()

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
		protocol.WriteBlock(a.Writer, a.Config.Name, protocol.BlockSystemPrompt, systemPrompt)
	}
	if opts.UserMessage != "" && a.Filter.Enabled(protocol.BlockUserMessage) {
		protocol.WriteBlock(a.Writer, a.Config.Name, protocol.BlockUserMessage, opts.UserMessage)
	}

	toolDefs := a.Registry.ToolDefinitions()

	var fullResponse strings.Builder
	var totalPromptTok, totalCompTok int64

	for {
		llmTimeout := a.Timeouts.LLMRequestDuration()
		reqCtx, cancel := context.WithTimeout(ctx, llmTimeout)

		var textBuf strings.Builder
		var thinkingBuf strings.Builder
		var toolCalls []llm.ToolCallDelta
		var streamPromptTok, streamCompTok int64

		err := a.LLM.ChatCompletionStream(reqCtx, messages, toolDefs, func(e llm.StreamEvent) {
			switch e.Type {
			case llm.EventText:
				textBuf.WriteString(e.DeltaContent)
			case llm.EventThinking:
				thinkingBuf.WriteString(e.DeltaReasoningContent)
				if a.Filter.Enabled(protocol.BlockThinking) {
					io.WriteString(a.Writer, e.DeltaReasoningContent)
				}
			case llm.EventToolCall:
				toolCalls = e.ToolCalls
			case llm.EventDone:
				streamPromptTok = e.UsagePromptTokens
				streamCompTok = e.UsageCompletionTokens
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

		totalPromptTok += streamPromptTok
		totalCompTok += streamCompTok

		if thinkingBuf.Len() > 0 && a.Filter.Enabled(protocol.BlockThinking) {
			io.WriteString(a.Writer, "\n")
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
					protocol.WriteBlock(a.Writer, a.Config.Name, protocol.BlockToolsInput, FormatToolInput(toolName, toolArgs))
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
				protocol.WriteBlock(a.Writer, a.Config.Name, protocol.BlockToolsOutput, FormatToolOutput(toolName, toolContent))
			}

				if a.Filter.Enabled(protocol.BlockAgentResponse) {
					protocol.WriteFooter(a.Writer, time.Since(start), totalPromptTok, totalCompTok)
				}
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
			protocol.WriteBlock(a.Writer, a.Config.Name, protocol.BlockAgentResponse, response)
		}

		if a.Filter.Enabled(protocol.BlockAgentResponse) {
			protocol.WriteFooter(a.Writer, time.Since(start), totalPromptTok, totalCompTok)
		}

		messages = append(messages, llm.Message{
			Role:    "assistant",
			Content: response,
		})

		break
	}

	a.History = messages[1:]

	return &RunResult{
		Response:         fullResponse.String(),
		PromptTokens:     totalPromptTok,
		CompletionTokens: totalCompTok,
		Duration:         time.Since(start),
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

func interpolatePrompt(prompt string) string {
	now := time.Now()
	prompt = strings.ReplaceAll(prompt, "{current_date}", now.Format("2006-01-02"))
	prompt = strings.ReplaceAll(prompt, "{current_time}", now.Format("2006-01-02T15:04:05"))
	return prompt
}
