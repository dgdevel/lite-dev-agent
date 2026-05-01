package web

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
)

func TestAskHubRegisterRespond(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan AskResponse, 1)
	hub.Register("test1", ch)

	ok := hub.Respond("test1", "hello")
	if !ok {
		t.Fatal("expected Respond to succeed")
	}

	select {
	case resp := <-ch:
		if resp.Content != "hello" {
			t.Errorf("expected 'hello', got %q", resp.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestAskHubRespondUnknown(t *testing.T) {
	hub := NewAskHub()
	ok := hub.Respond("nonexistent", "test")
	if ok {
		t.Error("expected Respond to fail for unknown ID")
	}
}

func TestAskHubUnregister(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan AskResponse, 1)
	hub.Register("test1", ch)
	hub.Unregister("test1")

	ok := hub.Respond("test1", "hello")
	if ok {
		t.Error("expected Respond to fail after Unregister")
	}
}

func TestAskHubNewID(t *testing.T) {
	hub := NewAskHub()
	id1 := hub.NewID()
	id2 := hub.NewID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

func TestWebAskProviderToolDefinitions(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)
	defs := p.ToolDefinitions()

	if len(defs) != 3 {
		t.Fatalf("expected 3 tool definitions, got %d", len(defs))
	}

	names := []string{"ask_open_ended", "ask_multiple_choice", "ask_exec"}
	for i, name := range names {
		if defs[i].Function.Name != name {
			t.Errorf("definition %d: expected name %q, got %q", i, name, defs[i].Function.Name)
		}
	}
}

func TestWebAskProviderOpenEnded(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result agent.ToolResult
	var err error
	done := make(chan struct{})

	go func() {
		result, err = p.CallTool(ctx, "ask_open_ended", `{"question":"What is your name?"}`)
		close(done)
	}()

	var event Event
	select {
	case event = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ask event")
	}

	if event.Type != "ask_question" {
		t.Fatalf("expected ask_question event, got %q", event.Type)
	}
	if event.AskQuestion.Type != "open_ended" {
		t.Fatalf("expected open_ended ask type, got %q", event.AskQuestion.Type)
	}
	if event.AskQuestion.Question != "What is your name?" {
		t.Fatalf("expected question text, got %q", event.AskQuestion.Question)
	}

	hub.Respond(event.AskQuestion.ID, "my answer")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool result")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if result.Content != "my answer" {
		t.Errorf("expected 'my answer', got %q", result.Content)
	}
}

func TestWebAskProviderOpenEndedMissingQuestion(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	result, err := p.CallTool(context.Background(), "ask_open_ended", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing question")
	}
}

func TestWebAskProviderMultipleChoice(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result agent.ToolResult
	var err error
	done := make(chan struct{})

	go func() {
		result, err = p.CallTool(ctx, "ask_multiple_choice", `{
			"question": "Pick a color",
			"options": ["Red", "Green", "Blue"],
			"type": "single"
		}`)
		close(done)
	}()

	var event Event
	select {
	case event = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ask event")
	}

	if event.AskQuestion.Type != "multiple_choice" {
		t.Fatalf("expected multiple_choice ask type, got %q", event.AskQuestion.Type)
	}
	if len(event.AskQuestion.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(event.AskQuestion.Options))
	}
	if event.AskQuestion.Mode != "single" {
		t.Fatalf("expected single mode, got %q", event.AskQuestion.Mode)
	}

	hub.Respond(event.AskQuestion.ID, "Green")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool result")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "Green" {
		t.Errorf("expected 'Green', got %q", result.Content)
	}
}

func TestWebAskProviderMultipleChoiceValidation(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	tests := []struct {
		name string
		args string
	}{
		{"missing question", `{"options":["A"],"type":"single"}`},
		{"missing options", `{"question":"Q?","type":"single"}`},
		{"empty options", `{"question":"Q?","options":[],"type":"single"}`},
		{"invalid type", `{"question":"Q?","options":["A"],"type":"invalid"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.CallTool(context.Background(), "ask_multiple_choice", tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestWebAskProviderExec(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result agent.ToolResult
	var err error
	done := make(chan struct{})

	go func() {
		result, err = p.CallTool(ctx, "ask_exec", `{
			"cmdline": "echo hello",
			"timeout": 5
		}`)
		close(done)
	}()

	var event Event
	select {
	case event = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ask event")
	}

	if event.AskQuestion.Type != "exec" {
		t.Fatalf("expected exec ask type, got %q", event.AskQuestion.Type)
	}
	if event.AskQuestion.Cmdline != "echo hello" {
		t.Fatalf("expected cmdline, got %q", event.AskQuestion.Cmdline)
	}

	hub.Respond(event.AskQuestion.ID, "y")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool result")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestWebAskProviderExecMissingCmdline(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	result, err := p.CallTool(context.Background(), "ask_exec", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing cmdline")
	}
}

func TestWebAskProviderUnknownTool(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	result, err := p.CallTool(context.Background(), "ask_unknown", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown tool")
	}
}

func TestWebAskProviderContextCancellation(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.CallTool(ctx, "ask_open_ended", `{"question":"test"}`)
		close(done)
	}()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ask event")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation")
	}
}

func TestWebAskProviderWithToolRegistry(t *testing.T) {
	hub := NewAskHub()
	ch := make(chan Event, 16)
	p := NewWebAskProvider("test-agent", hub, ch)
	registry := agent.NewToolRegistry()
	registry.Register("ask", p)

	tools := registry.ToolDefinitions()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools in registry, got %d", len(tools))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result agent.ToolResult
	var err error
	done := make(chan struct{})

	go func() {
		result, err = registry.CallTool(ctx, "ask_open_ended", `{"question":"test"}`)
		close(done)
	}()

	var event Event
	select {
	case event = <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	hub.Respond(event.AskQuestion.ID, "test response")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "test response" {
		t.Errorf("expected 'test response', got %q", result.Content)
	}
}

func TestAskQuestionJSONSerialization(t *testing.T) {
	q := AskQuestion{
		ID:           "ask_1",
		Type:         "multiple_choice",
		Question:     "Pick one",
		Options:      []string{"A", "B", "C"},
		Mode:         "single",
		AllowOpenEnd: true,
	}

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var q2 AskQuestion
	if err := json.Unmarshal(data, &q2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if q2.ID != q.ID || q2.Type != q.Type || q2.Mode != q.Mode {
		t.Errorf("roundtrip mismatch: %+v vs %+v", q, q2)
	}
	if len(q2.Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(q2.Options))
	}
}

func TestEventWithAskQuestion(t *testing.T) {
	ch := make(chan Event, 16)
	e := Event{
		Type: "ask_question",
		AskQuestion: &AskQuestion{
			ID:       "ask_1",
			Type:     "open_ended",
			Question: "hello?",
		},
	}
	ch <- e

	select {
	case got := <-ch:
		if got.Type != "ask_question" {
			t.Errorf("expected ask_question, got %q", got.Type)
		}
		if got.AskQuestion == nil {
			t.Fatal("expected AskQuestion to be set")
		}
		if got.AskQuestion.Question != "hello?" {
			t.Errorf("expected 'hello?', got %q", got.AskQuestion.Question)
		}
	default:
		t.Fatal("expected event on channel")
	}
}
