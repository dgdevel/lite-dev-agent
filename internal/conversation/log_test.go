package conversation

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

func TestNewLog(t *testing.T) {
	dir := t.TempDir()
	log, err := NewLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if log.Path() == "" {
		t.Fatal("path should not be empty")
	}
	if !strings.Contains(log.Path(), ".lite-dev-agent/conversations/") {
		t.Fatalf("unexpected path: %s", log.Path())
	}
}

func TestLogWrite(t *testing.T) {
	dir := t.TempDir()
	log, err := NewLog(dir)
	if err != nil {
		t.Fatal(err)
	}

	n, err := log.Write([]byte("hello world\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Fatalf("wrote %d bytes", n)
	}

	log.Close()

	data, err := os.ReadFile(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("content: %q", string(data))
	}
}

func TestTeeWriter(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	tee := NewTeeWriter(&buf1, &buf2)

	tee.Write([]byte("hello"))

	if buf1.String() != "hello" {
		t.Fatalf("buf1: %q", buf1.String())
	}
	if buf2.String() != "hello" {
		t.Fatalf("buf2: %q", buf2.String())
	}
}

func TestParseBasic(t *testing.T) {
	input := `# agent: manager | system_prompt
You are the manager

# agent: manager | user_message
Hello

# agent: manager | agent_response
Hi there

# time: 5s | input_tokens: 10 | output_tokens: 20
`
	blocks, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	if blocks[0].Header.AgentName != "manager" || blocks[0].Header.BlockType != protocol.BlockSystemPrompt {
		t.Fatalf("block 0: %+v", blocks[0].Header)
	}
	if blocks[0].Content != "You are the manager" {
		t.Fatalf("block 0 content: %q", blocks[0].Content)
	}

	if blocks[1].Header.BlockType != protocol.BlockUserMessage {
		t.Fatalf("block 1 type: %d", blocks[1].Header.BlockType)
	}
	if blocks[1].Content != "Hello" {
		t.Fatalf("block 1 content: %q", blocks[1].Content)
	}

	if blocks[2].Header.BlockType != protocol.BlockAgentResponse {
		t.Fatalf("block 2 type: %d", blocks[2].Header.BlockType)
	}
	if blocks[2].Content != "Hi there" {
		t.Fatalf("block 2 content: %q", blocks[2].Content)
	}
	if blocks[2].Footer == nil {
		t.Fatal("block 2 should have footer")
	}
	if blocks[2].Footer.InputTokens != 10 || blocks[2].Footer.OutputTokens != 20 {
		t.Fatalf("footer: %+v", blocks[2].Footer)
	}
}

func TestParseWithToolCalls(t *testing.T) {
	input := `# agent: manager | system_prompt
You manage

# agent: manager | user_message
Search for x

# agent: manager | tools_input
Tool name: worker
Argument 1 (prompt): Search for x

# agent: worker | system_prompt
You search

# agent: worker | user_message
Search for x

# agent: worker | agent_response
Found x

# time: 2s | input_tokens: 50 | output_tokens: 10

# agent: manager | tools_output
Tool name: worker
Response:
Found x

# time: 3s | input_tokens: 0 | output_tokens: 0

# agent: manager | agent_response
Here are the results

# time: 5s | input_tokens: 100 | output_tokens: 50
`
	blocks, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 8 {
		t.Fatalf("expected 8 blocks, got %d", len(blocks))
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := `# agent: test | system_prompt
Hello

# agent: test | user_message
Hi

# agent: test | agent_response
Hey
`
	os.WriteFile(path, []byte(content), 0644)

	blocks, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
}

func TestReconstructHistory(t *testing.T) {
	blocks := []ParsedBlock{
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockSystemPrompt}, Content: "You manage"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockUserMessage}, Content: "Hello"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockAgentResponse}, Content: "Hi there"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockUserMessage}, Content: "How are you?"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockAgentResponse}, Content: "I'm fine"},
	}

	history := ReconstructFromBlocks(blocks, "You manage")
	if len(history.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(history.Messages))
	}
	if history.Messages[0].Role != "user" || history.Messages[0].Content != "Hello" {
		t.Fatalf("msg 0: %+v", history.Messages[0])
	}
	if history.Messages[1].Role != "assistant" || history.Messages[1].Content != "Hi there" {
		t.Fatalf("msg 1: %+v", history.Messages[1])
	}
}

func TestReconstructHistoryWithToolCalls(t *testing.T) {
	blocks := []ParsedBlock{
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockUserMessage}, Content: "Search"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockToolsInput}, Content: "Tool name: worker\nArgument 1 (prompt): find files"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockToolsOutput}, Content: "Tool name: worker\nResponse:\nfound 3 files"},
		{Header: protocol.Header{AgentName: "mgr", BlockType: protocol.BlockAgentResponse}, Content: "I found 3 files"},
	}

	history := ReconstructFromBlocks(blocks, "You manage")
	if len(history.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(history.Messages))
	}

	if history.Messages[1].Role != "assistant" || len(history.Messages[1].ToolCalls) != 1 {
		t.Fatalf("msg 1 should have tool calls: %+v", history.Messages[1])
	}
	if history.Messages[1].ToolCalls[0].Function.Name != "worker" {
		t.Fatalf("tool call name: %s", history.Messages[1].ToolCalls[0].Function.Name)
	}
	if history.Messages[2].Role != "tool" {
		t.Fatalf("msg 2 should be tool: %s", history.Messages[2].Role)
	}
	if history.Messages[2].Content != "found 3 files" {
		t.Fatalf("tool content: %q", history.Messages[2].Content)
	}
}

func TestParseToolInput(t *testing.T) {
	name, args := parseToolInput("Tool name: searcher\nArgument 1 (prompt): find files")
	if name != "searcher" {
		t.Fatalf("name: %s", name)
	}
	if !strings.Contains(args, "find files") {
		t.Fatalf("args: %s", args)
	}
}

func TestParseToolOutputContent(t *testing.T) {
	content := parseToolOutputContent("Tool name: worker\nResponse:\ndone")
	if content != "done" {
		t.Fatalf("content: %q", content)
	}
}

func TestEscapeJSON(t *testing.T) {
	if escapeJSON(`hello "world"`) != `hello \"world\"` {
		t.Fatalf("escapeJSON failed")
	}
	if escapeJSON("line1\nline2") != `line1\nline2` {
		t.Fatalf("escapeJSON newline failed")
	}
}

func TestOpenLogAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("existing\n"), 0644)

	log, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Write([]byte("appended\n"))
	log.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\nappended\n" {
		t.Fatalf("content: %q", string(data))
	}
}
