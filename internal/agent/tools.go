package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dgdevel/lite-dev-agent/internal/llm"
)

type ToolResult struct {
	Content string
	IsError bool
}

type ToolProvider interface {
	ToolDefinitions() []llm.ToolDefinition
	CallTool(ctx context.Context, name string, arguments string) (ToolResult, error)
}

type ToolRegistry struct {
	providers map[string]ToolProvider
	nameMap   map[string]ToolProvider
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		providers: make(map[string]ToolProvider),
		nameMap:   make(map[string]ToolProvider),
	}
}

func (r *ToolRegistry) Register(groupName string, provider ToolProvider) {
	r.providers[groupName] = provider
	for _, def := range provider.ToolDefinitions() {
		r.nameMap[def.Function.Name] = provider
	}
}

func (r *ToolRegistry) HasGroup(name string) bool {
	_, ok := r.providers[name]
	return ok
}

func (r *ToolRegistry) ToolDefinitions() []llm.ToolDefinition {
	var defs []llm.ToolDefinition
	for _, p := range r.providers {
		defs = append(defs, p.ToolDefinitions()...)
	}
	return defs
}

func (r *ToolRegistry) CallTool(ctx context.Context, name string, arguments string) (ToolResult, error) {
	provider, ok := r.nameMap[name]
	if !ok {
		return ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}, nil
	}
	return provider.CallTool(ctx, name, arguments)
}

type ToolGroups struct {
	Groups []ToolProvider
}

func ResolveTools(groups []string, registry *ToolRegistry) []llm.ToolDefinition {
	var defs []llm.ToolDefinition
	for _, g := range groups {
		if p, ok := registry.providers[g]; ok {
			defs = append(defs, p.ToolDefinitions()...)
		}
	}
	return defs
}

func FormatToolInput(name string, arguments string) string {
	args, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return fmt.Sprintf("Tool name: %s\nArguments: %s", name, arguments)
	}

	var parts []string
	i := 1
	for _, k := range sortedKeys(args) {
		parts = append(parts, fmt.Sprintf("Argument %d (%s): %v", i, k, args[k]))
		i++
	}
	return fmt.Sprintf("Tool name: %s\n%s", name, strings.Join(parts, "\n"))
}

func FormatToolOutput(name string, result string) string {
	return fmt.Sprintf("Tool name: %s\nResponse:\n%s", name, result)
}

func FormatToolDefinitions(defs []llm.ToolDefinition) string {
	var b strings.Builder
	for i, d := range defs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(d.Function.Name)
		b.WriteString(": ")
		b.WriteString(d.Function.Description)
		if d.Function.Parameters != nil {
			raw, _ := json.Marshal(d.Function.Parameters)
			b.WriteString("\nParameters: ")
			b.WriteString(string(raw))
		}
	}
	return b.String()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
