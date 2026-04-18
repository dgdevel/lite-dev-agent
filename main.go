package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/conversation"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/protocol"
	"github.com/dgdevel/lite-dev-agent/internal/tools"
)

func main() {
	outputFlag := flag.String("output", "", "comma-separated list of output sections to emit")
	devkitPathFlag := flag.String("devkit-path", "", "path to the nixdevkit executable")
	resumeFlag := flag.String("resume", "", "path to conversation log to resume from")
	colorFlag := flag.Bool("color", false, "colorize output with ANSI escape codes")
	flag.Parse()

	rootPath := "."
	if args := flag.Args(); len(args) > 0 {
		rootPath = args[0]
	}

	if *devkitPathFlag != "" {
		devkitAbs, err := filepath.Abs(*devkitPathFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error resolving devkit path: %v\n", err)
			os.Exit(1)
		}
		*devkitPathFlag = devkitAbs
	}

	abs, err := filepath.Abs(rootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving path: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(abs); err != nil {
		fmt.Fprintf(os.Stderr, "error changing directory: %v\n", err)
		os.Exit(1)
	}
	rootPath = "."

	cfg, err := config.Load(rootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	filter := protocol.NewOutputFilter(*outputFlag)

	var colorWriter *protocol.ColorWriter
	stdoutWriter := io.Writer(os.Stdout)
	if *colorFlag {
		colorWriter = protocol.NewColorWriter(os.Stdout)
		stdoutWriter = colorWriter
	}

	agentToolProvider := tools.NewAgentToolProvider(cfg, stdoutWriter, filter, &cfg.Timeouts)

	needsDevkit := false
	for _, ac := range cfg.Agents {
		for _, t := range ac.ToolList() {
			if t == "devkit" {
				needsDevkit = true
			}
		}
	}

	var devkitProvider *tools.DevkitProvider
	if needsDevkit {
		dp, err := tools.NewDevkitProvider(*devkitPathFlag, rootPath, cfg.Timeouts.ToolExecutionDuration())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer dp.Close()
		devkitProvider = dp
	}

	var convLog *conversation.Log
	if *resumeFlag != "" {
		convLog, err = conversation.OpenLog(*resumeFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		convLog, err = conversation.NewLog(rootPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create conversation log: %v\n", err)
		}
	}
	if convLog != nil {
		defer convLog.Close()
		fmt.Fprintf(os.Stderr, "conversation log: %s\n", convLog.Path())
	}

	stdin := bufio.NewReader(os.Stdin)
	readInputFn := func() (string, error) {
		return readInput(stdin)
	}

	defaultAgentCfg := cfg.DefaultAgent()
	agents := make(map[string]*agent.Agent)

	for _, ac := range cfg.Agents {
		llmCfg := ac.ResolveLLM(cfg)
		client := llm.NewClient(llm.Options{
			APIBase:   llmCfg.APIBase,
			Model:     llmCfg.Model,
			APIKey:    llmCfg.APIKey,
			Headers:   llmCfg.Headers,
			Timeout:   cfg.Timeouts.LLMRequestDuration(),
			MaxTokens: llmCfg.MaxTokens,
		})

		agentRegistry := agent.NewToolRegistry()

		var agentWriter io.Writer = stdoutWriter
		if convLog != nil {
			agentWriter = conversation.NewTeeWriter(stdoutWriter, convLog)
		}

		toolList := ac.ToolList()
		for _, toolGroup := range toolList {
			switch toolGroup {
			case "agents":
				agentRegistry.Register("agents", agentToolProvider)
			case "devkit":
				if devkitProvider != nil {
					agentRegistry.Register("devkit", devkitProvider)
				}
			case "online":
				agentRegistry.Register("online", tools.NewOnlineProvider(cfg.Timeouts.ToolExecutionDuration()))
			case "ask":
				agentRegistry.Register("ask", tools.NewAskProvider(ac.Name, agentWriter, readInputFn))
			}
		}

		a := &agent.Agent{
			Config:    &ac,
			LLMConfig: llmCfg,
			LLM:       client,
			Registry:  agentRegistry,
			Writer:    agentWriter,
			Filter:    filter,
			Timeouts:  &cfg.Timeouts,
			IsMain:    ac.Default,
		}

		agents[ac.Name] = a
		agentToolProvider.Register(a)
	}

	mainAgent := agents[defaultAgentCfg.Name]
	if mainAgent == nil {
		fmt.Fprintf(os.Stderr, "error: default agent %q not found\n", defaultAgentCfg.Name)
		os.Exit(1)
	}

	var history []llm.Message
	if *resumeFlag != "" {
		blocks, err := conversation.ParseFile(*resumeFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing resume file: %v\n", err)
			os.Exit(1)
		}
		reconstructed := conversation.ReconstructFromBlocks(blocks, defaultAgentCfg.SystemPrompt)
		history = reconstructed.Messages
		fmt.Fprintf(os.Stderr, "resumed session with %d messages\n", len(history))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	for {
		protocol.WriteWaitingInput(stdoutWriter, mainAgent.Config.Name)

		input, err := readInput(stdin)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
			continue
		}

		if input == "" {
			continue
		}

		result, err := mainAgent.Run(ctx, agent.RunOptions{
			UserMessage: input,
			History:     history,
		})

		if err != nil {
			if _, ok := err.(*agent.InterruptionError); ok {
				break
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}

		if result != nil {
			history = mainAgent.History
		}
	}
}

func readInput(r *bufio.Reader) (string, error) {
	var lines []string
	blankCount := 0

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(lines) > 0 {
				return joinLines(lines), nil
			}
			if err == io.EOF {
				return "", io.EOF
			}
			return "", err
		}

		trimmed := trimNewline(line)

		if trimmed == "" {
			blankCount++
			if blankCount >= 1 && len(lines) > 0 {
				return joinLines(lines), nil
			}
			continue
		}

		blankCount = 0
		lines = append(lines, trimmed)
	}
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}
