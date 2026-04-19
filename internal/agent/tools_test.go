package agent

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type mockProvider struct {
	defs []llm.ToolDefinition
	calls map[string]string
}

func newMockProvider(defs []llm.ToolDefinition) *mockProvider {
	return &mockProvider{
		defs:  defs,
		calls: make(map[string]string),
	}
}

func (m *mockProvider) ToolDefinitions() []llm.ToolDefinition {
	return m.defs
}

func (m *mockProvider) CallTool(ctx context.Context, name string, arguments string) (ToolResult, error) {
	m.calls[name] = arguments
	if name == "fail_tool" {
		return ToolResult{Content: "tool failed", IsError: true}, nil
	}
	return ToolResult{Content: "result of " + name}, nil
}

func TestToolRegistryRegister(t *testing.T) {
	r := NewToolRegistry()
	p := newMockProvider([]llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "search", Description: "Search"}},
		{Type: "function", Function: llm.Function{Name: "fetch", Description: "Fetch"}},
	})
	r.Register("test", p)

	if !r.HasGroup("test") {
		t.Fatal("should have group test")
	}
	if r.HasGroup("nonexistent") {
		t.Fatal("should not have group nonexistent")
	}
}

func TestToolRegistryToolDefinitions(t *testing.T) {
	r := NewToolRegistry()
	p := newMockProvider([]llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "search", Description: "Search"}},
	})
	r.Register("test", p)

	defs := r.ToolDefinitions()
	if len(defs) != 1 || defs[0].Function.Name != "search" {
		t.Fatalf("defs: %v", defs)
	}
}

func TestToolRegistryCallTool(t *testing.T) {
	r := NewToolRegistry()
	p := newMockProvider([]llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "search", Description: "Search"}},
	})
	r.Register("test", p)

	result, err := r.CallTool(context.Background(), "search", `{"query": "test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "result of search" {
		t.Fatalf("content: %s", result.Content)
	}
}

func TestToolRegistryCallUnknownTool(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.CallTool(context.Background(), "unknown", "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error")
	}
}

func TestFormatToolInput(t *testing.T) {
	got := FormatToolInput("search", `{"query": "test"}`)
	if !strings.Contains(got, "Tool name: search") {
		t.Fatalf("missing tool name: %q", got)
	}
	if !strings.Contains(got, "Argument 1 (query)") {
		t.Fatalf("missing argument: %q", got)
	}
}

func TestFormatToolInputInvalidArgs(t *testing.T) {
	got := FormatToolInput("search", "not json")
	if !strings.Contains(got, "Tool name: search") {
		t.Fatalf("missing tool name: %q", got)
	}
}

func TestFormatToolOutput(t *testing.T) {
	got := FormatToolOutput("search", "found results")
	if !strings.Contains(got, "Tool name: search") {
		t.Fatalf("missing tool name: %q", got)
	}
	if !strings.Contains(got, "Response:\nfound results") {
		t.Fatalf("missing response: %q", got)
	}
}

func TestOutputFilterIntegration(t *testing.T) {
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("agent_response")

	registry := NewToolRegistry()
	_ = registry

	_ = &Agent{
		Writer: &buf,
		Filter: filter,
	}

	if !filter.Enabled(protocol.BlockAgentResponse) {
		t.Fatal("agent_response should be enabled")
	}
	if filter.Enabled(protocol.BlockThinking) {
		t.Fatal("agent_thinking should be disabled")
	}
}

func TestFormatToolDefinitions(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "search", Description: "Search the web", Parameters: map[string]any{"type": "object"}}},
		{Type: "function", Function: llm.Function{Name: "read", Description: "Read a file"}},
	}
	got := FormatToolDefinitions(defs)
	if !strings.Contains(got, "search: Search the web") {
		t.Fatalf("missing search tool: %q", got)
	}
	if !strings.Contains(got, "Parameters:") {
		t.Fatalf("missing parameters: %q", got)
	}
	if !strings.Contains(got, "read: Read a file") {
		t.Fatalf("missing read tool: %q", got)
	}
}

func TestResolveTools(t *testing.T) {
	r := NewToolRegistry()
	p1 := newMockProvider([]llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "search", Description: "Search"}},
	})
	p2 := newMockProvider([]llm.ToolDefinition{
		{Type: "function", Function: llm.Function{Name: "read", Description: "Read"}},
	})
	r.Register("online", p1)
	r.Register("devkit", p2)

	defs := ResolveTools([]string{"online", "devkit"}, r)
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
}

func TestResolveToolsMissingGroup(t *testing.T) {
	r := NewToolRegistry()
	defs := ResolveTools([]string{"nonexistent"}, r)
	if len(defs) != 0 {
		t.Fatalf("expected 0 defs for missing group, got %d", len(defs))
	}
}
