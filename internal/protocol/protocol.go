package protocol

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const prefix = "#! "

type BlockType int

const (
	BlockSystemPrompt BlockType = iota
	BlockUserMessage
	BlockAgentResponse
	BlockToolsInput
	BlockToolsOutput
	BlockThinking
	BlockWaitingInput
)

func (b BlockType) String() string {
	switch b {
	case BlockSystemPrompt:
		return "system_prompt"
	case BlockUserMessage:
		return "user_message"
	case BlockAgentResponse:
		return "agent_response"
	case BlockToolsInput:
		return "tools_input"
	case BlockToolsOutput:
		return "tools_output"
	case BlockThinking:
		return "agent_thinking"
	case BlockWaitingInput:
		return "waiting_user_input"
	default:
		return "unknown"
	}
}

func ParseBlockType(s string) (BlockType, bool) {
	switch s {
	case "system_prompt":
		return BlockSystemPrompt, true
	case "user_message":
		return BlockUserMessage, true
	case "agent_response":
		return BlockAgentResponse, true
	case "tools_input":
		return BlockToolsInput, true
	case "tools_output":
		return BlockToolsOutput, true
	case "agent_thinking":
		return BlockThinking, true
	case "waiting_user_input":
		return BlockWaitingInput, true
	default:
		return -1, false
	}
}

type Header struct {
	AgentName string
	BlockType BlockType
}

type Footer struct {
	Duration time.Duration
}

func ParseHeader(line string) (Header, bool) {
	line = strings.TrimPrefix(line, prefix)
	parts := strings.SplitN(line, " | ", 2)
	if len(parts) != 2 {
		return Header{}, false
	}

	agentPart := parts[0]
	blockPart := parts[1]

	if !strings.HasPrefix(agentPart, "agent: ") {
		return Header{}, false
	}
	agentName := strings.TrimPrefix(agentPart, "agent: ")

	bt, ok := ParseBlockType(blockPart)
	if !ok {
		return Header{}, false
	}

	return Header{AgentName: agentName, BlockType: bt}, true
}

func ParseFooter(line string) (Footer, bool) {
	line = strings.TrimPrefix(line, prefix)
	if !strings.HasPrefix(line, "time:") {
		return Footer{}, false
	}

	var f Footer
	parts := strings.Split(line, " | ")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		kv := strings.SplitN(p, ": ", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "time" {
			d, err := time.ParseDuration(kv[1])
			if err == nil {
				f.Duration = d
			}
		}
	}
	return f, true
}

func IsHeader(line string) bool {
	_, ok := ParseHeader(line)
	return ok
}

func IsFooter(line string) bool {
	_, ok := ParseFooter(line)
	return ok
}

type OutputFilter struct {
	enabled map[BlockType]bool
	all     bool
}

func NewOutputFilter(filter string) *OutputFilter {
	f := &OutputFilter{
		enabled: make(map[BlockType]bool),
		all:     filter == "",
	}
	if f.all {
		return f
	}
	for _, s := range splitCSV(filter) {
		if bt, ok := ParseBlockType(s); ok {
			f.enabled[bt] = true
		}
	}
	return f
}

func (f *OutputFilter) Enabled(bt BlockType) bool {
	if f.all {
		return true
	}
	return f.enabled[bt]
}

func WriteHeader(w io.Writer, agentName string, bt BlockType) {
	fmt.Fprintf(w, "%sagent: %s | %s\n", prefix, agentName, bt)
}

func WriteFooter(w io.Writer, d time.Duration) {
	fmt.Fprintf(w, "%stime: %s\n", prefix, formatDuration(d))
}

func WriteBlock(w io.Writer, agentName string, bt BlockType, content string) {
	WriteHeader(w, agentName, bt)
	fmt.Fprint(w, content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		fmt.Fprintln(w)
	}
}

func WriteWaitingInput(w io.Writer, agentName string) {
	fmt.Fprintf(w, "%sagent: %s | waiting_user_input\n", prefix, agentName)
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func splitCSV(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	return result
}

const (
	ansiReset      = "\033[0m"
	ansiYellow     = "\033[33m"
	ansiWhite      = "\033[37m"
	ansiLightRed   = "\033[91m"
	ansiLightGreen = "\033[92m"
)

func blockColor(bt BlockType) string {
	switch bt {
	case BlockAgentResponse, BlockUserMessage:
		return ansiWhite
	case BlockThinking:
		return ansiLightRed
	case BlockToolsInput, BlockToolsOutput:
		return ansiLightGreen
	default:
		return ansiYellow
	}
}

type ColorWriter struct {
	w       io.Writer
	current BlockType
	buf     []byte
}

func NewColorWriter(w io.Writer) *ColorWriter {
	return &ColorWriter{w: w}
}

func (c *ColorWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			line := string(c.buf)
			c.writeColoredLine(line)
			c.buf = c.buf[:0]
			n, err := c.w.Write([]byte{'\n'})
			if err != nil {
				return 0, err
			}
			_ = n
			continue
		}
		c.buf = append(c.buf, b)
	}
	return len(p), nil
}

func (c *ColorWriter) writeColoredLine(line string) {
	if header, ok := ParseHeader(line); ok {
		c.current = header.BlockType
		c.w.Write([]byte(ansiYellow + line + ansiReset))
		return
	}
	if IsFooter(line) {
		c.w.Write([]byte(ansiYellow + line + ansiReset))
		return
	}
	if strings.HasPrefix(line, prefix) && strings.Contains(line, "waiting_user_input") {
		c.w.Write([]byte(ansiYellow + line + ansiReset))
		return
	}
	color := blockColor(c.current)
	if color != "" {
		c.w.Write([]byte(color + line + ansiReset))
	} else {
		c.w.Write([]byte(line))
	}
}

func (c *ColorWriter) Flush() {
	if len(c.buf) > 0 {
		line := string(c.buf)
		c.writeColoredLine(line)
		c.buf = c.buf[:0]
	}
}
