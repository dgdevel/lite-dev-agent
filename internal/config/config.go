package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type BlockStyleConfig struct {
	Color string `yaml:"color"`
	Bold  bool   `yaml:"bold"`
}

type Config struct {
	LLMs     []LLMConfig                  `yaml:"llms"`
	MCPs     []MCPConfig                  `yaml:"mcp"`
	Agents   []AgentConfig                `yaml:"agents"`
	Timeouts TimeoutConfig                `yaml:"timeouts"`
	Blocks   map[string]BlockStyleConfig  `yaml:"blocks"`
}

type MCPConfig struct {
	Name     string            `yaml:"name"`
	Prefix   string            `yaml:"prefix"`
	Type     string            `yaml:"type"`
	Command  string            `yaml:"command"`
	URL      string            `yaml:"url"`
	Headers  map[string]string `yaml:"headers"`
	Allow    []string          `yaml:"allow"`
	Deny     []string          `yaml:"deny"`
}

type LLMConfig struct {
	Name      string            `yaml:"name"`
	Default   bool              `yaml:"default"`
	APIBase   string            `yaml:"api_base"`
	Model     string            `yaml:"model"`
	APIKey    string            `yaml:"api_key"`
	Headers   map[string]string `yaml:"headers"`
	MaxTokens int               `yaml:"max_tokens"`
}

type AgentConfig struct {
	Name        string `yaml:"name"`
	Default     bool   `yaml:"default"`
	LLM         string `yaml:"llm"`
	Tools       string `yaml:"tools"`
	Expose      string `yaml:"expose"`
	SystemPrompt string `yaml:"system_prompt"`
}

type TimeoutConfig struct {
	LLMRequest     string `yaml:"llm_request"`
	ToolExecution  string `yaml:"tool_execution"`
}

func (t *TimeoutConfig) LLMRequestDuration() time.Duration {
	if t.LLMRequest == "" {
		return 30 * time.Minute
	}
	d, err := time.ParseDuration(t.LLMRequest)
	if err != nil {
		return 30 * time.Minute
	}
	return d
}

func (t *TimeoutConfig) ToolExecutionDuration() time.Duration {
	if t.ToolExecution == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(t.ToolExecution)
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

func Load(rootPath string) (*Config, error) {
	configPath := filepath.Join(rootPath, ".lite-dev-agent", "config.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", configPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.LLMs) == 0 {
		return fmt.Errorf("no llms defined")
	}
	if len(c.Agents) == 0 {
		return fmt.Errorf("no agents defined")
	}

	defaultLLMs := 0
	llmNames := make(map[string]bool)
	for _, l := range c.LLMs {
		if l.Name == "" {
			return fmt.Errorf("llm missing name")
		}
		if l.APIBase == "" {
			return fmt.Errorf("llm %q missing api_base", l.Name)
		}
		if llmNames[l.Name] {
			return fmt.Errorf("duplicate llm name %q", l.Name)
		}
		llmNames[l.Name] = true
		if l.Default {
			defaultLLMs++
		}
	}
	if defaultLLMs != 1 {
		return fmt.Errorf("expected exactly 1 default llm, got %d", defaultLLMs)
	}

	defaultAgents := 0
	agentNames := make(map[string]bool)
	for _, a := range c.Agents {
		if a.Name == "" {
			return fmt.Errorf("agent missing name")
		}
		if a.SystemPrompt == "" {
			return fmt.Errorf("agent %q missing system_prompt", a.Name)
		}
		if a.Tools == "" {
			return fmt.Errorf("agent %q missing tools", a.Name)
		}
		if agentNames[a.Name] {
			return fmt.Errorf("duplicate agent name %q", a.Name)
		}
		agentNames[a.Name] = true
		if a.LLM != "" && !llmNames[a.LLM] {
			return fmt.Errorf("agent %q references unknown llm %q", a.Name, a.LLM)
		}
		if a.Default {
			defaultAgents++
		}
	}
	if defaultAgents != 1 {
		return fmt.Errorf("expected exactly 1 default agent, got %d", defaultAgents)
	}

	mcpNames := make(map[string]bool)
	for _, m := range c.MCPs {
		if m.Name == "" {
			return fmt.Errorf("mcp entry missing name")
		}
		if m.Type != "stdio" && m.Type != "http" {
			return fmt.Errorf("mcp %q: type must be \"stdio\" or \"http\", got %q", m.Name, m.Type)
		}
		if m.Type == "stdio" && m.Command == "" {
			return fmt.Errorf("mcp %q: stdio type requires command", m.Name)
		}
		if m.Type == "http" && m.URL == "" {
			return fmt.Errorf("mcp %q: http type requires url", m.Name)
		}
		if mcpNames[m.Name] {
			return fmt.Errorf("duplicate mcp name %q", m.Name)
		}
		mcpNames[m.Name] = true
	}

	return nil
}

func (c *Config) DefaultLLM() *LLMConfig {
	for i := range c.LLMs {
		if c.LLMs[i].Default {
			return &c.LLMs[i]
		}
	}
	return nil
}

func (c *Config) DefaultAgent() *AgentConfig {
	for i := range c.Agents {
		if c.Agents[i].Default {
			return &c.Agents[i]
		}
	}
	return nil
}

func (c *Config) FindLLM(name string) *LLMConfig {
	for i := range c.LLMs {
		if c.LLMs[i].Name == name {
			return &c.LLMs[i]
		}
	}
	return nil
}

func (c *Config) FindAgent(name string) *AgentConfig {
	for i := range c.Agents {
		if c.Agents[i].Name == name {
			return &c.Agents[i]
		}
	}
	return nil
}

func (c *Config) FindMCP(name string) *MCPConfig {
	for i := range c.MCPs {
		if c.MCPs[i].Name == name {
			return &c.MCPs[i]
		}
	}
	return nil
}

func (c *Config) ExposedAgents() []*AgentConfig {
	var exposed []*AgentConfig
	for i := range c.Agents {
		if c.Agents[i].Expose != "" {
			exposed = append(exposed, &c.Agents[i])
		}
	}
	return exposed
}

func (a *AgentConfig) ToolList() []string {
	var tools []string
	for _, t := range splitCSV(a.Tools) {
		tools = append(tools, t)
	}
	return tools
}

func (a *AgentConfig) ResolveLLM(cfg *Config) *LLMConfig {
	if a.LLM != "" {
		if l := cfg.FindLLM(a.LLM); l != nil {
			return l
		}
	}
	return cfg.DefaultLLM()
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			part = trimSpaces(part)
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	return result
}

func trimSpaces(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
