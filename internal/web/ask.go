package web

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
)

type AskQuestion struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Question string `json:"question,omitempty"`
	Cmdline  string `json:"cmdline,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
	Options  []string `json:"options,omitempty"`
	Mode     string `json:"mode,omitempty"`
	AllowOpenEnd bool `json:"allowOpenEnd,omitempty"`
}

type AskResponse struct {
	Content string
	Err     error
}

type AskHub struct {
	mu     sync.Mutex
	nextID atomic.Int64
	waiting map[string]chan AskResponse
}

func NewAskHub() *AskHub {
	return &AskHub{
		waiting: make(map[string]chan AskResponse),
	}
}

func (h *AskHub) NewID() string {
	return fmt.Sprintf("ask_%d_%d", time.Now().UnixMilli(), h.nextID.Add(1))
}

func (h *AskHub) Register(id string, ch chan AskResponse) {
	h.mu.Lock()
	h.waiting[id] = ch
	h.mu.Unlock()
}

func (h *AskHub) Unregister(id string) {
	h.mu.Lock()
	delete(h.waiting, id)
	h.mu.Unlock()
}

func (h *AskHub) Respond(id string, response string) bool {
	h.mu.Lock()
	ch, ok := h.waiting[id]
	if ok {
		delete(h.waiting, id)
	}
	h.mu.Unlock()
	if !ok {
		return false
	}
	ch <- AskResponse{Content: response}
	return true
}

func (h *AskHub) askAndWait(ctx context.Context, eventCh chan Event, question AskQuestion) (string, error) {
	ch := make(chan AskResponse, 1)
	h.Register(question.ID, ch)
	defer h.Unregister(question.ID)

	select {
	case eventCh <- Event{Type: "ask_question", AskQuestion: &question}:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case resp := <-ch:
		if resp.Err != nil {
			return "", resp.Err
		}
		return resp.Content, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type WebAskProvider struct {
	agentName string
	hub       *AskHub
	eventCh   chan Event
}

func NewWebAskProvider(agentName string, hub *AskHub, eventCh chan Event) *WebAskProvider {
	return &WebAskProvider{
		agentName: agentName,
		hub:       hub,
		eventCh:   eventCh,
	}
}

func (p *WebAskProvider) ToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Type: "function",
			Function: llm.Function{
				Name:        "ask_open_ended",
				Description: "Ask the user an open-ended question",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The question text",
						},
					},
					"required": []string{"question"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "ask_multiple_choice",
				Description: "Ask the user a multiple choice question",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The question text",
						},
						"options": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "List of options",
						},
						"type": map[string]any{
							"type":        "string",
							"enum":        []string{"single", "multi"},
							"description": "Selection mode: single for one choice, multi for multiple choices",
						},
						"allow_open_end_response": map[string]any{
							"type":        "boolean",
							"description": "If true, add a 'Type your own response' option",
						},
					},
					"required": []string{"question", "options", "type"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "ask_exec",
				Description: "Ask the user to execute a command and get the output",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cmdline": map[string]any{
							"type":        "string",
							"description": "The command line to execute",
						},
						"timeout": map[string]any{
							"type":        "integer",
							"description": "Timeout in seconds for the command execution",
						},
					},
					"required": []string{"cmdline"},
				},
			},
		},
	}
}

func (p *WebAskProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	args, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	switch name {
	case "ask_open_ended":
		return p.handleOpenEnded(ctx, args)
	case "ask_multiple_choice":
		return p.handleMultipleChoice(ctx, args)
	case "ask_exec":
		return p.handleExec(ctx, args)
	default:
		return agent.ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}, nil
	}
}

func (p *WebAskProvider) handleOpenEnded(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	question := llm.GetArgumentString(args, "question")
	if question == "" {
		return agent.ToolResult{Content: "missing question argument", IsError: true}, nil
	}

	id := p.hub.NewID()
	response, err := p.hub.askAndWait(ctx, p.eventCh, AskQuestion{
		ID:       id,
		Type:     "open_ended",
		Question: question,
	})
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}
	return agent.ToolResult{Content: response}, nil
}

func (p *WebAskProvider) handleMultipleChoice(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	question := llm.GetArgumentString(args, "question")
	if question == "" {
		return agent.ToolResult{Content: "missing question argument", IsError: true}, nil
	}

	optionsRaw, ok := args["options"]
	if !ok {
		return agent.ToolResult{Content: "missing options argument", IsError: true}, nil
	}
	optionsSlice, ok := optionsRaw.([]any)
	if !ok || len(optionsSlice) == 0 {
		return agent.ToolResult{Content: "options must be a non-empty array", IsError: true}, nil
	}
	var options []string
	for _, o := range optionsSlice {
		s, ok := o.(string)
		if !ok {
			return agent.ToolResult{Content: "all options must be strings", IsError: true}, nil
		}
		options = append(options, s)
	}

	choiceType := llm.GetArgumentString(args, "type")
	if choiceType == "" {
		choiceType = "single"
	}
	if choiceType != "single" && choiceType != "multi" {
		return agent.ToolResult{Content: "type must be 'single' or 'multi'", IsError: true}, nil
	}

	allowOpenEnd := false
	if v, ok := args["allow_open_end_response"]; ok {
		if b, ok := v.(bool); ok {
			allowOpenEnd = b
		}
	}

	id := p.hub.NewID()
	response, err := p.hub.askAndWait(ctx, p.eventCh, AskQuestion{
		ID:           id,
		Type:         "multiple_choice",
		Question:     question,
		Options:      options,
		Mode:         choiceType,
		AllowOpenEnd: allowOpenEnd,
	})
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: response}, nil
}

func (p *WebAskProvider) handleExec(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
	cmdline := llm.GetArgumentString(args, "cmdline")
	if cmdline == "" {
		return agent.ToolResult{Content: "missing cmdline argument", IsError: true}, nil
	}

	timeoutSec := 0
	if v, ok := args["timeout"]; ok {
		if f, ok := v.(float64); ok {
			timeoutSec = int(f)
		}
	}

	id := p.hub.NewID()
	response, err := p.hub.askAndWait(ctx, p.eventCh, AskQuestion{
		ID:      id,
		Type:    "exec",
		Cmdline: cmdline,
		Timeout: timeoutSec,
	})
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: response}, nil
}
