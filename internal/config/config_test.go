package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, dir, content string) {
	t.Helper()
	cfgDir := filepath.Join(dir, ".lite-dev-agent")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: thinker
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.6-35B
  - name: quick
    default: true
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.5-9B

agents:
  - name: manager
    default: true
    llm: thinker
    tools: agents
    system_prompt: You are the manager
  - name: searcher
    tools: devkit
    expose: File system researcher
    system_prompt: You search files
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.LLMs) != 2 {
		t.Fatalf("expected 2 llms, got %d", len(cfg.LLMs))
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}

	dl := cfg.DefaultLLM()
	if dl == nil || dl.Name != "quick" {
		t.Fatalf("default llm: got %v", dl)
	}

	da := cfg.DefaultAgent()
	if da == nil || da.Name != "manager" {
		t.Fatalf("default agent: got %v", da)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `: [invalid`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidateNoLLMs(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
agents:
  - name: a
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for no llms")
	}
}

func TestValidateNoAgents(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: test
    default: true
    api_base: http://localhost/v1
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for no agents")
	}
}

func TestValidateMultipleDefaultLLMs(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
  - name: b
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for multiple default llms")
	}
}

func TestValidateNoDefaultAgent(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for no default agent")
	}
}

func TestValidateDuplicateLLMName(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: same
    default: true
    api_base: http://localhost/v1
  - name: same
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for duplicate llm name")
	}
}

func TestValidateDuplicateAgentName(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
  - name: x
    tools: online
    system_prompt: test2
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for duplicate agent name")
	}
}

func TestValidateUnknownLLMRef(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    llm: nonexistent
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for unknown llm ref")
	}
}

func TestValidateMissingAPIBase(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing api_base")
	}
}

func TestValidateMissingSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing system_prompt")
	}
}

func TestDefaultTimeouts(t *testing.T) {
	cfg := &Config{}
	if cfg.Timeouts.LLMRequestDuration() != 30*time.Minute {
		t.Fatalf("expected 30m default, got %v", cfg.Timeouts.LLMRequestDuration())
	}
	if cfg.Timeouts.ToolExecutionDuration() != 10*time.Minute {
		t.Fatalf("expected 10m default, got %v", cfg.Timeouts.ToolExecutionDuration())
	}
}

func TestCustomTimeouts(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
timeouts:
  llm_request: 45m
  tool_execution: 15m
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeouts.LLMRequestDuration() != 45*time.Minute {
		t.Fatalf("expected 45m, got %v", cfg.Timeouts.LLMRequestDuration())
	}
	if cfg.Timeouts.ToolExecutionDuration() != 15*time.Minute {
		t.Fatalf("expected 15m, got %v", cfg.Timeouts.ToolExecutionDuration())
	}
}

func TestFindLLM(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: thinker
    api_base: http://localhost/v1
  - name: quick
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l := cfg.FindLLM("thinker"); l == nil || l.Name != "thinker" {
		t.Fatal("FindLLM(thinker) failed")
	}
	if l := cfg.FindLLM("nonexistent"); l != nil {
		t.Fatal("FindLLM(nonexistent) should return nil")
	}
}

func TestFindAgent(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: manager
    default: true
    tools: agents
    system_prompt: test
  - name: searcher
    tools: devkit
    expose: searcher
    system_prompt: test2
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.FindAgent("searcher"); a == nil || a.Name != "searcher" {
		t.Fatal("FindAgent(searcher) failed")
	}
	if a := cfg.FindAgent("nonexistent"); a != nil {
		t.Fatal("FindAgent(nonexistent) should return nil")
	}
}

func TestExposedAgents(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: manager
    default: true
    tools: agents
    system_prompt: test
  - name: searcher
    tools: devkit
    expose: File researcher
    system_prompt: test2
  - name: online
    tools: online
    expose: Online researcher
    system_prompt: test3
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	exposed := cfg.ExposedAgents()
	if len(exposed) != 2 {
		t.Fatalf("expected 2 exposed agents, got %d", len(exposed))
	}
}

func TestToolList(t *testing.T) {
	a := &AgentConfig{Tools: "devkit, online"}
	tools := a.ToolList()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0] != "devkit" {
		t.Fatalf("expected devkit, got %s", tools[0])
	}
	if tools[1] != "online" {
		t.Fatalf("expected online, got %s", tools[1])
	}
}

func TestToolListSingle(t *testing.T) {
	a := &AgentConfig{Tools: "agents"}
	tools := a.ToolList()
	if len(tools) != 1 || tools[0] != "agents" {
		t.Fatalf("expected [agents], got %v", tools)
	}
}

func TestToolListWithSpaces(t *testing.T) {
	a := &AgentConfig{Tools: "  devkit ,  online , agents  "}
	tools := a.ToolList()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(tools), tools)
	}
	if tools[0] != "devkit" || tools[1] != "online" || tools[2] != "agents" {
		t.Fatalf("unexpected tools: %v", tools)
	}
}

func TestResolveLLM(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: thinker
    api_base: http://localhost/v1
  - name: quick
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    llm: thinker
    tools: devkit
    system_prompt: test
  - name: y
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	x := cfg.FindAgent("x")
	resolved := x.ResolveLLM(cfg)
	if resolved.Name != "thinker" {
		t.Fatalf("expected thinker, got %s", resolved.Name)
	}

	y := cfg.FindAgent("y")
	resolved = y.ResolveLLM(cfg)
	if resolved.Name != "quick" {
		t.Fatalf("expected quick (default), got %s", resolved.Name)
	}
}

func TestMaxTokens(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
    max_tokens: 32000
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultLLM().MaxTokens != 32000 {
		t.Fatalf("expected 32000, got %d", cfg.DefaultLLM().MaxTokens)
	}
}

func TestHeaders(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
    headers:
      Authorization: Bearer abc
      X-Custom: value
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.DefaultLLM()
	if l.Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("expected Bearer abc, got %s", l.Headers["Authorization"])
	}
	if l.Headers["X-Custom"] != "value" {
		t.Fatalf("expected value, got %s", l.Headers["X-Custom"])
	}
}
