package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, dir, content string) {
	t.Helper()
	xdgDir := t.TempDir()
	orig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Cleanup(func() { os.Setenv("XDG_CONFIG_HOME", orig) })
	cfgDir := filepath.Join(dir, ".lite-dev-agent")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeGlobalTestConfig(t *testing.T, xdgDir, content string) {
	t.Helper()
	cfgDir := filepath.Join(xdgDir, "lite-dev-agent")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func withXDGConfigHome(t *testing.T, xdgDir string) {
	t.Helper()
	orig := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Cleanup(func() { os.Setenv("XDG_CONFIG_HOME", orig) })
}

func writeLocalConfig(t *testing.T, dir, content string) {
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

func TestLoadWithMCP(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
    command: "nixdevkit %s"
  - name: remote
    type: http
    url: http://localhost:8080/mcp
    headers:
      Authorization: Bearer token123
agents:
  - name: x
    default: true
    tools: devkit, remote
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPs) != 2 {
		t.Fatalf("expected 2 mcp entries, got %d", len(cfg.MCPs))
	}
	if cfg.MCPs[0].Name != "devkit" {
		t.Fatalf("expected devkit, got %s", cfg.MCPs[0].Name)
	}
	if cfg.MCPs[0].Type != "stdio" {
		t.Fatalf("expected stdio, got %s", cfg.MCPs[0].Type)
	}
	if cfg.MCPs[0].Command != "nixdevkit %s" {
		t.Fatalf("expected nixdevkit %%s, got %s", cfg.MCPs[0].Command)
	}
	if cfg.MCPs[1].Name != "remote" {
		t.Fatalf("expected remote, got %s", cfg.MCPs[1].Name)
	}
	if cfg.MCPs[1].URL != "http://localhost:8080/mcp" {
		t.Fatalf("unexpected url: %s", cfg.MCPs[1].URL)
	}
	if cfg.MCPs[1].Headers["Authorization"] != "Bearer token123" {
		t.Fatalf("unexpected headers: %v", cfg.MCPs[1].Headers)
	}
}

func TestFindMCP(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
    command: "nixdevkit %s"
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
	if m := cfg.FindMCP("devkit"); m == nil || m.Name != "devkit" {
		t.Fatal("FindMCP(devkit) failed")
	}
	if m := cfg.FindMCP("nonexistent"); m != nil {
		t.Fatal("FindMCP(nonexistent) should return nil")
	}
}

func TestValidateMCPMissingName(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - type: stdio
    command: "nixdevkit %s"
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for mcp missing name")
	}
}

func TestValidateMCPInvalidType(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: grpc
    command: "nixdevkit"
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid mcp type")
	}
}

func TestValidateMCPStdioMissingCommand(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for stdio missing command")
	}
}

func TestValidateMCPHTTPMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: remote
    type: http
agents:
  - name: x
    default: true
    tools: remote
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for http missing url")
	}
}

func TestValidateMCPDuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
    command: "cmd1"
  - name: devkit
    type: stdio
    command: "cmd2"
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for duplicate mcp name")
	}
}

func TestConfigWithMCPPrefix(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
    command: "nixdevkit %s"
    prefix: "fs_"
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
	if cfg.MCPs[0].Prefix != "fs_" {
		t.Fatalf("expected prefix fs_, got %q", cfg.MCPs[0].Prefix)
	}
}

func TestMCPAllowDeny(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: devkit
    type: stdio
    command: "nixdevkit %s"
    allow:
      - ls
      - read
      - grep
  - name: toolsrv
    type: http
    url: http://localhost:8080/mcp
    deny:
      - dangerous_tool
agents:
  - name: x
    default: true
    tools: devkit, toolsrv
    system_prompt: test
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPs[0].Allow) != 3 || cfg.MCPs[0].Allow[0] != "ls" {
		t.Fatalf("allow: %v", cfg.MCPs[0].Allow)
	}
	if len(cfg.MCPs[1].Deny) != 1 || cfg.MCPs[1].Deny[0] != "dangerous_tool" {
		t.Fatalf("deny: %v", cfg.MCPs[1].Deny)
	}
}

func TestBlocksConfig(t *testing.T) {
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
blocks:
  waiting_user_input:
    color: cyan
    bold: true
  agent_response:
    color: light_green
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Blocks) != 2 {
		t.Fatalf("expected 2 block overrides, got %d", len(cfg.Blocks))
	}
	ws := cfg.Blocks["waiting_user_input"]
	if ws.Color != "cyan" || !ws.Bold {
		t.Fatalf("waiting_user_input: got color=%q bold=%v", ws.Color, ws.Bold)
	}
	as := cfg.Blocks["agent_response"]
	if as.Color != "light_green" || as.Bold {
		t.Fatalf("agent_response: got color=%q bold=%v", as.Color, as.Bold)
	}
}

func TestBlocksConfigEmpty(t *testing.T) {
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
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Blocks) != 0 {
		t.Fatalf("expected 0 block overrides, got %d", len(cfg.Blocks))
	}
}

func TestLoadGlobalOnly(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultLLM().Name != "a" {
		t.Fatalf("expected default llm a, got %s", cfg.DefaultLLM().Name)
	}
}

func TestLoadLocalOnly(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeTestConfig(t, localDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultLLM().Name != "a" {
		t.Fatalf("expected default llm a, got %s", cfg.DefaultLLM().Name)
	}
}

func TestLoadNoConfigAtAll(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	_, err := Load(localDir)
	if err == nil {
		t.Fatal("expected error when no config found")
	}
}

func TestLoadMergeOverrideLLM(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
    model: global-model
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	writeTestConfig(t, localDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v2
    model: local-model
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultLLM().Model != "local-model" {
		t.Fatalf("expected local-model, got %s", cfg.DefaultLLM().Model)
	}
	if cfg.DefaultLLM().APIBase != "http://localhost/v2" {
		t.Fatalf("expected http://localhost/v2, got %s", cfg.DefaultLLM().APIBase)
	}
}

func TestLoadMergeAddsLLM(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: global-llm
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`)
	writeTestConfig(t, localDir, `
llms:
  - name: global-llm
    default: true
    api_base: http://localhost/v1
  - name: local-llm
    api_base: http://localhost/v2
agents:
  - name: x
    default: true
    llm: local-llm
    tools: devkit
    system_prompt: test
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.LLMs) != 2 {
		t.Fatalf("expected 2 llms, got %d", len(cfg.LLMs))
	}
	if cfg.FindLLM("local-llm") == nil {
		t.Fatal("expected to find local-llm")
	}
}

func TestLoadMergeOverrideAgent(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: global prompt
`)
	writeTestConfig(t, localDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: local prompt
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultAgent().SystemPrompt != "local prompt" {
		t.Fatalf("expected 'local prompt', got %q", cfg.DefaultAgent().SystemPrompt)
	}
}

func TestLoadMergeAddsAgent(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: global-agent
    default: true
    tools: devkit
    system_prompt: global
`)
	writeTestConfig(t, localDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: global-agent
    default: true
    tools: devkit
    system_prompt: global
  - name: local-agent
    tools: devkit
    expose: local worker
    system_prompt: local
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}
	if cfg.FindAgent("local-agent") == nil {
		t.Fatal("expected to find local-agent")
	}
}

func TestLoadMergeMCPs(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
mcp:
  - name: global-mcp
    type: stdio
    command: "global-cmd"
agents:
  - name: x
    default: true
    tools: global-mcp
    system_prompt: test
`)
	writeLocalConfig(t, localDir, `
mcp:
  - name: global-mcp
    type: stdio
    command: "local-cmd"
  - name: local-mcp
    type: http
    url: http://localhost:8080/mcp
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.MCPs) != 2 {
		t.Fatalf("expected 2 mcps, got %d", len(cfg.MCPs))
	}
	m := cfg.FindMCP("global-mcp")
	if m == nil || m.Command != "local-cmd" {
		t.Fatalf("expected global-mcp overridden to local-cmd, got %v", m)
	}
	if cfg.FindMCP("local-mcp") == nil {
		t.Fatal("expected to find local-mcp")
	}
}

func TestLoadMergeTimeouts(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
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
	writeLocalConfig(t, localDir, `
timeouts:
  llm_request: 5m
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Timeouts.LLMRequestDuration() != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", cfg.Timeouts.LLMRequestDuration())
	}
	if cfg.Timeouts.ToolExecutionDuration() != 15*time.Minute {
		t.Fatalf("expected 15m (from global), got %v", cfg.Timeouts.ToolExecutionDuration())
	}
}

func TestLoadMergeBlocks(t *testing.T) {
	xdgDir := t.TempDir()
	localDir := t.TempDir()
	withXDGConfigHome(t, xdgDir)
	writeGlobalTestConfig(t, xdgDir, `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
blocks:
  agent_response:
    color: red
  waiting_user_input:
    color: blue
`)
	writeLocalConfig(t, localDir, `
blocks:
  agent_response:
    color: green
    bold: true
`)
	cfg, err := Load(localDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Blocks) != 2 {
		t.Fatalf("expected 2 block overrides, got %d", len(cfg.Blocks))
	}
	ar := cfg.Blocks["agent_response"]
	if ar.Color != "green" || !ar.Bold {
		t.Fatalf("agent_response: got color=%q bold=%v", ar.Color, ar.Bold)
	}
	ws := cfg.Blocks["waiting_user_input"]
	if ws.Color != "blue" {
		t.Fatalf("waiting_user_input: expected blue, got %q", ws.Color)
	}
}

func TestLoadXDGFallback(t *testing.T) {
	orig := os.Getenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Cleanup(func() { os.Setenv("XDG_CONFIG_HOME", orig) })

	home := os.Getenv("HOME")
	fallbackDir := filepath.Join(home, ".config", "lite-dev-agent")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(home, ".config", "lite-dev-agent")) })

	configContent := `
llms:
  - name: a
    default: true
    api_base: http://localhost/v1
agents:
  - name: x
    default: true
    tools: devkit
    system_prompt: test
`
	if err := os.WriteFile(filepath.Join(fallbackDir, "config.yml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultLLM().Name != "a" {
		t.Fatalf("expected default llm a, got %s", cfg.DefaultLLM().Name)
	}
}
