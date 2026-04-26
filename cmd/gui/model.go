package main

import (
	"fmt"
	"sync"
	"time"
)

// AskType identifies what kind of ask interaction is pending.
type AskType int

const (
	AskNone AskType = iota
	AskOpenEnded
	AskMultipleChoice
	AskExecConfirm
	AskExecAltCmd
	AskExecReason
)

// AskState holds the state of a pending ask tool interaction.
type AskState struct {
	Type         AskType
	Question     string
	Options      []string
	AllowOpenEnd bool
	Cmdline      string
	// UI state for multiple choice
	SelectedOptions []int
	OpenEndText     string
}

// Block represents a single protocol block from the agent output.
type Block struct {
	BlockType string    // e.g. "agent_thinking", "agent_response", "tools_input", "tools_output", "stderr"
	AgentName string    // agent that produced this block
	Level     int       // nesting level
	Content   string    // body text between header and footer
	Duration  string    // from footer, if present
	Expanded  bool      // UI state: collapsed by default
	Time      time.Time // timestamp from the header line
}

// TokenStats holds the latest token usage data.
type TokenStats struct {
	AgentName        string
	PromptTokens     int64
	CompletionTokens int64
}

type ViewState int

const (
	ViewSelect ViewState = iota
	ViewChat
)

// AppModel holds all application state.
type AppModel struct {
	mu sync.Mutex

	Blocks      []Block
	TokenStats  []TokenStats
	InputText   string
	CurrentTime string

	View ViewState

	// Conversation selection state
	Conversations []ConversationInfo
	SelectError   string

	// State for bridge communication
	Started   bool   // true after subprocess has been launched
	Ready     bool   // true after receiving waiting_user_input
	Running   bool   // true while agent is processing
	AgentName string // name of the default agent
	ConvFile  string // conversation file path
	IsResume  bool   // true if this is a resumed conversation

	// Ask tool state
	AskPending *AskState // non-nil when an ask tool is waiting for input
}

func NewAppModel() *AppModel {
	return &AppModel{
		Blocks: make([]Block, 0),
		View:   ViewSelect,
	}
}

func (m *AppModel) SetView(v ViewState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.View = v
}

func (m *AppModel) SetConversations(convs []ConversationInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Conversations = convs
}

func (m *AppModel) SetSelectError(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SelectError = err
}

func (m *AppModel) AddBlock(b Block) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Blocks = append(m.Blocks, b)
	m.CurrentTime = time.Now().Format("15:04:05")
	return len(m.Blocks) - 1
}

func (m *AppModel) UpdateLastBlock(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Blocks) > 0 {
		m.Blocks[len(m.Blocks)-1].Content += content
	}
}

func (m *AppModel) SetStarted(started bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Started = started
}

func (m *AppModel) SetReady(ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Ready = ready
}

func (m *AppModel) SetRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Running = running
}

func (m *AppModel) SetAgentName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AgentName = name
}

func (m *AppModel) SetConvFile(f string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ConvFile = f
}

func (m *AppModel) SetIsResume(r bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IsResume = r
}

func (m *AppModel) ClearBlocks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Blocks = make([]Block, 0)
	m.TokenStats = nil
}

func (m *AppModel) SetAskState(s *AskState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AskPending = s
}

func (m *AppModel) SetTokenStats(stats []TokenStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TokenStats = stats
	m.CurrentTime = time.Now().Format("15:04:05")
}

func (m *AppModel) UpdateBlockContent(index int, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Blocks) {
		m.Blocks[index].Content = content
	}
}

func (m *AppModel) SetBlockExpanded(index int, expanded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Blocks) {
		m.Blocks[index].Expanded = expanded
	}
}

// TotalTokens returns total prompt + completion tokens across all stats.
func (m *AppModel) TotalTokens() (prompt int64, completion int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ts := range m.TokenStats {
		prompt += ts.PromptTokens
		completion += ts.CompletionTokens
	}
	return
}

// ToggleBlock expands/collapses a block.
func (m *AppModel) ToggleBlock(index int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Blocks) {
		m.Blocks[index].Expanded = !m.Blocks[index].Expanded
	}
}
func BlockLabel(bt string) string {
	switch bt {
	case "agent_thinking":
		return "💭 Thinking"
	case "agent_response":
		return "💬 Response"
	case "tools_input":
		return "🔧 Tool Input"
	case "tools_output":
		return "📤 Tool Output"
	case "user_message":
		return "👤 User"
	case "system_prompt":
		return "⚙️ System"
	case "token_stats":
		return "📊 Tokens"
	case "tools_definition":
		return "📋 Tools"
	case "waiting_user_input":
		return "⏳ Waiting"
	case "stderr":
		return "⚠️ Stderr"
	default:
		return fmt.Sprintf("❓ %s", bt)
	}
}

// ShouldDisplay returns true for block types that should be shown in the list.
func ShouldDisplay(bt string) bool {
	switch bt {
	case "waiting_user_input":
		return false
	default:
		return true
	}
}
