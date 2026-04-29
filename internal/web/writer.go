package web

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type Event struct {
	Type      string `json:"type"`
	BlockType string `json:"blockType,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Level     int    `json:"level,omitempty"`
	Content   string `json:"content,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

type DirectWriter struct {
	ch  chan Event
	log io.Writer
	mu  sync.Mutex
}

func NewDirectWriter(ch chan Event, log io.Writer) *DirectWriter {
	return &DirectWriter{ch: ch, log: log}
}

func (w *DirectWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.log != nil {
		w.log.Write(p)
	}

	s := string(p)
	trimmed := strings.TrimRight(s, "\n")

	if header, ok := protocol.ParseHeader(trimmed); ok {
		w.emit(Event{
			Type:      "block_start",
			BlockType: header.BlockType.String(),
			Agent:     header.AgentName,
			Level:     header.Level,
		})
		return len(p), nil
	}

	if footer, ok := protocol.ParseFooter(trimmed); ok {
		w.emit(Event{
			Type:     "block_end",
			Duration: formatDuration(footer.Duration),
		})
		return len(p), nil
	}

	if protocol.IsConversationMarker(trimmed) {
		return len(p), nil
	}

	content := s
	if strings.HasPrefix(s, "#!") {
		content = stripAllTimestamps(s)
	}

	if content != "" {
		w.emit(Event{Type: "stream_delta", Content: content})
	}

	return len(p), nil
}

func (w *DirectWriter) emit(e Event) {
	select {
	case w.ch <- e:
	default:
	}
}

func formatDuration(d time.Duration) string {
	abs := d
	if abs < 0 {
		abs = -abs
	}
	h := int(abs.Hours())
	m := int(abs.Minutes()) % 60
	s := int(abs.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func stripAllTimestamps(s string) string {
	if !strings.Contains(s, "\n") {
		return stripLineTimestamp(s)
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = stripLineTimestamp(lines[i])
	}
	return strings.Join(lines, "\n")
}

func stripLineTimestamp(line string) string {
	if len(line) >= 22 &&
		line[0] == '#' && line[1] == '!' &&
		line[6] == '-' && line[9] == '-' &&
		line[12] == ' ' && line[15] == ':' &&
		line[18] == ':' && line[21] == ' ' {
		return line[22:]
	}
	return line
}
