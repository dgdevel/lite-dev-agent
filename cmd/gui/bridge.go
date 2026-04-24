package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bridge manages the subprocess connection to lite-dev-agent.
type Bridge struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	model   *AppModel
	readyCh chan struct{}
	mu      sync.Mutex
}

var headerRe = regexp.MustCompile(
	`^#!(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) agent: (.+) \| level: (\d+) \| (\w+)$`)

var footerRe = regexp.MustCompile(
	`^#!(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) time: (.+)$`)

var conversationRe = regexp.MustCompile(
	`^#!(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) (begin_conversation|resume_conversation|end_conversation) \| file: (.+)$`)

var tokenStatsRe = regexp.MustCompile(
	`^(\S+)\s+prompt:\s+(\d+)\s+completion:\s+(\d+)`)

var waitingInputRe = regexp.MustCompile(
	`^#!(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) agent: (.+) \| level: (\d+) \| waiting_user_input$`)

// Patterns for detecting ask tool prompts from loose stdout lines.
var execConfirmRe = regexp.MustCompile(`^Execute command: (.+)$`)
var allowExecRe = regexp.MustCompile(`^Allow execution\? \[y/n\]$`)
var deniedAltCmdRe = regexp.MustCompile(`^Command denied\. Provide an alternative command \(or leave empty to give a reason\):$`)
var deniedReasonRe = regexp.MustCompile(`^Type the reason for denial:$`)
var optionLineRe = regexp.MustCompile(`^(\d+)\) (.+)$`)

func NewBridge(model *AppModel) *Bridge {
	return &Bridge{
		model:   model,
		readyCh: make(chan struct{}, 1),
	}
}

// Start launches the lite-dev-agent subprocess.
func (b *Bridge) Start(ctx context.Context, binaryPath string, args []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Args[0] = "lite-dev-agent"

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	b.stdin = stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	b.cmd = cmd
	b.model.SetStarted(true)

	go b.readStderr(stderr)
	go b.parseOutput(stdout)

	return nil
}

// readStderr feeds stderr lines as "stderr" blocks into the model.
// Consecutive stderr lines are merged into one block.
func (b *Bridge) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		b.model.mu.Lock()
		if len(b.model.Blocks) > 0 && b.model.Blocks[len(b.model.Blocks)-1].BlockType == "stderr" {
			prev := b.model.Blocks[len(b.model.Blocks)-1].Content
			b.model.Blocks[len(b.model.Blocks)-1].Content = prev + "\n" + line
			b.model.CurrentTime = time.Now().Format("15:04:05")
			b.model.mu.Unlock()
			continue
		}
		b.model.mu.Unlock()
		b.model.AddBlock(Block{
			BlockType: "stderr",
			AgentName: "system",
			Content:   line,
			Expanded:  true,
			Time:      time.Now(),
		})
	}
}

// parseOutput reads stdout line by line and assembles blocks.
func (b *Bridge) parseOutput(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0), 10*1024*1024)

	var currentBlock *Block
	var contentLines []string
	var looseLines []string // lines outside blocks, possibly ask prompts

	finalizeBlock := func() {
		if currentBlock != nil {
			currentBlock.Content = strings.Join(contentLines, "\n")
			if currentBlock.BlockType == "token_stats" {
				b.parseTokenStats(currentBlock.Content)
			}
			b.model.AddBlock(*currentBlock)
			currentBlock = nil
			contentLines = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if m := waitingInputRe.FindStringSubmatch(line); m != nil {
			finalizeBlock()

			// Try to parse loose lines as an ask prompt
			askState := b.parseAskPrompt(looseLines)
			looseLines = nil

			b.model.SetAgentName(m[2])
			if askState != nil {
				b.model.SetAskState(askState)
				b.model.SetReady(true)
				b.model.SetRunning(false)
			} else {
				b.model.SetAskState(nil)
				b.model.SetReady(true)
				b.model.SetRunning(false)
			}
			select {
			case b.readyCh <- struct{}{}:
			default:
			}
			continue
		}

		if conversationRe.MatchString(line) {
			m := conversationRe.FindStringSubmatch(line)
			if m[2] == "begin_conversation" || m[2] == "resume_conversation" {
				b.model.SetConvFile(m[3])
			}
			continue
		}

		if m := headerRe.FindStringSubmatch(line); m != nil {
			finalizeBlock()
			looseLines = nil
			t, _ := time.Parse("2006-01-02 15:04:05", m[1])
			level, _ := strconv.Atoi(m[3])
			currentBlock = &Block{
				BlockType: m[4],
				AgentName: m[2],
				Level:     level,
				Time:      t,
			}
			contentLines = nil
			continue
		}

		if m := footerRe.FindStringSubmatch(line); m != nil {
			if currentBlock != nil {
				currentBlock.Duration = m[2]
				currentBlock.Content = strings.Join(contentLines, "\n")
				if currentBlock.BlockType == "token_stats" {
					b.parseTokenStats(currentBlock.Content)
				}
				b.model.AddBlock(*currentBlock)
				currentBlock = nil
				contentLines = nil
			}
			continue
		}

		if currentBlock != nil {
			contentLines = append(contentLines, line)
		} else {
			// Accumulate loose lines (potential ask prompts)
			looseLines = append(looseLines, line)
		}
	}

	finalizeBlock()
}

// parseAskPrompt analyzes loose lines before a waiting_user_input marker
// to determine what kind of ask interaction is pending.
func (b *Bridge) parseAskPrompt(lines []string) *AskState {
	if len(lines) == 0 {
		return nil
	}

	// Check for ask_exec confirmation: "Execute command: X" + "Allow execution? [y/n]"
	for i := 0; i < len(lines)-1; i++ {
		if m := execConfirmRe.FindStringSubmatch(lines[i]); m != nil {
			if allowExecRe.MatchString(lines[i+1]) {
				return &AskState{
					Type:     AskExecConfirm,
					Cmdline:  m[1],
					Question: "Allow execution?",
				}
			}
		}
	}

	// Check for ask_exec denied alternative: "Command denied. Provide an alternative command..."
	for _, l := range lines {
		if deniedAltCmdRe.MatchString(l) {
			return &AskState{
				Type: AskExecAltCmd,
			}
		}
	}

	// Check for ask_exec denied reason: "Type the reason for denial:"
	for _, l := range lines {
		if deniedReasonRe.MatchString(l) {
			return &AskState{
				Type: AskExecReason,
			}
		}
	}

	// Check for ask_multiple_choice: question line followed by numbered options
	if len(lines) >= 2 {
		if optionLineRe.MatchString(lines[1]) || (len(lines) >= 3 && optionLineRe.MatchString(lines[2])) {
			question := lines[0]
			var options []string
			allowOpenEnd := false
			for _, l := range lines[1:] {
				if m := optionLineRe.FindStringSubmatch(l); m != nil {
					if m[2] == "Type your own response" {
						allowOpenEnd = true
					}
					options = append(options, m[2])
				}
			}
			if len(options) > 0 {
				return &AskState{
					Type:          AskMultipleChoice,
					Question:      question,
					Options:       options,
					AllowOpenEnd:  allowOpenEnd,
				}
			}
		}
	}

	// Check for ask_open_ended: single line question (or "Type your response:" from multiple choice open end)
	if len(lines) == 1 {
		line := lines[0]
		// "Type your response:" is from the open-end follow-up in multiple choice
		if line == "Type your response:" {
			return &AskState{
				Type:     AskOpenEnded,
				Question: line,
			}
		}
		return &AskState{
			Type:     AskOpenEnded,
			Question: line,
		}
	}

	// Multi-line loose text treated as open-ended
	return &AskState{
		Type:     AskOpenEnded,
		Question: strings.Join(lines, "\n"),
	}
}

func (b *Bridge) parseTokenStats(content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimLeft(line, " │├└─ ")
		if m := tokenStatsRe.FindStringSubmatch(line); m != nil {
			prompt, _ := strconv.ParseInt(m[2], 10, 64)
			completion, _ := strconv.ParseInt(m[3], 10, 64)
			b.model.AddTokenStats(TokenStats{
				AgentName:        m[1],
				PromptTokens:     prompt,
				CompletionTokens: completion,
			})
		}
	}
}

// SendPrompt writes the user's prompt to the agent's stdin.
func (b *Bridge) SendPrompt(text string) error {
	b.model.SetReady(false)
	b.model.SetRunning(true)
	_, err := fmt.Fprintf(b.stdin, "%s\n\n", text)
	return err
}

// SendResponse writes a response line to the agent's stdin (for ask tools).
func (b *Bridge) SendResponse(text string) error {
	b.model.SetAskState(nil)
	b.model.SetReady(false)
	b.model.SetRunning(true)
	_, err := fmt.Fprintf(b.stdin, "%s\n", text)
	return err
}

// Ready returns a channel that signals when the agent is ready for input.
func (b *Bridge) Ready() <-chan struct{} {
	return b.readyCh
}

// Close cleans up the subprocess.
func (b *Bridge) Close() {
	if b.stdin != nil {
		b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill()
	}
}

