package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient(Options{
		APIBase: "http://localhost:8080/v1",
		Model:   "test-model",
		Timeout: 5 * time.Minute,
	})
	if c.apiBase != "http://localhost:8080/v1" {
		t.Fatalf("apiBase: %s", c.apiBase)
	}
	if c.model != "test-model" {
		t.Fatalf("model: %s", c.model)
	}
}

func TestNewClientTrailingSlash(t *testing.T) {
	c := NewClient(Options{APIBase: "http://localhost/v1/"})
	if c.apiBase != "http://localhost/v1" {
		t.Fatalf("trailing slash not trimmed: %s", c.apiBase)
	}
}

func TestSetAuth(t *testing.T) {
	c := NewClient(Options{
		APIKey: "mykey",
		Headers: map[string]string{
			"X-Custom": "val",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c.setAuth(req)

	if req.Header.Get("Authorization") != "Bearer mykey" {
		t.Fatalf("expected Bearer mykey, got %s", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Custom") != "val" {
		t.Fatalf("expected val, got %s", req.Header.Get("X-Custom"))
	}
}

func TestSetAuthHeadersPrecedence(t *testing.T) {
	c := NewClient(Options{
		APIKey: "mykey",
		Headers: map[string]string{
			"Authorization": "Custom abc",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	c.setAuth(req)

	if req.Header.Get("Authorization") != "Custom abc" {
		t.Fatalf("headers should take precedence, got %s", req.Header.Get("Authorization"))
	}
}

func TestChatCompletionNonStreaming(t *testing.T) {
	response := `{
		"choices": [{
			"finish_reason": "stop",
			"message": {
				"role": "assistant",
				"content": "Hello!"
			}
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()

	c := NewClient(Options{
		APIBase: server.URL,
		Model:   "test",
		Timeout: 10 * time.Second,
	})

	content, toolCalls, promptTok, compTok, err := c.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil)

	if err != nil {
		t.Fatal(err)
	}
	if content != "Hello!" {
		t.Fatalf("content: %q", content)
	}
	if len(toolCalls) != 0 {
		t.Fatalf("toolCalls: %v", toolCalls)
	}
	if promptTok != 10 || compTok != 5 {
		t.Fatalf("tokens: prompt=%d comp=%d", promptTok, compTok)
	}
}

func TestChatCompletionWithToolCalls(t *testing.T) {
	response := `{
		"choices": [{
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{
					"id": "call_123",
					"type": "function",
					"function": {
						"name": "search",
						"arguments": "{\"query\": \"test\"}"
					}
				}]
			}
		}],
		"usage": {
			"prompt_tokens": 20,
			"completion_tokens": 10
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 10 * time.Second})

	content, toolCalls, _, _, err := c.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "search for test"},
	}, []ToolDefinition{
		{Type: "function", Function: Function{Name: "search", Description: "Search"}},
	})

	if err != nil {
		t.Fatal(err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_123" || tc.Function.Name != "search" {
		t.Fatalf("tool call: %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["query"] != "test" {
		t.Fatalf("args: %v", args)
	}
}

func TestChatCompletionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": "model not found"}`)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 10 * time.Second})

	_, _, _, _, err := c.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 in error, got: %v", err)
	}
}

func TestChatCompletionNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices": []}`)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 10 * time.Second})

	_, _, _, _, err := c.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected no choices error, got: %v", err)
	}
}

func TestChatCompletionStream(t *testing.T) {
	sseData := `data: {"choices":[{"finish_reason":null,"index":0,"delta":{"role":"assistant","content":null}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"reasoning_content":"thinking..."}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"content":"Hello"}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"content":" world"}}]}

data: {"choices":[{"finish_reason":"stop","index":0,"delta":{}}],"timings":{"prompt_n":10,"predicted_n":5}}

data: [DONE]
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if !reqBody.Stream {
			t.Error("expected stream=true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseData)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 10 * time.Second})

	var events []StreamEvent
	err := c.ChatCompletionStream(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, func(e StreamEvent) {
		events = append(events, e)
	})

	if err != nil {
		t.Fatal(err)
	}

	var textParts, thinkingParts []string
	var doneCount int
	for _, e := range events {
		switch e.Type {
		case EventText:
			textParts = append(textParts, e.DeltaContent)
		case EventThinking:
			thinkingParts = append(thinkingParts, e.DeltaReasoningContent)
		case EventDone:
			doneCount++
		}
	}

	combined := strings.Join(textParts, "")
	if combined != "Hello world" {
		t.Fatalf("text: %q", combined)
	}

	thinking := strings.Join(thinkingParts, "")
	if thinking != "thinking..." {
		t.Fatalf("thinking: %q", thinking)
	}

	if doneCount != 1 {
		t.Fatalf("done count: %d", doneCount)
	}
}

func TestChatCompletionStreamWithToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"finish_reason":null,"index":0,"delta":{"role":"assistant","content":null}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"qu"}}]}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ery\": \"test\"}"}}]}}]}

data: {"choices":[{"finish_reason":"tool_calls","index":0,"delta":{}}]}

data: [DONE]
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseData)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 10 * time.Second})

	var events []StreamEvent
	err := c.ChatCompletionStream(context.Background(), []Message{
		{Role: "user", Content: "search"},
	}, []ToolDefinition{{Type: "function", Function: Function{Name: "search"}}}, func(e StreamEvent) {
		events = append(events, e)
	})

	if err != nil {
		t.Fatal(err)
	}

	var toolCallEvent *StreamEvent
	for i := range events {
		if events[i].Type == EventToolCall {
			toolCallEvent = &events[i]
			break
		}
	}

	if toolCallEvent == nil {
		t.Fatal("no tool call event")
	}
	if len(toolCallEvent.ToolCalls) != 1 {
		t.Fatalf("tool calls: %d", len(toolCallEvent.ToolCalls))
	}
	tc := toolCallEvent.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "search" {
		t.Fatalf("tool call: %+v", tc)
	}
	if tc.Function.Arguments != `{"query": "test"}` {
		t.Fatalf("arguments: %q", tc.Function.Arguments)
	}
}

func TestChatCompletionStreamTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	c := NewClient(Options{APIBase: server.URL, Model: "test", Timeout: 100 * time.Millisecond})

	err := c.ChatCompletionStream(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, func(e StreamEvent) {})

	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("empty string should be 0")
	}
	if EstimateTokens("hello world") != 2 {
		t.Fatalf("expected 2, got %d", EstimateTokens("hello world"))
	}
}

func TestTruncateHistory(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "first message"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second message"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "latest message"},
	}

	truncated := TruncateHistory(msgs, 100, "system prompt")
	if len(truncated) != 5 {
		t.Fatalf("all should fit, got %d", len(truncated))
	}

	truncated = TruncateHistory(msgs, 10, "system prompt")
	if len(truncated) < 1 {
		t.Fatal("should keep at least 1 message")
	}

	last := truncated[len(truncated)-1]
	if last.Content != "latest message" {
		t.Fatalf("should keep latest, got %q", last.Content)
	}
}

func TestParseToolCallArguments(t *testing.T) {
	args, err := ParseToolCallArguments(`{"query": "test", "count": 5}`)
	if err != nil {
		t.Fatal(err)
	}
	if args["query"] != "test" {
		t.Fatalf("query: %v", args["query"])
	}
}

func TestParseToolCallArgumentsInvalid(t *testing.T) {
	_, err := ParseToolCallArguments(`not json`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetArgumentString(t *testing.T) {
	args := map[string]any{
		"query": "test",
		"count": float64(5),
	}
	if GetArgumentString(args, "query") != "test" {
		t.Fatal("string arg")
	}
	if GetArgumentString(args, "count") != "5" {
		t.Fatalf("numeric arg: %s", GetArgumentString(args, "count"))
	}
	if GetArgumentString(args, "missing") != "" {
		t.Fatal("missing arg")
	}
}

func TestModelContextWindow(t *testing.T) {
	c := NewClient(Options{MaxTokens: 64000})
	if c.ModelContextWindow(context.Background()) != 64000 {
		t.Fatal("should use config maxTokens")
	}
}

func TestModelContextWindowDefault(t *testing.T) {
	c := NewClient(Options{})
	if c.ModelContextWindow(context.Background()) != 128000 {
		t.Fatal("should default to 128000")
	}
}
