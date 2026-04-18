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
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def (only exposed agents), got %d", len(defs))
	}
	if defs[0].Function.Name != "worker" {
		t.Fatalf("tool name: %s", defs[0].Function.Name)
	}
	if defs[0].Function.Description != "A worker agent" {
		t.Fatalf("tool description: %s", defs[0].Function.Description)
	}
}

func TestAgentToolProviderCallUnknownAgent(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "unknown", `{"prompt": "test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for unknown agent")
	}
}

func TestAgentToolProviderCallMissingPrompt(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "worker", `{"wrong": "arg"}`)
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

	result, err := p.CallTool(context.Background(), "worker", "not json")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for invalid args")
	}
}

func TestAgentToolProviderCallWithNoRegisteredAgent(t *testing.T) {
	cfg := makeTestConfig()
	var buf bytes.Buffer
	filter := protocol.NewOutputFilter("")
	p := NewAgentToolProvider(cfg, &buf, filter, &cfg.Timeouts)

	result, err := p.CallTool(context.Background(), "worker", `{"prompt": "do work"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("worker agent not registered, should error")
	}
}
