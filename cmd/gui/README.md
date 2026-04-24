# Lite Dev Agent GUI

A graphical frontend for [lite-dev-agent](..) built with [Gio UI](https://gioui.org).

## Architecture

The GUI launches the `lite-dev-agent` CLI as a hidden subprocess and communicates via stdin/stdout using the protocol defined in `internal/protocol`.

- **cmd/gui/main.go** — Entry point, window setup, binary discovery
- **cmd/gui/bridge.go** — Subprocess lifecycle, stdout parsing, stdin writing  
- **cmd/gui/model.go** — Data model: blocks, token stats, UI state
- **cmd/gui/ui.go** — Gio UI layout: top bar, scrollable block list, text input

## Building

```bash
# From repo root
make build    # builds the CLI binary
make gui      # builds both CLI + GUI binaries
```

Or manually:

```bash
go build -o lite-dev-agent .
cd cmd/gui
go mod tidy
go build -o ../../lite-dev-agent-gui .
```

## Running

```bash
./lite-dev-agent-gui [args passed to lite-dev-agent]
```

The GUI looks for the `lite-dev-agent` binary in:
1. Same directory as the GUI binary (and parent directories)
2. System `PATH`

## Features

- **Scrollable block list** — Each output block (thinking, response, tool I/O) is collapsed by default, click to expand
- **Top bar** — Shows current time, cumulative token counts, and processing status
- **Text input** — Multi-line editor; press **Ctrl+Enter** to send, or click the **Send** button
- **Auto-scroll** — List scrolls to the latest block as the agent processes

## Protocol

The GUI parses the `#!`-prefixed protocol lines from stdout:
- Headers: `#!TIMESTAMP agent: name | level: N | block_type`
- Footers: `#!TIMESTAMP time: duration`
- Content lines between header and footer form the block body
- `waiting_user_input` signals readiness for next prompt
