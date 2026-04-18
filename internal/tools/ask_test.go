package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
)

func mockReadInput(responses ...string) func() (string, error) {
	idx := 0
	return func() (string, error) {
		if idx >= len(responses) {
			return "", context.DeadlineExceeded
		}
		r := responses[idx]
		idx++
		return r, nil
	}
}

func newTestAskProvider(readInput func() (string, error)) *AskProvider {
	return NewAskProvider("test-agent", &bytes.Buffer{}, readInput)
}

func newTestAskProviderWithWriter(readInput func() (string, error)) (*AskProvider, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return NewAskProvider("test-agent", buf, readInput), buf
}

func TestAskToolDefinitions(t *testing.T) {
	p := newTestAskProvider(mockReadInput())
	defs := p.ToolDefinitions()

	if len(defs) != 3 {
		t.Fatalf("expected 3 tool definitions, got %d", len(defs))
	}

	names := []string{"ask_open_ended", "ask_multiple_choice", "ask_exec"}
	for i, name := range names {
		if defs[i].Function.Name != name {
			t.Errorf("definition %d: expected name %q, got %q", i, name, defs[i].Function.Name)
		}
	}
}

func TestAskOpenEnded(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("my answer"))

	result, err := p.CallTool(context.Background(), "ask_open_ended", `{"question":"What is your name?"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}

	if result.Content != "my answer" {
		t.Errorf("expected 'my answer', got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "What is your name?") {
		t.Errorf("expected question in output, got: %s", output)
	}
	if !strings.Contains(output, "waiting_user_input") {
		t.Errorf("expected waiting_user_input in output, got: %s", output)
	}
}

func TestAskOpenEnded_MissingQuestion(t *testing.T) {
	p := newTestAskProvider(mockReadInput())

	result, err := p.CallTool(context.Background(), "ask_open_ended", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for missing question")
	}
}

func TestAskMultipleChoice_Single(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("2"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick a color",
		"options": ["Red", "Green", "Blue"],
		"type": "single"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if result.Content != "Green" {
		t.Errorf("expected 'Green', got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "1) Red") {
		t.Errorf("expected numbered options in output, got: %s", output)
	}
	if !strings.Contains(output, "3) Blue") {
		t.Errorf("expected numbered options in output, got: %s", output)
	}
}

func TestAskMultipleChoice_Multi(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("1, 3"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick colors",
		"options": ["Red", "Green", "Blue"],
		"type": "multi"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if result.Content != "Red, Blue" {
		t.Errorf("expected 'Red, Blue', got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "waiting_user_input") {
		t.Errorf("expected waiting_user_input in output, got: %s", output)
	}
}

func TestAskMultipleChoice_SingleMultipleSelection(t *testing.T) {
	p := newTestAskProvider(mockReadInput("1, 2"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": ["A", "B"],
		"type": "single"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error when selecting multiple in single mode")
	}
}

func TestAskMultipleChoice_InvalidNumber(t *testing.T) {
	p := newTestAskProvider(mockReadInput("5"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": ["A", "B"],
		"type": "single"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid option number")
	}
}

func TestAskMultipleChoice_OpenEndSelected(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("3", "my custom answer"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": ["A", "B"],
		"type": "single",
		"allow_open_end_response": true
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if result.Content != "my custom answer" {
		t.Errorf("expected 'my custom answer', got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "3) Type your own response") {
		t.Errorf("expected open end option in output, got: %s", output)
	}
	if !strings.Contains(output, "Type your response:") {
		t.Errorf("expected 'Type your response:' in output, got: %s", output)
	}
}

func TestAskMultipleChoice_OpenEndNotSelected(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("1"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": ["A", "B"],
		"type": "single",
		"allow_open_end_response": true
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if result.Content != "A" {
		t.Errorf("expected 'A', got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "3) Type your own response") {
		t.Errorf("expected open end option in output, got: %s", output)
	}
}

func TestAskMultipleChoice_NoValidNumbers(t *testing.T) {
	p := newTestAskProvider(mockReadInput("abc"))

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": ["A", "B"],
		"type": "single"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid input")
	}
}

func TestAskMultipleChoice_EmptyOptions(t *testing.T) {
	p := newTestAskProvider(mockReadInput())

	result, err := p.CallTool(context.Background(), "ask_multiple_choice", `{
		"question": "Pick one",
		"options": [],
		"type": "single"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for empty options")
	}
}

func TestAskExec_Approved(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("y"))

	result, err := p.CallTool(context.Background(), "ask_exec", `{
		"cmdline": "echo hello world",
		"timeout": 5
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if !strings.Contains(result.Content, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "Execute command: echo hello world") {
		t.Errorf("expected command prompt in output, got: %s", output)
	}
	if !strings.Contains(output, "Allow execution? [y/n]") {
		t.Errorf("expected confirmation prompt in output, got: %s", output)
	}
}

func TestAskExec_Denied(t *testing.T) {
	p, buf := newTestAskProviderWithWriter(mockReadInput("n", "I prefer a different approach"))

	result, err := p.CallTool(context.Background(), "ask_exec", `{
		"cmdline": "rm -rf /",
		"timeout": 5
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if !strings.HasPrefix(result.Content, "DENIED:") {
		t.Errorf("expected DENIED prefix, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "I prefer a different approach") {
		t.Errorf("expected denial reason in content, got %q", result.Content)
	}

	output := buf.String()
	if !strings.Contains(output, "Command denied. Type your response:") {
		t.Errorf("expected denial prompt in output, got: %s", output)
	}
}

func TestAskExec_Timeout(t *testing.T) {
	p, _ := newTestAskProviderWithWriter(mockReadInput("yes"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := p.CallTool(ctx, "ask_exec", `{
		"cmdline": "sleep 10",
		"timeout": 1
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for timed out command")
	}

	if !strings.Contains(result.Content, "timed out") {
		t.Errorf("expected timeout message, got %q", result.Content)
	}
}

func TestAskExec_DefaultTimeout(t *testing.T) {
	p, _ := newTestAskProviderWithWriter(mockReadInput("y"))

	start := time.Now()
	result, err := p.CallTool(context.Background(), "ask_exec", `{
		"cmdline": "echo fast"
	}`)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	if !strings.Contains(result.Content, "fast") {
		t.Errorf("expected 'fast' in output, got %q", result.Content)
	}

	if elapsed > 5*time.Second {
		t.Errorf("command took too long: %v", elapsed)
	}
}

func TestAskExec_CommandError(t *testing.T) {
	p, _ := newTestAskProviderWithWriter(mockReadInput("y"))

	result, err := p.CallTool(context.Background(), "ask_exec", `{
		"cmdline": "false"
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for failing command")
	}
}

func TestAskExec_MissingCmdline(t *testing.T) {
	p := newTestAskProvider(mockReadInput())

	result, err := p.CallTool(context.Background(), "ask_exec", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for missing cmdline")
	}
}

func TestAskUnknownTool(t *testing.T) {
	p := newTestAskProvider(mockReadInput())

	result, err := p.CallTool(context.Background(), "ask_unknown", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for unknown tool")
	}
}

func TestParseNumbers(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
	}{
		{"1", []int{1}},
		{"1, 2, 3", []int{1, 2, 3}},
		{" 3 , 1 ", []int{3, 1}},
		{"abc", nil},
		{"1, abc, 3", []int{1, 3}},
		{"", nil},
		{"  ", nil},
	}

	for _, tt := range tests {
		result := parseNumbers(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseNumbers(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("parseNumbers(%q): expected %v, got %v", tt.input, tt.expected, result)
			}
		}
	}
}

func TestIsAffirmative(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"y", true},
		{"Y", true},
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		{"n", false},
		{"no", false},
		{"", false},
		{"yeah", false},
		{" y ", true},
		{" yes ", true},
	}

	for _, tt := range tests {
		result := isAffirmative(tt.input)
		if result != tt.expected {
			t.Errorf("isAffirmative(%q): expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}

func TestAskProvider_ToolRegistry(t *testing.T) {
	p := newTestAskProvider(mockReadInput("test response"))
	registry := agent.NewToolRegistry()
	registry.Register("ask", p)

	tools := registry.ToolDefinitions()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools in registry, got %d", len(tools))
	}

	result, err := registry.CallTool(context.Background(), "ask_open_ended", `{"question":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "test response" {
		t.Errorf("expected 'test response', got %q", result.Content)
	}
}
