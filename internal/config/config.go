package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func xdgConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "lite-dev-agent", "config.yml")
}

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

type InitialToolCall struct {
	Tool      string                 `yaml:"tool"`
	Arguments map[string]interface{} `yaml:"arguments"`
}

type AgentConfig struct {
	Name             string            `yaml:"name"`
	Default          bool              `yaml:"default"`
	LLM              string            `yaml:"llm"`
	Tools            string            `yaml:"tools"`
	Expose           string            `yaml:"expose"`
	SystemPrompt     string            `yaml:"system_prompt"`
	InitialToolCalls []InitialToolCall `yaml:"initial_tool_calls"`
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
	var global *Config
	globalPath := xdgConfigPath()
	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil {
			var g Config
			if err := yaml.Unmarshal(data, &g); err != nil {
				return nil, fmt.Errorf("parsing global config %s: %w", globalPath, err)
			}
			global = &g
		}
	}

	localPath := filepath.Join(rootPath, ".lite-dev-agent", "config.yml")
	localData, localErr := os.ReadFile(localPath)
	var local *Config
	if localErr == nil {
		var l Config
		if err := yaml.Unmarshal(localData, &l); err != nil {
			return nil, fmt.Errorf("parsing local config %s: %w", localPath, err)
		}
		local = &l
	}

	if global == nil && local == nil {
		return nil, fmt.Errorf("no config found (checked %s and %s)", globalPath, localPath)
	}

	var cfg Config
	if global != nil && local != nil {
		cfg = *mergeConfigs(global, local)
	} else if global != nil {
		cfg = *global
	} else {
		cfg = *local
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func mergeConfigs(global, local *Config) *Config {
	result := *global
	result.LLMs = mergeLLMs(global.LLMs, local.LLMs)
	result.MCPs = mergeMCPs(global.MCPs, local.MCPs)
	result.Agents = mergeAgents(global.Agents, local.Agents)
	result.Timeouts = mergeTimeouts(global.Timeouts, local.Timeouts)
	result.Blocks = mergeBlocks(global.Blocks, local.Blocks)
	return &result
}

func mergeLLMs(global, local []LLMConfig) []LLMConfig {
	merged := make([]LLMConfig, len(global))
	copy(merged, global)
	localByName := make(map[string]int, len(local))
	for i, l := range local {
		localByName[l.Name] = i
	}
	for i, g := range global {
		if idx, ok := localByName[g.Name]; ok {
			merged[i] = local[idx]
			delete(localByName, g.Name)
		}
	}
	for _, l := range local {
		if _, ok := localByName[l.Name]; ok {
			merged = append(merged, l)
		}
	}
	return merged
}

func mergeMCPs(global, local []MCPConfig) []MCPConfig {
	merged := make([]MCPConfig, len(global))
	copy(merged, global)
	localByName := make(map[string]int, len(local))
	for i, l := range local {
		localByName[l.Name] = i
	}
	for i, g := range global {
		if idx, ok := localByName[g.Name]; ok {
			merged[i] = local[idx]
			delete(localByName, g.Name)
		}
	}
	for _, l := range local {
		if _, ok := localByName[l.Name]; ok {
			merged = append(merged, l)
		}
	}
	return merged
}

func mergeAgents(global, local []AgentConfig) []AgentConfig {
	merged := make([]AgentConfig, len(global))
	copy(merged, global)
	localByName := make(map[string]int, len(local))
	for i, l := range local {
		localByName[l.Name] = i
	}
	for i, g := range global {
		if idx, ok := localByName[g.Name]; ok {
			merged[i] = local[idx]
			delete(localByName, g.Name)
		}
	}
	for _, l := range local {
		if _, ok := localByName[l.Name]; ok {
			merged = append(merged, l)
		}
	}
	return merged
}

func mergeTimeouts(global, local TimeoutConfig) TimeoutConfig {
	result := global
	if local.LLMRequest != "" {
		result.LLMRequest = local.LLMRequest
	}
	if local.ToolExecution != "" {
		result.ToolExecution = local.ToolExecution
	}
	return result
}

func mergeBlocks(global, local map[string]BlockStyleConfig) map[string]BlockStyleConfig {
	result := make(map[string]BlockStyleConfig, len(global)+len(local))
	for k, v := range global {
		result[k] = v
	}
	for k, v := range local {
		result[k] = v
	}
	return result
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
