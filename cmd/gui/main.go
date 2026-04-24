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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Force --color=false so ANSI escapes don't corrupt protocol parsing
	agentArgs := append([]string{"--color=false"}, os.Args[1:]...)
	if err := bridge.Start(ctx, binaryPath, agentArgs); err != nil {

		os.Exit(1)
	}
	defer bridge.Close()

	ui := NewUI(model, bridge)

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
	// Periodic invalidation to refresh UI while agent is processing
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			model.mu.Lock()
			running := model.Running
			model.mu.Unlock()
			if running {
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
