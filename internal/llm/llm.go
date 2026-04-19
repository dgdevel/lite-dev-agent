package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type StreamEvent struct {
	Type StreamEventType

	DeltaContent          string
	DeltaReasoningContent string
	ToolCalls             []ToolCallDelta

	FinishReason string

	UsagePromptTokens     int64
	UsageCompletionTokens int64
}

type StreamEventType int

const (
	EventText StreamEventType = iota
	EventThinking
	EventToolCall
	EventDone
	EventError
	EventUsage
)

type ToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

type Client struct {
	apiBase    string
	model      string
	apiKey     string
	headers    map[string]string
	httpClient *http.Client
	timeout    time.Duration
	maxTokens  int
}

type Options struct {
	APIBase   string
	Model     string
	APIKey    string
	Headers   map[string]string
	Timeout   time.Duration
	MaxTokens int
}

func NewClient(opts Options) *Client {
	return &Client{
		apiBase: strings.TrimRight(opts.APIBase, "/"),
		model:   opts.Model,
		apiKey:  opts.APIKey,
		headers: opts.Headers,
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		timeout:   opts.Timeout,
		maxTokens: opts.MaxTokens,
	}
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	Tools         []ToolDefinition `json:"tools,omitempty"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content,omitempty"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type modelInfo struct {
	ID string `json:"id"`
}

type modelsResponse struct {
	Data []modelInfo `json:"data"`
}

func (c *Client) ModelContextWindow(ctx context.Context) int {
	if c.maxTokens > 0 {
		return c.maxTokens
	}

	url := c.apiBase + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 128000
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 128000
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 128000
	}

	return 128000
}

func (c *Client) ChatCompletion(ctx context.Context, messages []Message, tools []ToolDefinition) (string, []ToolCall, int64, int64, error) {
	body := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", nil, 0, 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", nil, 0, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, 0, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, 0, 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, 0, 0, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respData))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respData, &chatResp); err != nil {
		return "", nil, 0, 0, fmt.Errorf("parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", nil, 0, 0, fmt.Errorf("no choices in response")
	}

	choice := chatResp.Choices[0]
	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return choice.Message.Content, toolCalls, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, nil
}

type StreamCallback func(event StreamEvent)

func (c *Client) ChatCompletionStream(ctx context.Context, messages []Message, tools []ToolDefinition, cb StreamCallback) error {
	body := chatRequest{
		Model:         c.model,
		Messages:      messages,
		Tools:         tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	toolCallAccum := make(map[int]*ToolCallDelta)
	finished := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			if !finished {
				cb(StreamEvent{Type: EventDone})
			}
			return nil
		}

		var chunk struct {
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Role             string `json:"role"`
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id,omitempty"`
						Type     string `json:"type,omitempty"`
						Function struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments,omitempty"`
						} `json:"function"`
					} `json:"tool_calls,omitempty"`
				} `json:"delta"`
			} `json:"choices"`
			Timings struct {
				PromptN    int `json:"prompt_n"`
				PredictedN int `json:"predicted_n"`
			} `json:"timings"`
			Usage *struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				cb(StreamEvent{
					Type:                  EventUsage,
					UsagePromptTokens:     chunk.Usage.PromptTokens,
					UsageCompletionTokens: chunk.Usage.CompletionTokens,
				})
			}
			continue
		}

		choice := chunk.Choices[0]

		if choice.Delta.ReasoningContent != "" {
			cb(StreamEvent{
				Type:                  EventThinking,
				DeltaReasoningContent: choice.Delta.ReasoningContent,
			})
		}

		if choice.Delta.Content != "" {
			cb(StreamEvent{
				Type:         EventText,
				DeltaContent: choice.Delta.Content,
			})
		}

		for _, tc := range choice.Delta.ToolCalls {
			existing, ok := toolCallAccum[tc.Index]
			if !ok {
				existing = &ToolCallDelta{Index: tc.Index}
				toolCallAccum[tc.Index] = existing
			}
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				existing.Function.Arguments += tc.Function.Arguments
			}
		}

		if choice.FinishReason != nil {
			finished = true
			switch *choice.FinishReason {
			case "tool_calls":
				finalized := make([]ToolCallDelta, 0, len(toolCallAccum))
				for i := 0; i < len(toolCallAccum); i++ {
					if acc, ok := toolCallAccum[i]; ok {
						finalized = append(finalized, *acc)
					}
				}
				cb(StreamEvent{
					Type:         EventToolCall,
					ToolCalls:    finalized,
					FinishReason: "tool_calls",
				})
				cb(StreamEvent{Type: EventDone, FinishReason: "tool_calls"})
			case "stop":
				var promptTokens, completionTokens int64
				if chunk.Usage != nil {
					promptTokens = chunk.Usage.PromptTokens
					completionTokens = chunk.Usage.CompletionTokens
				} else {
					promptTokens = int64(chunk.Timings.PromptN)
					completionTokens = int64(chunk.Timings.PredictedN)
				}
				cb(StreamEvent{
					Type:                  EventDone,
					FinishReason:          "stop",
					UsagePromptTokens:     promptTokens,
					UsageCompletionTokens: completionTokens,
				})
			}
		}
	}

	return scanner.Err()
}

func (c *Client) setAuth(req *http.Request) {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.apiKey != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func EstimateTokens(text string) int {
	return len(text) / 4
}

func TruncateHistory(messages []Message, maxTokens int, systemPrompt string) []Message {
	estimated := EstimateTokens(systemPrompt)
	kept := make([]Message, 0, len(messages))

	for i := len(messages) - 1; i >= 0; i-- {
		tok := EstimateTokens(messages[i].Content)
		for _, tc := range messages[i].ToolCalls {
			tok += EstimateTokens(tc.Function.Name + tc.Function.Arguments)
		}
		if estimated+tok > maxTokens && len(kept) > 0 {
			break
		}
		estimated += tok
		kept = append(kept, messages[i])
	}

	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	return kept
}

func ParseToolCallArguments(args string) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	return result, nil
}

func GetArgumentString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", val)
	}
}
