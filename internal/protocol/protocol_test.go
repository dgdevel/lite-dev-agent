package protocol

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBlockTypeString(t *testing.T) {
	cases := []struct {
		bt   BlockType
		want string
	}{
		{BlockSystemPrompt, "system_prompt"},
		{BlockUserMessage, "user_message"},
		{BlockAgentResponse, "agent_response"},
		{BlockToolsInput, "tools_input"},
		{BlockToolsOutput, "tools_output"},
		{BlockThinking, "agent_thinking"},
		{BlockWaitingInput, "waiting_user_input"},
	}
	for _, c := range cases {
		if c.bt.String() != c.want {
			t.Errorf("BlockType(%d).String() = %q, want %q", c.bt, c.bt.String(), c.want)
		}
	}
}

func TestParseBlockType(t *testing.T) {
	_, ok := ParseBlockType("nonexistent")
	if ok {
		t.Fatal("ParseBlockType should return false for unknown type")
	}

	bt, ok := ParseBlockType("agent_response")
	if !ok || bt != BlockAgentResponse {
		t.Fatal("ParseBlockType(agent_response) failed")
	}
}

func TestParseHeader(t *testing.T) {
	h, ok := ParseHeader("#! agent: manager | level: 0 | system_prompt")
	if !ok {
		t.Fatal("ParseHeader failed")
	}
	if h.AgentName != "manager" {
		t.Fatalf("agent name: got %q", h.AgentName)
	}
	if h.Level != 0 {
		t.Fatalf("level: got %d", h.Level)
	}
	if h.BlockType != BlockSystemPrompt {
		t.Fatalf("block type: got %d", h.BlockType)
	}
}

func TestParseHeaderAllTypes(t *testing.T) {
	cases := []struct {
		line string
		bt   BlockType
	}{
		{"#! agent: mgr | level: 0 | system_prompt", BlockSystemPrompt},
		{"#! agent: mgr | level: 0 | user_message", BlockUserMessage},
		{"#! agent: mgr | level: 0 | agent_response", BlockAgentResponse},
		{"#! agent: mgr | level: 0 | tools_input", BlockToolsInput},
		{"#! agent: mgr | level: 0 | tools_output", BlockToolsOutput},
		{"#! agent: mgr | level: 0 | agent_thinking", BlockThinking},
		{"#! agent: mgr | level: 0 | waiting_user_input", BlockWaitingInput},
	}
	for _, c := range cases {
		h, ok := ParseHeader(c.line)
		if !ok {
			t.Errorf("ParseHeader(%q) not ok", c.line)
			continue
		}
		if h.BlockType != c.bt {
			t.Errorf("ParseHeader(%q) type = %d, want %d", c.line, h.BlockType, c.bt)
		}
	}
}

func TestParseHeaderInvalid(t *testing.T) {
	cases := []string{
		"not a header",
		"#! agent: ",
		"#! agent: x | invalid_type",
		"#! foo: bar | system_prompt",
		"",
	}
	for _, c := range cases {
		_, ok := ParseHeader(c)
		if ok {
			t.Errorf("ParseHeader(%q) should fail", c)
		}
	}
}

func TestParseFooter(t *testing.T) {
	f, ok := ParseFooter("#! time: 1m32s")
	if !ok {
		t.Fatal("ParseFooter failed")
	}
	if f.Duration != 1*time.Minute+32*time.Second {
		t.Fatalf("duration: got %v", f.Duration)
	}
}

func TestParseFooterSeconds(t *testing.T) {
	f, ok := ParseFooter("#! time: 45s")
	if !ok {
		t.Fatal("ParseFooter failed")
	}
	if f.Duration != 45*time.Second {
		t.Fatalf("duration: got %v", f.Duration)
	}
}

func TestParseFooterInvalid(t *testing.T) {
	cases := []string{
		"#! agent: x | system_prompt",
		"not a footer",
		"",
	}
	for _, c := range cases {
		_, ok := ParseFooter(c)
		if ok {
			t.Errorf("ParseFooter(%q) should fail", c)
		}
	}
}

func TestIsHeader(t *testing.T) {
	if !IsHeader("#! agent: x | level: 0 | system_prompt") {
		t.Fatal("should be header")
	}
	if IsHeader("not a header") {
		t.Fatal("should not be header")
	}
}

func TestIsFooter(t *testing.T) {
	if !IsFooter("#! time: 1s") {
		t.Fatal("should be footer")
	}
	if IsFooter("#! agent: x | system_prompt") {
		t.Fatal("should not be footer")
	}
}

func TestWriteHeader(t *testing.T) {
	var buf bytes.Buffer
	WriteHeader(&buf, "manager", 0, BlockSystemPrompt)
	if buf.String() != "#! agent: manager | level: 0 | system_prompt\n" {
		t.Fatalf("unexpected: %q", buf.String())
	}
}

func TestWriteHeaderNestedLevel(t *testing.T) {
	var buf bytes.Buffer
	WriteHeader(&buf, "worker", 2, BlockAgentResponse)
	if buf.String() != "#! agent: worker | level: 2 | agent_response\n" {
		t.Fatalf("unexpected: %q", buf.String())
	}
}

func TestWriteFooter(t *testing.T) {
	var buf bytes.Buffer
	WriteFooter(&buf, 92*time.Second)
	got := buf.String()
	want := "#! time: 1m32s\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteFooterHours(t *testing.T) {
	var buf bytes.Buffer
	WriteFooter(&buf, 2*time.Hour+30*time.Minute)
	got := buf.String()
	if !strings.Contains(got, "2h30m0s") {
		t.Fatalf("expected hours in footer, got %q", got)
	}
}

func TestWriteFooterSecondsOnly(t *testing.T) {
	var buf bytes.Buffer
	WriteFooter(&buf, 5*time.Second)
	got := buf.String()
	if !strings.Contains(got, "5s") {
		t.Fatalf("expected seconds in footer, got %q", got)
	}
}

func TestWriteBlock(t *testing.T) {
	var buf bytes.Buffer
	WriteBlock(&buf, "manager", 0, BlockAgentResponse, "Hello world")
	got := buf.String()
	if !strings.HasPrefix(got, "#! agent: manager | level: 0 | agent_response\n") {
		t.Fatalf("missing header: %q", got)
	}
	if !strings.Contains(got, "Hello world\n") {
		t.Fatalf("missing content: %q", got)
	}
}

func TestWriteBlockNoTrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	WriteBlock(&buf, "mgr", 0, BlockAgentResponse, "content")
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatal("block should end with newline")
	}
}

func TestWriteBlockEmptyContent(t *testing.T) {
	var buf bytes.Buffer
	WriteBlock(&buf, "mgr", 0, BlockAgentResponse, "")
	lines := strings.Count(buf.String(), "\n")
	if lines != 1 {
		t.Fatalf("expected 1 line (header only), got %d lines: %q", lines, buf.String())
	}
}

func TestWriteWaitingInput(t *testing.T) {
	var buf bytes.Buffer
	WriteWaitingInput(&buf, "manager", 0)
	got := buf.String()
	if got != "#! agent: manager | level: 0 | waiting_user_input\n" {
		t.Fatalf("got %q", got)
	}
}

func TestOutputFilterAll(t *testing.T) {
	f := NewOutputFilter("")
	for _, bt := range []BlockType{BlockSystemPrompt, BlockUserMessage, BlockAgentResponse, BlockToolsInput, BlockToolsOutput, BlockThinking} {
		if !f.Enabled(bt) {
			t.Fatalf("filter should enable %s when empty", bt)
		}
	}
}

func TestOutputFilterSpecific(t *testing.T) {
	f := NewOutputFilter("system_prompt,agent_response")
	if !f.Enabled(BlockSystemPrompt) {
		t.Fatal("system_prompt should be enabled")
	}
	if !f.Enabled(BlockAgentResponse) {
		t.Fatal("agent_response should be enabled")
	}
	if f.Enabled(BlockToolsInput) {
		t.Fatal("tools_input should be disabled")
	}
	if f.Enabled(BlockThinking) {
		t.Fatal("agent_thinking should be disabled")
	}
}

func TestOutputFilterWithSpaces(t *testing.T) {
	f := NewOutputFilter("  system_prompt , agent_response  ")
	if !f.Enabled(BlockSystemPrompt) || !f.Enabled(BlockAgentResponse) {
		t.Fatal("spaces should be trimmed")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{92 * time.Second, "1m32s"},
		{45 * time.Second, "45s"},
		{2*time.Hour + 30*time.Minute + 5*time.Second, "2h30m5s"},
		{0, "0s"},
	}
	for _, c := range cases {
		got := formatDuration(c.d)
		if got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestColorWriterHeader(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | system_prompt\n"))
	cw.Flush()
	if !strings.Contains(buf.String(), ansiYellow) {
		t.Fatalf("header should be yellow: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "system_prompt") {
		t.Fatalf("should contain header text: %q", buf.String())
	}
}

func TestColorWriterFooter(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | user_message\n"))
	cw.Write([]byte("#! time: 5s\n"))
	cw.Flush()
	if !strings.Contains(buf.String(), ansiYellow+"#! time:") {
		t.Fatalf("footer should be yellow: %q", buf.String())
	}
}

func TestColorWriterUserMessage(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | user_message\n"))
	cw.Write([]byte("Hello world\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiYellow+"Hello world"+ansiReset) {
		t.Fatalf("user_message content should be yellow: %q", got)
	}
}

func TestColorWriterAgentResponse(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | agent_response\n"))
	cw.Write([]byte("The answer is 42\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiWhite+"The answer is 42"+ansiReset) {
		t.Fatalf("agent_response content should be white: %q", got)
	}
}

func TestColorWriterThinking(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | agent_thinking\n"))
	cw.Write([]byte("hmm let me think\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiLightRed+"hmm let me think"+ansiReset) {
		t.Fatalf("thinking content should be light red: %q", got)
	}
}

func TestColorWriterToolsInput(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | tools_input\n"))
	cw.Write([]byte("Tool name: search\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiLightGreen+"Tool name: search"+ansiReset) {
		t.Fatalf("tools_input content should be light green: %q", got)
	}
}

func TestColorWriterToolsOutput(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | tools_output\n"))
	cw.Write([]byte("Response: found 3 items\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiLightGreen+"Response: found 3 items"+ansiReset) {
		t.Fatalf("tools_output content should be light green: %q", got)
	}
}

func TestColorWriterWaitingInput(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	cw.Write([]byte("#! agent: mgr | level: 0 | waiting_user_input\n"))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, ansiYellow) {
		t.Fatalf("waiting_user_input should be yellow: %q", got)
	}
}

func TestColorWriterNoColorPassthrough(t *testing.T) {
	var buf bytes.Buffer
	cw := NewColorWriter(&buf)
	input := "#! agent: mgr | level: 0 | system_prompt\nHello\n"
	cw.Write([]byte(input))
	cw.Flush()
	got := buf.String()
	if !strings.Contains(got, "Hello") {
		t.Fatalf("content should be present: %q", got)
	}
	if !strings.Contains(got, ansiReset) {
		t.Fatalf("should have reset codes: %q", got)
	}
}
