package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dgdevel/lite-dev-agent/internal/config"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
	"github.com/dgdevel/lite-dev-agent/internal/tools"
	"github.com/dgdevel/lite-dev-agent/internal/web"
)

func main() {
	listenFlag := flag.String("listen", ":8080", "web server listen address in [address]:[port] format")
	flag.Parse()

	rootPath := "."
	if args := flag.Args(); len(args) > 0 {
		rootPath = args[0]
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

	neededMCPs := make(map[string]bool)
	for _, ac := range cfg.Agents {
		for _, t := range ac.ToolList() {
			if cfg.FindMCP(t) != nil {
				neededMCPs[t] = true
			}
		}
	}

	mcpProviders := make(map[string]*tools.MCPProvider)
	for mcpName := range neededMCPs {
		mcpCfg := cfg.FindMCP(mcpName)
		mp, err := tools.NewMCPProvider(mcpCfg, rootPath, cfg.Timeouts.ToolExecutionDuration())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer mp.Close()
		mcpProviders[mcpName] = mp
	}

	llmClients := make(map[string]*llm.Client)
	for _, ac := range cfg.Agents {
		llmCfg := ac.ResolveLLM(cfg)
		llmClients[ac.Name] = llm.NewClient(llm.Options{
			APIBase:   llmCfg.APIBase,
			Model:     llmCfg.Model,
			APIKey:    llmCfg.APIKey,
			Headers:   llmCfg.Headers,
			Timeout:   cfg.Timeouts.LLMRequestDuration(),
			MaxTokens: llmCfg.MaxTokens,
		})
	}

	srv := web.NewServer(cfg, rootPath, llmClients, mcpProviders)
	if err := srv.Start(*listenFlag); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
