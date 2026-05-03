package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/conversation"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
	"github.com/dgdevel/lite-dev-agent/internal/tools"
)

type Server struct {
	cfg              *config.Config
	rootPath         string
	llmClients       map[string]*llm.Client
	mcpProviders     map[string]*tools.MCPProvider
	conversationsDir string

	mu       sync.Mutex
	active   bool
	cancelFn context.CancelFunc
	askHub   *AskHub
}

func NewServer(cfg *config.Config, rootPath string, llmClients map[string]*llm.Client, mcpProviders map[string]*tools.MCPProvider) *Server {
	conversationsDir := filepath.Join(rootPath, ".lite-dev-agent", "conversations")
	os.MkdirAll(conversationsDir, 0755)

	return &Server{
		cfg:              cfg,
		rootPath:         rootPath,
		llmClients:       llmClients,
		mcpProviders:     mcpProviders,
		conversationsDir: conversationsDir,
		askHub:           NewAskHub(),
	}
}

func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	mux.HandleFunc("GET /api/conversations/{filename}", s.handleGetConversation)
	mux.HandleFunc("POST /api/conversations", s.handleNewConversation)
	mux.HandleFunc("POST /api/conversations/{filename}/chat", s.handleChat)
	mux.HandleFunc("POST /api/conversations/{filename}/ask/{askId}", s.handleAskResponse)
	mux.HandleFunc("POST /api/abort", s.handleAbort)

	fmt.Fprintf(os.Stderr, "web server listening on %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	dirName := filepath.Base(s.rootPath)
	page := bytes.ReplaceAll(indexHTML, []byte("{{CWD_NAME}}"), []byte(dirName))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.conversationsDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
		return
	}

	type convInfo struct {
		Filename string `json:"filename"`
		Modified string `json:"modified"`
		Size     int64  `json:"size"`
		Stats    string `json:"stats,omitempty"`
	}

	var convs []convInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		stats := extractLastTokenStats(filepath.Join(s.conversationsDir, e.Name()))
		convs = append(convs, convInfo{
			Filename: e.Name(),
			Modified: info.ModTime().Format(time.RFC3339),
			Size:     info.Size(),
			Stats:    stats,
		})
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Modified > convs[j].Modified
	})

	if convs == nil {
		convs = []convInfo{}
	}
	writeJSON(w, http.StatusOK, convs)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	filename := sanitizeFilename(r.PathValue("filename"))
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	convPath := filepath.Join(s.conversationsDir, filename)
	blocks, err := conversation.ParseFile(convPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
		return
	}

	type blockInfo struct {
		Type     string  `json:"type"`
		Agent    string  `json:"agent"`
		Level    int     `json:"level"`
		Content  string  `json:"content"`
		Duration *string `json:"duration,omitempty"`
	}

	var result []blockInfo
	for _, b := range blocks {
		if b.Header.BlockType == protocol.BlockWaitingInput {
			continue
		}
		bi := blockInfo{
			Type:    b.Header.BlockType.String(),
			Agent:   b.Header.AgentName,
			Level:   b.Header.Level,
			Content: stripAllTimestamps(b.Content),
		}
		if b.Footer != nil {
			d := formatDuration(b.Footer.Duration)
			bi.Duration = &d
		}
		result = append(result, bi)
	}

	if result == nil {
		result = []blockInfo{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleNewConversation(w http.ResponseWriter, r *http.Request) {
	log, err := conversation.NewLog(s.rootPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
		return
	}
	filename := filepath.Base(log.Path())
	log.Close()

	writeJSON(w, http.StatusOK, map[string]string{"filename": filename})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	filename := sanitizeFilename(r.PathValue("filename"))
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid message"})
		return
	}

	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is already running"})
		return
	}
	s.active = true
	ctx, cancel := context.WithCancel(r.Context())
	s.cancelFn = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.active = false
		s.cancelFn = nil
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)

	eventCh := make(chan Event, 256)

	convPath := filepath.Join(s.conversationsDir, filename)
	ensureFile(convPath)

	convLog, err := os.OpenFile(convPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to open conversation log %s: %v\n", convPath, err)
		sseSend(w, flusher, Event{Type: "error", Content: "failed to open conversation log"})
		return
	}

	var history []llm.Message
	isResume := false
	if info, _ := os.Stat(convPath); info != nil && info.Size() > 0 {
		blocks, err := conversation.ParseFile(convPath)
		if err == nil && len(blocks) > 0 {
			defaultSP := s.cfg.DefaultAgent().SystemPrompt
			reconstructed := conversation.ReconstructFromBlocks(blocks, defaultSP)
			history = reconstructed.Messages
			isResume = len(history) > 0
		}
	}

	dw := NewDirectWriter(eventCh, convLog)
	protocol.WriteBeginConversation(dw, convPath, isResume)

	_, mainAgent := s.createAgents(dw, eventCh)

	done := make(chan error, 1)
	go func() {
		_, err := mainAgent.Run(ctx, agent.RunOptions{
			UserMessage: body.Message,
			History:     history,
		})
		done <- err
	}()

	go func() {
		agentErr := <-done
		if agentErr != nil {
			fmt.Fprintf(os.Stderr, "[error] agent run failed: %v\n", agentErr)
			eventCh <- Event{Type: "error", Content: agentErr.Error()}
		}
		protocol.WriteEndConversation(dw, convPath)
		convLog.Close()
		eventCh <- Event{Type: "done"}
		close(eventCh)
	}()

	for event := range eventCh {
		sseSend(w, flusher, event)
	}
}

func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAskResponse(w http.ResponseWriter, r *http.Request) {
	askID := r.PathValue("askId")
	if askID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ask ID"})
		return
	}

	var body struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Response == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid response"})
		return
	}

	if s.askHub.Respond(askID, body.Response) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown or expired ask ID"})
	}
}

func (s *Server) createAgents(writer io.Writer, eventCh chan Event) (map[string]*agent.Agent, *agent.Agent) {
	filter := protocol.NewOutputFilter("")
	agentToolProvider := tools.NewAgentToolProvider(s.cfg, writer, filter, &s.cfg.Timeouts)

	var agentsMdContent string
	if data, err := os.ReadFile(filepath.Join(s.rootPath, "AGENTS.md")); err == nil {
		agentsMdContent = string(data)
	}

	agents := make(map[string]*agent.Agent)
	var defaultAgent *agent.Agent

	for i := range s.cfg.Agents {
		ac := &s.cfg.Agents[i]
		llmCfg := ac.ResolveLLM(s.cfg)
		client := s.llmClients[ac.Name]

		registry := agent.NewToolRegistry()
		for _, toolGroup := range ac.ToolList() {
			switch toolGroup {
			case "agents":
				registry.Register("agents", agentToolProvider)
			case "ask":
				registry.Register("ask", NewWebAskProvider(ac.Name, s.askHub, eventCh))
			default:
				if mp, ok := s.mcpProviders[toolGroup]; ok {
					registry.Register(toolGroup, mp)
				}
			}
		}

		a := &agent.Agent{
			Config:          ac,
			LLMConfig:       llmCfg,
			LLM:             client,
			Registry:        registry,
			Writer:          writer,
			Filter:          filter,
			Timeouts:        &s.cfg.Timeouts,
			AgentsMdContent: agentsMdContent,
			IsMain:          ac.Default,
		}

		agents[ac.Name] = a
		agentToolProvider.Register(a)

		if ac.Default {
			defaultAgent = a
		}
	}

	return agents, defaultAgent
}

func sseSend(w http.ResponseWriter, flusher http.Flusher, event Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

func ensureFile(path string) {
	os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.Close()
	}
}

var tokenStatsRe = regexp.MustCompile(`prompt:\s*(\d+)\s+completion:\s*(\d+)`)

func extractLastTokenStats(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Scan file for token_stats blocks, collect all non-zero entries, return last valid
	var allStats []string
	var currentContent string
	var inTokenStats bool
	scanner := newScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Header lines: #!timestamp agent: name | level: N | token_stats
		if strings.HasPrefix(line, "#!") && strings.Contains(line, "| token_stats") {
			// Finish previous block if any
			if inTokenStats && currentContent != "" {
				if sum, ok := summarizeTokenContent(currentContent); ok {
					allStats = append(allStats, sum)
				}
			}
			inTokenStats = true
			currentContent = ""
			continue
		}
		if !inTokenStats {
			continue
		}
		// Footer line: #!timestamp time: ...
		if strings.HasPrefix(line, "#!") {
			if currentContent != "" {
				if sum, ok := summarizeTokenContent(currentContent); ok {
					allStats = append(allStats, sum)
				}
			}
			inTokenStats = false
			currentContent = ""
			continue
		}
		currentContent += line + "\n"
	}
	// Handle last block if file ends without a footer
	if inTokenStats && currentContent != "" {
		if sum, ok := summarizeTokenContent(currentContent); ok {
			allStats = append(allStats, sum)
		}
	}

	if len(allStats) == 0 {
		return ""
	}
	return allStats[len(allStats)-1]
}

func summarizeTokenContent(content string) (string, bool) {
	var totalPrompt, totalCompletion int64
	matches := tokenStatsRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		p, _ := strconv.ParseInt(m[1], 10, 64)
		c, _ := strconv.ParseInt(m[2], 10, 64)
		totalPrompt += p
		totalCompletion += c
	}
	if totalPrompt == 0 && totalCompletion == 0 {
		return "", false
	}
	total := totalPrompt + totalCompletion
	fmtP := formatTokenCount(totalPrompt)
	fmtC := formatTokenCount(totalCompletion)
	fmtT := formatTokenCount(total)
	return "▸ in " + fmtP + "  ▸ out " + fmtC + "  total " + fmtT, true
}

func newScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return s
}

func formatTokenCount(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
