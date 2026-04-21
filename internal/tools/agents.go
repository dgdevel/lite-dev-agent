package tools

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type AgentToolProvider struct {
	Agents   map[string]*agent.Agent
	Config   *config.Config
	Writer   io.Writer
	Filter   *protocol.OutputFilter
	Timeouts *config.TimeoutConfig
}

func NewAgentToolProvider(cfg *config.Config, writer io.Writer, filter *protocol.OutputFilter, timeouts *config.TimeoutConfig) *AgentToolProvider {
	return &AgentToolProvider{
		Agents:   make(map[string]*agent.Agent),
		Config:   cfg,
		Writer:   writer,
		Filter:   filter,
		Timeouts: timeouts,
	}
}

func (p *AgentToolProvider) Register(a *agent.Agent) {
	p.Agents[a.Config.Name] = a
}

func (p *AgentToolProvider) ToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Type: "function",
			Function: llm.Function{
				Name:        "agents_available",
				Description: "List available agents",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
					"required":   []string{},
				},
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "invoke_agent",
				Description: "Send request to agent",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent_name": map[string]any{
							"type":        "string",
							"description": "Name of agent",
						},
						"prompt": map[string]any{
							"type":        "string",
							"description": "Request to send",
						},
					},
					"required": []string{"agent_name", "prompt"},
				},
			},
		},
	}
}

func (p *AgentToolProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	switch name {
	case "agents_available":
		return p.callAgentsAvailable()
	case "invoke_agent":
		return p.callInvokeAgent(ctx, arguments)
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown tool: %s", name),
			IsError: true,
		}, nil
	}
}

func (p *AgentToolProvider) callAgentsAvailable() (agent.ToolResult, error) {
	exposed := p.Config.ExposedAgents()
	var sb strings.Builder
	for i, ac := range exposed {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("Name: ")
		sb.WriteString(ac.Name)
		sb.WriteString("\nDescription: ")
		sb.WriteString(ac.Expose)
	}
	return agent.ToolResult{
		Content: sb.String(),
	}, nil
}

func (p *AgentToolProvider) callInvokeAgent(ctx context.Context, arguments string) (agent.ToolResult, error) {
	args, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}, nil
	}

	agentName := llm.GetArgumentString(args, "agent_name")
	if agentName == "" {
		return agent.ToolResult{
			Content: "missing agent_name argument",
			IsError: true,
		}, nil
	}

	prompt := llm.GetArgumentString(args, "prompt")
	if prompt == "" {
		return agent.ToolResult{
			Content: "missing prompt argument",
			IsError: true,
		}, nil
	}

	a, ok := p.Agents[agentName]
	if !ok {
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown agent: %s", agentName),
			IsError: true,
		}, nil
	}

	result, err := a.Run(ctx, agent.RunOptions{
		UserMessage: prompt,
		Level:       agent.LevelFromContext(ctx) + 1,
	})
	if err != nil {
		if _, ok := err.(*agent.InterruptionError); ok {
			return agent.ToolResult{
				Content: "agent interrupted by user",
				IsError: true,
			}, nil
		}
		return agent.ToolResult{
			Content: fmt.Sprintf("agent error: %v", err),
			IsError: true,
		}, nil
	}

	return agent.ToolResult{
		Content: result.Response,
		Usage:   result.Usage,
	}, nil
}
