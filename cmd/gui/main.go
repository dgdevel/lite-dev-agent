package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"
)

func main() {
	binaryPath, err := findAgentBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	model := NewAppModel()
	bridge := NewBridge(model)

	ui := NewUI(model, bridge)

	ui.onStartConversation = func(resumePath string) {
		ctx, cancel := context.WithCancel(context.Background())

		agentArgs := append([]string{"--color=false"}, os.Args[1:]...)
		if resumePath != "" {
			agentArgs = append([]string{"--color=false", "--resume", resumePath}, os.Args[1:]...)
		}
		if err := bridge.Start(ctx, binaryPath, agentArgs); err != nil {
			model.SetSelectError(fmt.Sprintf("failed to start agent: %v", err))
			cancel()
			return
		}

		model.SetView(ViewChat)
		_ = cancel
	}

	ui.loadConversations()

	go func() {
		window := new(app.Window)
		window.Option(app.Title("Lite Dev Agent"))
		window.Option(app.Size(unit.Dp(900), unit.Dp(700)))
		if err := run(window, ui, model); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()

	app.Main()
}

func run(window *app.Window, ui *UI, model *AppModel) error {
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			model.mu.Lock()
			running := model.Running
			view := model.View
			model.mu.Unlock()
			if running || view == ViewSelect {
				window.Invalidate()
			}
		}
	}()

	var ops op.Ops
	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func findAgentBinary() (string, error) {
	self, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(self)
		for i := 0; i < 5; i++ {
			candidate := filepath.Join(dir, "lite-dev-agent")
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	path, err := exec.LookPath("lite-dev-agent")
	if err == nil {
		return path, nil
	}

	return "", fmt.Errorf("could not find lite-dev-agent binary; ensure it's in PATH or same directory as the GUI")
}
