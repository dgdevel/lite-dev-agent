package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type DevkitProvider struct {
	client   *mcpclient.Client
	rootPath string
	timeout  time.Duration
	tools    []mcp.Tool
}

func NewDevkitProvider(devkitPath, rootPath string, timeout time.Duration) (*DevkitProvider, error) {
	if devkitPath == "" {
		found, err := exec.LookPath("nixdevkit")
		if err != nil {
			return nil, fmt.Errorf("nixdevkit not found in PATH and --devkit-path not provided")
		}
		devkitPath = found
	}

	p := &DevkitProvider{
		rootPath: rootPath,
		timeout:  timeout,
	}

	if err := p.start(devkitPath); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *DevkitProvider) start(devkitPath string) error {
	client, err := mcpclient.NewStdioMCPClient(devkitPath, nil, p.rootPath)
	if err != nil {
		return fmt.Errorf("spawn nixdevkit: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = "2024-11-05"
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "lite-dev-agent",
		Version: "0.1.0",
	}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		client.Close()
		return fmt.Errorf("MCP initialize: %w", err)
	}

	p.client = client

	toolsReq := mcp.ListToolsRequest{}
	toolsResult, err := client.ListTools(ctx, toolsReq)
	if err != nil {
		client.Close()
		return fmt.Errorf("MCP list tools: %w", err)
	}

	p.tools = toolsResult.Tools
	return nil
}

func (p *DevkitProvider) Close() {
	if p.client != nil {
		p.client.Close()
	}
}

func (p *DevkitProvider) ToolDefinitions() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(p.tools))
	for _, tool := range p.tools {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.Function{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  convertSchema(tool.InputSchema),
			},
		})
	}
	return defs
}

func (p *DevkitProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	if p.client == nil {
		return agent.ToolResult{Content: "devkit not connected", IsError: true}, nil
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		args = make(map[string]any)
	}

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := p.client.CallTool(callCtx, req)
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return agent.ToolResult{
				Content: fmt.Sprintf("tool %q timed out after %s", name, p.timeout),
				IsError: true,
			}, nil
		}
		return agent.ToolResult{
			Content: fmt.Sprintf("tool %q error: %v", name, err),
			IsError: true,
		}, nil
	}

	var contentParts []string
	for _, c := range result.Content {
		if text, ok := c.(mcp.TextContent); ok {
			contentParts = append(contentParts, text.Text)
		}
	}

	response := strings.Join(contentParts, "\n")
	if result.IsError {
		return agent.ToolResult{Content: response, IsError: true}, nil
	}

	return agent.ToolResult{Content: response}, nil
}

func convertSchema(schema mcp.ToolInputSchema) map[string]any {
	result := map[string]any{
		"type":       "object",
		"properties": schema.Properties,
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	return result
}
