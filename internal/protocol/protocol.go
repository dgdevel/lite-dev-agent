package protocol

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

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
		return "thinking"
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
	case "thinking":
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
	Duration     time.Duration
	InputTokens  int64
	OutputTokens int64
}

func ParseHeader(line string) (Header, bool) {
	line = strings.TrimPrefix(line, "# ")
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
	line = strings.TrimPrefix(line, "# ")
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
		switch kv[0] {
		case "time":
			d, err := time.ParseDuration(kv[1])
			if err == nil {
				f.Duration = d
			}
		case "input_tokens":
			n, err := strconv.ParseInt(kv[1], 10, 64)
			if err == nil {
				f.InputTokens = n
			}
		case "output_tokens":
			n, err := strconv.ParseInt(kv[1], 10, 64)
			if err == nil {
				f.OutputTokens = n
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
	fmt.Fprintf(w, "# agent: %s | %s\n", agentName, bt)
}

func WriteFooter(w io.Writer, d time.Duration, inputTokens, outputTokens int64) {
	fmt.Fprintf(w, "# time: %s | input_tokens: %d | output_tokens: %d\n", formatDuration(d), inputTokens, outputTokens)
}

func WriteBlock(w io.Writer, agentName string, bt BlockType, content string) {
	WriteHeader(w, agentName, bt)
	fmt.Fprint(w, content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		fmt.Fprintln(w)
	}
}

func WriteWaitingInput(w io.Writer, agentName string) {
	fmt.Fprintf(w, "# agent: %s | waiting_user_input\n", agentName)
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
