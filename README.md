# lite-dev-agent

A web-based LLM agent orchestrator with configurable tool access and a browser UI.

## Build

```
make
```

Requires Go 1.26+. Produces the `lite-dev-agent` binary.

## Usage

```
./lite-dev-agent [OPTIONS] [ROOT_PATH]
```

Starts an HTTP server with a browser-based UI for interacting with agents.

| Option | Default | Description |
|--------|---------|-------------|
| `--listen` | `:8080` | Listen address in `[address]:[port]` format (e.g. `:8080`, `127.0.0.1:3000`) |

`ROOT_PATH` is the target project directory. Defaults to current directory.

Conversations are stored under `ROOT_PATH/.lite-dev-agent/conversations/` and can be resumed from the web UI.

## Configuration

Configuration is loaded from two locations and merged:
2. **Local config**: `ROOT_PATH/.lite-dev-agent/config.yml`

If both exist, the local config overrides the global config. Merge rules:

- **LLMs, MCPs, Agents**: entries are matched by `name`. Local entries override global entries with the same name. New names are appended. Unmatched global entries are kept.
- **Timeouts**: non-empty local fields override global fields.
- **Blocks**: local entries override global entries for the same block type key.

At least one config file must exist.

See `config.template.yml` for a full example.

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

### MCP Servers

```yaml
mcp:
  - name: devkit
    type: stdio
    command: "nixdevkit %s"
  - name: devkit_safe
    type: stdio
    command: "nixdevkit %s"
    allow:
      - ls
      - find
      - read
      - grep
  - name: remote
    type: http
    url: http://localhost:8080/mcp
    headers:
      Authorization: Bearer token123
    deny:
      - dangerous_tool
  - name: prefixed
    prefix: "fs_"
    type: stdio
    command: "nixdevkit %s"
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier. Referenced in agent `tools` and used as the tool group name. |
| `type` | yes | `stdio` or `http` |
| `command` | stdio | Command to spawn. `%s` is replaced by `ROOT_PATH`. Supports quoted arguments. |
| `url` | http | MCP server URL |
| `headers` | no | HTTP headers (for `http` type) |
| `prefix` | no | Prefix prepended to all tool names from this server (default: empty) |
| `allow` | no | Whitelist: only these tools are exposed. Mutually exclusive with `deny`. |
| `deny` | no | Blacklist: these tools are hidden. Mutually exclusive with `allow`. |

### Agents

```yaml
agents:
  - name: manager
    default: true
    llm: thinker
    tools: agents
    system_prompt: You are the team manager
  - name: searcher
    tools: devkit
    expose: File system researcher
    system_prompt: You search files
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier |
| `default` | no | Exactly one must be `true`. Entry-point agent. |
| `llm` | no | LLM name. Falls back to default LLM. |
| `tools` | yes | Comma-separated tool groups: MCP server names, `agents`, `ask` |
| `expose` | no | If set, this agent is available as a tool to other agents |
| `system_prompt` | yes | System prompt. Supports `{current_date}` and `{current_time}` variables. |
| `initial_tool_calls` | no | Tool calls to execute automatically at the start of a new conversation (see below) |
| `final_tool_calls` | no | Tool calls to execute automatically after the agent's final response (see below) |

### Initial Tool Calls

You can configure an agent to automatically execute tool calls right after the first user message in a new conversation. This is useful for injecting project context (e.g., listing files, reading README) before the agent begins reasoning.

```yaml
agents:
  - name: dev
    default: true
    tools: devkit
    system_prompt: You are a developer assistant.
    initial_tool_calls:
      - tool: ls
        arguments:
          path: .
      - tool: read
        arguments:
          path: README.md
```

Each entry has:

| Field | Required | Description |
|-------|----------|-------------|
| `tool` | yes | Tool name (must be available in the agent's tool groups) |
| `arguments` | no | Key-value map passed as JSON arguments to the tool. String values support placeholders (see below) |

The tool calls and their results are emitted as `tools_input`/`tools_output` blocks and injected into the message history as if the agent had made those calls. This only runs on the first message of a new conversation (not on resume).

### Final Tool Calls

You can configure an agent to automatically execute tool calls after it produces its final response, before returning control to the user. This is useful for side effects like logging, notifications, or post-processing (e.g., storing the conversation summary).

```yaml
agents:
  - name: dev
    default: true
    tools: devkit, ask
    system_prompt: You are a developer assistant.
    final_tool_calls:
      - tool: write
        arguments:
          path: SUMMARY.md
          content: "%c"
```

The configuration format is the same as `initial_tool_calls`. Final tool calls execute after the agent's last LLM response and their results are appended to the message history.

### Tool Call Placeholders

Both `initial_tool_calls` and `final_tool_calls` support these placeholders in string argument values:

| Placeholder | Available in | Description |
|-------------|-------------|-------------|
| `%p` | initial, final | Replaced with the user's original prompt |
| `%c` | final only | Replaced with the formatted conversation log (user requests, agent responses, tool call/response pairs) |

Example:
```yaml
final_tool_calls:
  - tool: write
    arguments:
      path: "chat-log.md"
      content: "%c"
```

### Prompt variables

These variables are interpolated at runtime in `system_prompt`:

| Variable | Example value | Description |
|----------|---------------|-------------|
| `{current_date}` | `2026-04-18` | Current date in ISO 8601 format |
| `{current_time}` | `2026-04-18T14:30:05` | Current date and time in ISO 8601 format |

Example:
```yaml
system_prompt: >
  You are an assistant. Today is {current_date},
  current time is {current_time}. Help the user.
```

### AGENTS.md

If an `AGENTS.md` file exists in `ROOT_PATH`, its contents are automatically appended to every agent's system prompt (after the config-defined `system_prompt`, under a `# AGENTS.md` heading). This is useful for injecting project-specific conventions, coding standards, or context that all agents should follow.

Example `AGENTS.md`:

```markdown
- All Go code must pass `gofmt` and `go vet`.
- Use structured logging via `slog`.
- Keep functions under 50 lines.
```

No configuration needed — just drop the file in the project root. If absent, nothing changes.

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
|-------|------|-------------|
| `agents` | built-in | Exposes all agents with an `expose` field as callable tools |
| `ask` | built-in | Interactive tools: `ask_open_ended`, `ask_multiple_choice`, `ask_exec` |
| `<mcp name>` | MCP | Any MCP server defined in the `mcp` section |


## Token Statistics

When `token_stats` is included in the output (default), an ASCII tree is emitted after each agent response showing prompt and completion token counts for every LLM call, including nested sub-agent calls:

```
#! agent: manager | level: 0 | token_stats
manager          prompt: 2500     completion: 180
├── searcher     prompt: 1200     completion: 95
│   └── indexer  prompt: 400      completion: 30
└── analyst      prompt: 800      completion: 60
```

Each line shows the agent name, total prompt tokens, and total completion tokens across all LLM requests made by that agent. The tree structure reflects the nesting of sub-agent tool calls.

This uses the OpenAI-compatible `stream_options: {"include_usage": true}` parameter to retrieve token counts from the server.

## Conversation Logging
When resuming, all previous conversation blocks are replayed to stdout so that downstream consumers (e.g., the GUI) can reconstruct the full conversation history. The `waiting_user_input` blocks are excluded from the replay.

## Test

```
make test
```

