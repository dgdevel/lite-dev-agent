package tools

import (
	"context"
	"fmt"
	"io"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type AgentToolProvider struct {
	Agents     map[string]*agent.Agent
	Config     *config.Config
	Writer     io.Writer
	Filter     *protocol.OutputFilter
	Timeouts   *config.TimeoutConfig
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
	var defs []llm.ToolDefinition
	for _, ac := range p.Config.ExposedAgents() {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.Function{
				Name:        ac.Name,
				Description: ac.Expose,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"prompt": map[string]any{
							"type":        "string",
							"description": "The request to send to the agent",
						},
					},
					"required": []string{"prompt"},
				},
			},
		})
	}
	return defs
}

func (p *AgentToolProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	a, ok := p.Agents[name]
	if !ok {
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown agent: %s", name),
			IsError: true,
		}, nil
	}

	prompt, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}, nil
	}

	userMsg := llm.GetArgumentString(prompt, "prompt")
	if userMsg == "" {
		return agent.ToolResult{
			Content: "missing prompt argument",
			IsError: true,
		}, nil
	}

	result, err := a.Run(ctx, agent.RunOptions{
		UserMessage: userMsg,
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
