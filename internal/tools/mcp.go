package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPProvider struct {
	client   *mcpclient.Client
	cfg      *config.MCPConfig
	timeout  time.Duration
	tools    []mcp.Tool
}

func NewMCPProvider(mcpCfg *config.MCPConfig, rootPath string, timeout time.Duration) (*MCPProvider, error) {
	p := &MCPProvider{
		cfg:     mcpCfg,
		timeout: timeout,
	}

	var client *mcpclient.Client
	var err error

	switch mcpCfg.Type {
	case "stdio":
		client, err = p.startStdio(rootPath)
	case "http":
		client, err = p.startHTTP()
	default:
		return nil, fmt.Errorf("mcp %q: unknown type %q", mcpCfg.Name, mcpCfg.Type)
	}

	if err != nil {
		return nil, err
	}

	p.client = client

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
		return nil, fmt.Errorf("mcp %q initialize: %w", mcpCfg.Name, err)
	}

	toolsReq := mcp.ListToolsRequest{}
	toolsResult, err := client.ListTools(ctx, toolsReq)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp %q list tools: %w", mcpCfg.Name, err)
	}

	p.tools = p.filterTools(toolsResult.Tools)
	return p, nil
}

func (p *MCPProvider) filterTools(tools []mcp.Tool) []mcp.Tool {
	if len(p.cfg.Allow) == 0 && len(p.cfg.Deny) == 0 {
		return tools
	}

	allowSet := make(map[string]bool, len(p.cfg.Allow))
	for _, n := range p.cfg.Allow {
		allowSet[n] = true
	}

	denySet := make(map[string]bool, len(p.cfg.Deny))
	for _, n := range p.cfg.Deny {
		denySet[n] = true
	}

	var filtered []mcp.Tool
	for _, tool := range tools {
		if len(allowSet) > 0 && !allowSet[tool.Name] {
			continue
		}
		if denySet[tool.Name] {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func (p *MCPProvider) startStdio(rootPath string) (*mcpclient.Client, error) {
	command := p.cfg.Command
	if strings.Contains(command, "%s") {
		command = fmt.Sprintf(command, rootPath)
	}

	args := parseCommandArgs(command)
	if len(args) == 0 {
		return nil, fmt.Errorf("mcp %q: empty command", p.cfg.Name)
	}

	client, err := mcpclient.NewStdioMCPClient(args[0], nil, args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("mcp %q spawn: %w", p.cfg.Name, err)
	}

	return client, nil
}

func (p *MCPProvider) startHTTP() (*mcpclient.Client, error) {
	opts := make([]transport.StreamableHTTPCOption, 0)
	if len(p.cfg.Headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(p.cfg.Headers))
	}

	client, err := mcpclient.NewStreamableHttpClient(p.cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("mcp %q connect: %w", p.cfg.Name, err)
	}

	return client, nil
}

func (p *MCPProvider) Close() {
	if p.client != nil {
		p.client.Close()
	}
}

func (p *MCPProvider) prefixedName(name string) string {
	if p.cfg.Prefix != "" {
		return p.cfg.Prefix + name
	}
	return name
}

func (p *MCPProvider) stripPrefix(name string) string {
	if p.cfg.Prefix != "" {
		return strings.TrimPrefix(name, p.cfg.Prefix)
	}
	return name
}

func (p *MCPProvider) ToolDefinitions() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(p.tools))
	for _, tool := range p.tools {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.Function{
				Name:        p.prefixedName(tool.Name),
				Description: tool.Description,
				Parameters:  convertSchema(tool.InputSchema),
			},
		})
	}
	return defs
}

func (p *MCPProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	if p.client == nil {
		return agent.ToolResult{Content: fmt.Sprintf("mcp %q not connected", p.cfg.Name), IsError: true}, nil
	}

	toolName := p.stripPrefix(name)

	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		args = make(map[string]any)
	}

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
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

func parseCommandArgs(cmd string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
