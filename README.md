# lite-dev-agent

A Go CLI that orchestrates LLM agents with configurable tool access. Operates entirely over stdin/stdout.

## Build

```
make
```

Requires Go 1.26+. Produces the `lite-dev-agent` binary.

## Usage

```
./lite-dev-agent [OPTIONS] [ROOT_PATH]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--output` | (all) | Comma-separated list of output sections: `system_prompt`, `user_message`, `agent_response`, `tools_input`, `tools_output`, `thinking` |
| `--devkit-path` | (PATH lookup) | Path to the nixdevkit executable |
| `--resume` | (none) | Path to a conversation log file to resume from |
| `--color` | false | Colorize output with ANSI escape codes |

`ROOT_PATH` is the target project directory. Defaults to current directory.

## Configuration

Place a config file at `ROOT_PATH/.lite-dev-agent/config.yml`.

See `config.example.yml` for a full example.

### LLMs

```yaml
llms:
  - name: thinker
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.6-35B-A3B-Q5_K_M
    api_key: abc
  - name: quick
    default: true
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.5-9B-Q4_K_M
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier |
| `default` | no | Exactly one must be `true` |
| `api_base` | yes | OpenAI-compatible API base URL |
| `model` | no | Model name |
| `api_key` | no | Sent as `Authorization: Bearer <key>` |
| `headers` | no | Extra HTTP headers (takes precedence over `api_key`) |
| `max_tokens` | no | Context window size (fallback: 128k) |

### Agents

```yaml
agents:
  - name: manager
    default: true
    llm: thinker
    tools: agents
    system_prompt: You are the team manager
  - name: searcher
    tools: devkit, online
    expose: File system and web researcher
    system_prompt: You search files and the web
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier |
| `default` | no | Exactly one must be `true`. Entry-point agent. |
| `llm` | no | LLM name. Falls back to default LLM. |
| `tools` | yes | Comma-separated tool groups: `agents`, `devkit`, `online` |
| `expose` | no | If set, this agent is available as a tool to other agents |
| `system_prompt` | yes | System prompt |

### Timeouts

```yaml
timeouts:
  llm_request: 30m
  tool_execution: 10m
```

| Field | Default | Description |
|-------|---------|-------------|
| `llm_request` | 30m | Max wait for a single LLM response |
| `tool_execution` | 10m | Max time for a single tool call |

### Tool groups

| Group | Description |
|-------|-------------|
| `agents` | Exposes all agents with an `expose` field as callable tools |
| `devkit` | File system tools via nixdevkit MCP server (ls, find, read, create, edit, grep, sed, diff, patch, rm, stat) |
| `online` | `online_search` (DuckDuckGo) and `online_fetch` (URL → Markdown) |

## I/O Protocol

Input and output use a structured text format over stdin/stdout.

### Input

Type your message after the `waiting_user_input` header. End with a blank line:

```
# agent: manager | waiting_user_input
What does this project do?
                                    ← blank line ends input
```

### Output

```
# agent: manager | system_prompt
You are the team manager

# agent: manager | user_message
What does this project do?

# agent: manager | tools_input
Tool name: searcher
Argument 1 (prompt): What does this project do?

# agent: searcher | system_prompt
You search files and the web

# agent: searcher | user_message
What does this project do?

# agent: searcher | agent_response
This is a Go CLI tool that orchestrates LLM agents.

# time: 1m32s | input_tokens: 1234 | output_tokens: 23424

# agent: manager | tools_output
Tool name: searcher
Response: This is a Go CLI tool that orchestrates LLM agents.

# time: 1m32s | input_tokens: 1234 | output_tokens: 23424

# agent: manager | agent_response
Based on the research, this project is a Go CLI for orchestrating LLM agents.

# time: 0m45s | input_tokens: 5678 | output_tokens: 312
```

Block types: `system_prompt`, `user_message`, `agent_response`, `tools_input`, `tools_output`, `thinking`.

Use `--output` to filter which blocks are emitted. Example: `--output agent_response` shows only the final responses.

## Conversation Logging

All sessions are logged to `ROOT_PATH/.lite-dev-agent/conversations/YYYY-MM-DD_HH-mm-ss.txt` in the same format as stdout output.

Resume a session with `--resume`:

```
./lite-dev-agent --resume .lite-dev-agent/conversations/2026-04-18_14-30-00.txt /path/to/project
```

New output is appended to the same log file. The agent retains full conversation context from the previous session.

## Test

```
make test
```

## Dependencies

Runtime (optional):
- **nixdevkit** — required only when using `devkit` tools. Must be in `$PATH` or specified via `--devkit-path`.
