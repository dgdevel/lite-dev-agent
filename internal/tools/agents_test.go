package tools

import (
	"bytes"
	"context"
	"testing"

	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

func makeTestConfig() *config.Config {
	return &config.Config{
		LLMs: []config.LLMConfig{
			{Name: "test", Default: true, APIBase: "http://localhost/v1"},
		},
		Agents: []config.AgentConfig{
			{
				Name:         "manager",
				Default:      true,
				Tools:        "agents",
				SystemPrompt: "You manage",
			},
			{
				Name:         "worker",
				Tools:        "agents",
				Expose:       "A worker agent",
				SystemPrompt: "You work",
			},
		},
	}
}

func TestAgentToolProviderToolDefinitions(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	defs := p.ToolDefinitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs (agents_available, invoke_agent), got %d", len(defs))
	}
	if defs[0].Function.Name != "agents_available" {
		t.Fatalf("first tool name: %s", defs[0].Function.Name)
	}
	if defs[1].Function.Name != "invoke_agent" {
		t.Fatalf("second tool name: %s", defs[1].Function.Name)
	}
	if defs[1].Function.Description != "Send request to agent" {
		t.Fatalf("invoke_agent description: %s", defs[1].Function.Description)
	}
}

func TestAgentToolProviderAgentsAvailable(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "agents_available", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("should not be error")
	}
	expected := "Name: worker\nDescription: A worker agent"
	if result.Content != expected {
		t.Fatalf("expected %q, got %q", expected, result.Content)
	}
}

func TestAgentToolProviderCallUnknownAgent(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "invoke_agent", `{"agent_name": "unknown", "prompt": "test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for unknown agent")
	}
}

func TestAgentToolProviderCallMissingAgentName(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "invoke_agent", `{"prompt": "test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for missing agent_name")
	}
}

func TestAgentToolProviderCallMissingPrompt(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "invoke_agent", `{"agent_name": "worker"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for missing prompt")
	}
}

func TestAgentToolProviderCallInvalidArgs(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "invoke_agent", "not json")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for invalid args")
	}
}

func TestAgentToolProviderCallUnknownTool(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "something_else", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for unknown tool")
	}
}

func TestAgentToolProviderCallWithNoRegisteredAgent(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "invoke_agent", `{"agent_name": "worker", "prompt": "do work"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("worker agent not registered, should error")
	}
}
