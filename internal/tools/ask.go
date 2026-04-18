package tools

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
)

type AskProvider struct {
	agentName string
	writer    io.Writer
	readInput func() (string, error)
}

func NewAskProvider(agentName string, writer io.Writer, readInput func() (string, error)) *AskProvider {
	return &AskProvider{
		agentName: agentName,
		writer:    writer,
		readInput: readInput,
	}
}

func (p *AskProvider) ToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Type: "function",
			Function: llm.Function{
				Name:        "ask_open_ended",
				Description: "Ask the user an open-ended question and wait for their response.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The question to ask the user",
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
				Description: "Ask the user a multiple choice question with numbered options.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The question text to present to the user",
						},
						"options": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "List of options to present to the user",
						},
						"type": map[string]any{
							"type":        "string",
							"enum":        []string{"single", "multi"},
							"description": "Selection mode: single for one choice, multi for multiple choices",
						},
						"allow_open_end_response": map[string]any{
							"type":        "boolean",
							"description": "If true, add a 'Type your own response' option for free text input",
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
				Description: "Ask the user for permission to execute a command, then run it if approved. Returns the command output or the user's rejection reason.",
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

func (p *AskProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	args, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	switch name {
	case "ask_open_ended":
		return p.handleOpenEnded(args)
	case "ask_multiple_choice":
		return p.handleMultipleChoice(args)
	case "ask_exec":
		return p.handleExec(ctx, args)
	default:
		return agent.ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}, nil
	}
}

func (p *AskProvider) handleOpenEnded(args map[string]any) (agent.ToolResult, error) {
	question := llm.GetArgumentString(args, "question")
	if question == "" {
		return agent.ToolResult{Content: "missing question argument", IsError: true}, nil
	}

	fmt.Fprintln(p.writer, question)
	protocol.WriteWaitingInput(p.writer, p.agentName)

	response, err := p.readInput()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: response}, nil
}

func (p *AskProvider) handleMultipleChoice(args map[string]any) (agent.ToolResult, error) {
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

	fmt.Fprintln(p.writer, question)
	for i, opt := range options {
		fmt.Fprintf(p.writer, "%d) %s\n", i+1, opt)
	}

	openEndIdx := 0
	if allowOpenEnd {
		openEndIdx = len(options) + 1
		fmt.Fprintf(p.writer, "%d) Type your own response\n", openEndIdx)
	}

	protocol.WriteWaitingInput(p.writer, p.agentName)

	response, err := p.readInput()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}

	nums := parseNumbers(response)
	if len(nums) == 0 {
		return agent.ToolResult{Content: "invalid selection: no valid numbers found", IsError: true}, nil
	}

	if openEndIdx > 0 {
		for _, n := range nums {
			if n == openEndIdx {
				fmt.Fprintln(p.writer, "Type your response:")
				protocol.WriteWaitingInput(p.writer, p.agentName)
				openResponse, err := p.readInput()
				if err != nil {
					return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
				}
				return agent.ToolResult{Content: openResponse}, nil
			}
		}
	}

	for _, n := range nums {
		if n < 1 || n > len(options) {
			return agent.ToolResult{Content: fmt.Sprintf("invalid option number: %d (valid: 1-%d)", n, len(options)), IsError: true}, nil
		}
	}

	if choiceType == "single" && len(nums) > 1 {
		return agent.ToolResult{Content: "only one option can be selected in single mode", IsError: true}, nil
	}

	var selected []string
	for _, n := range nums {
		selected = append(selected, options[n-1])
	}

	return agent.ToolResult{Content: strings.Join(selected, ", ")}, nil
}

func (p *AskProvider) handleExec(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
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

	fmt.Fprintf(p.writer, "Execute command: %s\n", cmdline)
	fmt.Fprintln(p.writer, "Allow execution? [y/n]")
	protocol.WriteWaitingInput(p.writer, p.agentName)

	response, err := p.readInput()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error reading response: %v", err), IsError: true}, nil
	}

	if !isAffirmative(response) {
		fmt.Fprintln(p.writer, "Command denied. Type your response:")
		protocol.WriteWaitingInput(p.writer, p.agentName)

		deniedResponse, err := p.readInput()
		if err != nil {
			return agent.ToolResult{Content: "DENIED"}, nil
		}
		return agent.ToolResult{Content: fmt.Sprintf("DENIED: %s", deniedResponse)}, nil
	}

	var timeoutDur time.Duration
	if timeoutSec > 0 {
		timeoutDur = time.Duration(timeoutSec) * time.Second
	} else {
		timeoutDur = 10 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeoutDur)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", cmdline)
	output, err := cmd.CombinedOutput()

	if execCtx.Err() == context.DeadlineExceeded {
		return agent.ToolResult{
			Content: fmt.Sprintf("Command timed out after %d seconds\n%s", timeoutSec, string(output)),
			IsError: true,
		}, nil
	}

	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("Command error: %v\n%s", err, string(output)),
			IsError: true,
		}, nil
	}

	return agent.ToolResult{Content: string(output)}, nil
}

func parseNumbers(s string) []int {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ",")
	var nums []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		nums = append(nums, n)
	}
	return nums
}

func isAffirmative(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "y" || s == "yes"
}
