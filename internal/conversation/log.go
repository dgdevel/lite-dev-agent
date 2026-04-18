package conversation

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type Log struct {
	file   *os.File
	writer *bufio.Writer
}

func NewLog(rootPath string) (*Log, error) {
	dir := filepath.Join(rootPath, ".lite-dev-agent", "conversations")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create conversations dir: %w", err)
	}

	filename := time.Now().Format("2006-01-02_15-04-05") + ".txt"
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	return &Log{
		file:   f,
		writer: bufio.NewWriter(f),
	}, nil
}

func OpenLog(path string) (*Log, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &Log{
		file:   f,
		writer: bufio.NewWriter(f),
	}, nil
}

func (l *Log) Writer() io.Writer {
	return &logWriter{log: l}
}

func (l *Log) Write(p []byte) (int, error) {
	n, err := l.writer.Write(p)
	if err != nil {
		return n, err
	}
	l.writer.Flush()
	return n, nil
}

func (l *Log) Close() error {
	l.writer.Flush()
	return l.file.Close()
}

func (l *Log) Path() string {
	return l.file.Name()
}

type logWriter struct {
	log *Log
}

func (w *logWriter) Write(p []byte) (int, error) {
	return w.log.Write(p)
}

type TeeWriter struct {
	writers []io.Writer
}

func NewTeeWriter(writers ...io.Writer) *TeeWriter {
	return &TeeWriter{writers: writers}
}

func (t *TeeWriter) Write(p []byte) (int, error) {
	for _, w := range t.writers {
		if _, err := w.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

type ParsedBlock struct {
	Header  protocol.Header
	Content string
	Footer  *protocol.Footer
}

func ParseFile(path string) ([]ParsedBlock, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open conversation log: %w", err)
	}
	defer f.Close()

	return Parse(f)
}

func Parse(r io.Reader) ([]ParsedBlock, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var blocks []ParsedBlock
	var current *ParsedBlock
	var contentLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if header, ok := protocol.ParseHeader(line); ok {
			if current != nil {
				current.Content = strings.TrimRight(strings.Join(contentLines, "\n"), "\n")
				blocks = append(blocks, *current)
			}
			current = &ParsedBlock{Header: header}
			contentLines = nil
			continue
		}

		if footer, ok := protocol.ParseFooter(line); ok {
			if current != nil {
				current.Content = strings.TrimRight(strings.Join(contentLines, "\n"), "\n")
				current.Footer = &footer
				blocks = append(blocks, *current)
				current = nil
				contentLines = nil
			}
			continue
		}

		if current != nil {
			contentLines = append(contentLines, line)
		}
	}

	if current != nil {
		current.Content = strings.Join(contentLines, "\n")
		blocks = append(blocks, *current)
	}

	return blocks, scanner.Err()
}

type ReconstructedHistory struct {
	Messages []llm.Message
}

func ReconstructFromBlocks(blocks []ParsedBlock, systemPrompt string) *ReconstructedHistory {
	history := &ReconstructedHistory{}

	pendingToolCalls := make(map[string][]llm.ToolCall)
	pendingToolCallIDs := make(map[string][]string)

	for _, block := range blocks {
		switch block.Header.BlockType {
		case protocol.BlockSystemPrompt:
			if systemPrompt == "" {
				systemPrompt = block.Content
			}

		case protocol.BlockUserMessage:
			history.Messages = append(history.Messages, llm.Message{
				Role:    "user",
				Content: block.Content,
			})

		case protocol.BlockAgentResponse:
			history.Messages = append(history.Messages, llm.Message{
				Role:    "assistant",
				Content: block.Content,
			})

		case protocol.BlockToolsInput:
			toolName, args := parseToolInput(block.Content)
			callID := fmt.Sprintf("resume_%s_%d", toolName, len(history.Messages))
			tc := llm.ToolCall{
				ID:   callID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      toolName,
					Arguments: args,
				},
			}
			agentKey := block.Header.AgentName
			pendingToolCalls[agentKey] = append(pendingToolCalls[agentKey], tc)
			pendingToolCallIDs[agentKey] = append(pendingToolCallIDs[agentKey], callID)

		case protocol.BlockToolsOutput:
			agentKey := block.Header.AgentName
			if calls, ok := pendingToolCalls[agentKey]; ok && len(calls) > 0 {
				history.Messages = append(history.Messages, llm.Message{
					Role:      "assistant",
					ToolCalls: calls,
				})

				responseContent := parseToolOutputContent(block.Content)

				for _, id := range pendingToolCallIDs[agentKey] {
					history.Messages = append(history.Messages, llm.Message{
						Role:       "tool",
						Content:    responseContent,
						ToolCallID: id,
					})
				}

				delete(pendingToolCalls, agentKey)
				delete(pendingToolCallIDs, agentKey)
			}
		}
	}

	return history
}

func parseToolInput(content string) (name string, arguments string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Tool name: ") {
			name = strings.TrimPrefix(line, "Tool name: ")
		} else if strings.HasPrefix(line, "Argument ") {
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				if arguments == "" {
					arguments = fmt.Sprintf(`{"prompt": "%s"}`, escapeJSON(parts[1]))
				}
			}
		}
	}
	return
}

func parseToolOutputContent(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Response: ") {
			return strings.TrimPrefix(line, "Response: ")
		}
	}
	return content
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
