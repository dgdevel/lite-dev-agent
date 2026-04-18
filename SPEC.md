# lite-dev-agent — Specification

A Go CLI tool that orchestrates LLM agents with configurable tool access. Operates entirely over stdin/stdout.

---

## 1. CLI Interface

```
./lite-dev-agent [OPTIONS] [ROOT_PATH]
```

### Positional Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `ROOT_PATH` | No | Target project directory. Defaults to current working directory. |

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--output` | (all) | Comma-separated list of output sections to emit. Filters which blocks appear on stdout. |
| `--devkit-path` | (lookup in `$PATH`) | Path to the nixdevkit MCP server executable. If not provided, `nixdevkit` must be discoverable in `$PATH`. |
| `--resume` | (none) | Path to a conversation log file to restore session context from. See §12. |
| `--color` | false | Colorize output using ANSI escape codes. |

### `--color` mapping

| Output | Color |
|--------|-------|
| Header lines (`# agent: ...`) | Yellow |
| Footer lines (`# time: ...`) | Yellow |
| `user_message` content | White |
| `agent_response` content | White |
| `thinking` content | Light red |
| `tools_input` content | Light green |
| `tools_output` content | Light green |

Colors are applied only to stdout. Conversation log files are always uncolored.

### `--output` filter values

| Value | What it shows |
|-------|---------------|
| `system_prompt` | The system prompt sent to an agent |
| `user_message` | The user message / prompt sent to an agent |
| `agent_response` | The LLM text response from an agent |
| `tools_input` | Tool call request (agent name + arguments) |
| `tools_output` | Tool call response (agent name + result) |
| `thinking` | Thinking/reasoning tokens from the LLM |

When `--output` is not specified, all sections are emitted.

---

## 2. I/O Protocol

All communication happens over stdin/stdout. No network server, no TUI.

### 2.1 Output Format

Output is a sequence of **blocks**. Each block is:

```
# <header-line>
<content>
```

A block starts with a header line prefixed by `# ` and ends when the next header line or footer line appears.

#### Header lines

```
# agent: <agent_name> | <block_type>
```

Where `<block_type>` is one of: `system_prompt`, `user_message`, `agent_response`, `tools_input`, `tools_output`, `thinking`.

#### Footer lines

```
# time: <duration> | input_tokens: <N> | output_tokens: <N>
```

Footer lines are optional. They report timing and token usage for the preceding block.

#### Example: agent with tool call and response

```
# agent: manager | system_prompt
You are the team manager, analyze requests and route it to the proper team agent

# agent: manager | user_message
How does the project get built?

# agent: manager | tools_input
Tool name: project_searcher
Argument 1: How does the project get built?

# agent: project_searcher | system_prompt
You search the project file system, analyze request and search the project using the tools

# agent: project_searcher | user_message
How does the project get built?

# agent: project_searcher | agent_response
The build is made using `make`, with customization in the `Makefile`.

# time: 1m32s | input_tokens: 1234 | output_tokens: 23424

# agent: manager | tools_output
Tool name: project_searcher
Response:
The build is made using `make`, with customization in the `Makefile`.

# time: 1m32s | input_tokens: 1234 | output_tokens: 23424

# agent: manager | agent_response
The project uses `make` as its build system, with configuration in the `Makefile`.

# time: 0m45s | input_tokens: 5678 | output_tokens: 312
```

### 2.2 Input Format

The program reads input from stdin after printing a prompt header:

```
# agent: <agent_name> | waiting_user_input
```

The user types their input. End of input is signaled by **two consecutive newlines** (i.e. a blank line).

```
# agent: manager | waiting_user_input
How does the project get built?
[blank line signals end of input]
```

---

## 3. Configuration

Configuration is loaded from `ROOT_PATH/.lite-dev-agent/config.yml`.

The `.lite-dev-agent/` directory also stores conversation logs (see §12).

### 3.1 Top-level structure

```yaml
llms:
  - name: <string>
    default: <bool>
    api_base: <string>       # mandatory
    model: <string>
    api_key: <string>
    headers:
      <key>: <value>
    max_tokens: <int>        # optional, fallback if endpoint doesn't report it
  ...

agents:
  - name: <string>
    default: <bool>
    llm: <string>            # references an LLM by name
    tools: <string>          # comma-separated tool group names
    expose: <string>         # optional description when exposed as tool to other agents
    system_prompt: <string>
  ...

timeouts:
  llm_request: <duration>    # default: 30m
  tool_execution: <duration> # default: 10m
```

### 3.2 `llms` section

Defines one or more LLM backends. All LLMs communicate via OpenAI-compatible chat completion API.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for this LLM configuration. |
| `default` | No | Marks this as the default LLM. Exactly one should be `true`. |
| `api_base` | Yes | Base URL for the OpenAI-compatible API (e.g. `http://127.0.0.1:12345/v1`). |
| `model` | No | Model name to request. If omitted, the API default is used. |
| `api_key` | No | API key sent as `Authorization: Bearer <api_key>`. |
| `headers` | No | Additional HTTP headers to include in every request. |
| `max_tokens` | No | Maximum context tokens for this model. Used for context window management. If not set, the system attempts to fetch it from the `/v1/models` endpoint. Falls back to a generous default (128k). |

**Authentication priority**: If both `api_key` and `headers.Authorization` are set, `headers` takes precedence (allowing custom schemes).

### 3.3 `agents` section

Defines the agents in the system. Each agent is an independent LLM conversation with its own system prompt and tool access.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for this agent. Used in output headers. |
| `default` | No | Marks this as the entry-point agent. Exactly one should be `true`. The user's stdin input goes to this agent. |
| `llm` | No | Reference to an LLM by `name`. Falls back to the default LLM. |
| `tools` | Yes | Comma-separated list of tool groups this agent can use. See §4. |
| `expose` | No | If set, this agent is available as a tool to other agents. The value is the tool description presented to the calling LLM. |
| `system_prompt` | Yes | The system prompt for this agent. |

### 3.4 `timeouts` section

Configurable timeouts with generous defaults suited for local LLM inference.

| Field | Default | Description |
|-------|---------|-------------|
| `llm_request` | `30m` | Maximum time to wait for a single LLM chat completion response (including streaming). |
| `tool_execution` | `10m` | Maximum time for a single tool execution (MCP call, agent-as-tool, online search/fetch). |

Duration format: Go-style duration strings (`30m`, `1h`, `30m30s`).

### 3.5 Example configuration

```yaml
llms:
  - name: thinker
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.6-35B-A3B-Q5_K_M
    api_key: abc
    headers:
      Authorization: Bearer abc
  - name: quick
    default: true
    api_base: http://127.0.0.1:12345/v1
    model: qwen3.5-9B-Q4_K_M

agents:
  - name: manager
    default: true
    llm: thinker
    tools: agents
    system_prompt: >
      You are the team manager, analyze requests and route it to the proper team agent
  - name: project_searcher
    tools: devkit
    expose: File System researcher
    system_prompt: >
      You search the project file system, analyze request and search the project using the tools
  - name: online_searcher
    tools: online
    expose: Online researcher
    system_prompt: >
      Analyze the request and search online using the tools
  - name: researcher
    llm: thinker
    tools: devkit, online
    expose: Full researcher with file system and online access
    system_prompt: >
      You research the project file system and the internet to answer questions

timeouts:
  llm_request: 30m
  tool_execution: 10m
```

---

## 4. Tool Groups

Tools are grouped. Each agent specifies one or more tool groups via the `tools` field (comma-separated).

Example: `tools: devkit, online` grants access to both file system and web tools.

Tool calls are always executed **sequentially**. If the LLM returns multiple tool calls in a single response, they are processed one at a time in order. This keeps the system simple and debuggable.

### 4.1 `agents` — Agent-as-tool

Every agent that has an `expose` field becomes available as a tool. The tool is presented to the LLM with:

- **Tool name**: the agent's `name`
- **Description**: the agent's `expose` value
- **Parameters**: a single `prompt` string argument

When the calling LLM invokes this tool, the target agent is executed with the given prompt as its user message. The agent runs to completion (including any sub-calls), and the final text response is returned as the tool result.

### 4.2 `devkit` — File system tools (MCP)

Spawns the nixdevkit MCP server as a subprocess (stdio transport) with `ROOT_PATH` as the root directory. Exposes all tools from nixdevkit:

| Tool | Description |
|------|-------------|
| `ls` | List directory content |
| `find` | Find files by glob pattern |
| `read` | Read file content |
| `create` | Create a new file |
| `edit` | Replace a file section |
| `grep` | Search file contents by regex |
| `sed` | Search and replace in files |
| `diff` | Compare two files (unified diff) |
| `patch` | Apply a unified diff |
| `rm` | Delete a file or directory |
| `stat` | File/directory metadata |
| `available_commands` | List user-defined commands |
| `exec_command` | Run a user-defined command |

The nixdevkit executable is located via:
1. `--devkit-path` CLI flag, if provided.
2. `$PATH` lookup for `nixdevkit`.

nixdevkit is **not** a build dependency of lite-dev-agent. It must be available at runtime only when an agent uses `devkit` tools.

### 4.3 `online` — Web search tools

Two built-in tools:

| Tool | Arguments | Description |
|------|-----------|-------------|
| `online_search` | `query` (string) | Searches the web using a free scraping-based search engine (e.g. DuckDuckGo HTML). Returns a list of results with titles, URLs, and snippets. |
| `online_fetch` | `url` (string) | Downloads the URL content, passes it through Mozilla Readability for content extraction, and converts to Markdown. Returns the cleaned article content. |

---

## 5. Execution Flow

### 5.1 Startup

1. Parse CLI arguments (`--output`, `--devkit-path`, `--resume`, `ROOT_PATH`).
2. Load configuration from `ROOT_PATH/.lite-dev-agent/config.yml`.
3. Resolve the default agent.
4. If any agent in the configuration uses `devkit` tools, locate the nixdevkit executable and spawn the MCP server subprocess.
5. If `--resume` is provided, load conversation history from the specified file (see §12).
6. Print `# agent: <default_agent> | waiting_user_input` and wait for stdin.

### 5.2 Agent execution

1. Send the system prompt and user message to the LLM (via OpenAI chat completion API with tool definitions).
2. Stream the response:
   - If the LLM returns text, emit it as an `agent_response` block.
   - If the LLM returns a tool call:
     a. Emit `tools_input` block.
     b. Execute the tool.
     c. Emit `tools_output` block.
     d. Feed the tool result back to the LLM and continue the conversation loop.
3. Repeat until the LLM produces a final text response with no more tool calls.
4. Print `# agent: <default_agent> | waiting_user_input` and wait for next stdin input.

#### Interruption handling

- **stdin EOF (Ctrl+D)** in the **main agent** context: the program terminates cleanly.
- **stdin EOF (Ctrl+D)** in a **sub-agent** context: an error is propagated up to the calling agent as a tool error result. The calling agent receives a message like `"agent interrupted by user"` and can decide how to proceed.

### 5.3 Agent-to-agent calls

When an agent calls another agent as a tool:

1. The calling agent's LLM emits a tool call with the target agent's name and a `prompt` argument.
2. The target agent starts a new conversation with its own system prompt and the `prompt` as user message.
3. The target agent may itself call tools (including other agents), producing nested blocks.
4. When the target agent completes, its final text response is returned as the tool result to the calling agent.
5. Output blocks for the target agent are nested within the parent agent's output, using the same header/footer format.

### 5.4 Shutdown

- On EOF from stdin (at main agent level), the program exits cleanly.
- SIGINT/SIGTERM trigger graceful shutdown (stop active LLM requests, kill MCP subprocess).
- On shutdown, the conversation log is finalized and closed.

---

## 6. LLM Communication

### 6.1 API

All LLM interactions use the OpenAI Chat Completions API format:

```
POST <api_base>/chat/completions
```

Request body:

```json
{
  "model": "<model>",
  "messages": [
    {"role": "system", "content": "<system_prompt>"},
    {"role": "user", "content": "<user_message>"},
    ...conversation history...
    {"role": "tool", "content": "<tool_result>", "tool_call_id": "<id>"}
  ],
  "tools": [...],
  "stream": true
}
```

Streaming is used for all requests. The response is parsed incrementally to detect tool calls vs text content.

### 6.2 Model context window

The system needs to know the model's maximum context window to manage conversation history length.

1. On first use of an LLM, attempt to fetch the model's context window from `GET <api_base>/models`.
2. If the endpoint doesn't exist or doesn't return usable data, fall back to the `max_tokens` field in the LLM configuration.
3. If neither is available, fall back to a default of 128000 tokens.

When the conversation approaches the context limit, older messages are truncated (keeping the system prompt and most recent messages).

### 6.3 Tool definitions (OpenAI format)

For agent-as-tool:

```json
{
  "type": "function",
  "function": {
    "name": "<agent_name>",
    "description": "<expose value>",
    "parameters": {
      "type": "object",
      "properties": {
        "prompt": {
          "type": "string",
          "description": "The request to send to the agent"
        }
      },
      "required": ["prompt"]
    }
  }
}
```

For devkit tools: definitions are fetched from the nixdevkit MCP server via the `tools/list` method and converted to OpenAI tool format.

For online tools: hardcoded definitions.

---

## 7. MCP Integration (devkit)

### 7.1 Lifecycle

- The nixdevkit server is spawned as a child process with stdio transport.
- Communication uses the MCP protocol over stdin/stdout of the child process.
- The server is initialized on startup and terminated on shutdown.

### 7.2 Protocol

MCP JSON-RPC messages over stdio:

1. `initialize` → `initialized` handshake.
2. `tools/list` to discover available tools and their schemas.
3. `tools/call` to invoke tools during agent execution.

### 7.3 Tool schema conversion

nixdevkit tools are defined in MCP format (JSON Schema). They must be converted to OpenAI function-calling format before being sent to the LLM.

---

## 8. Error Handling

| Scenario | Behavior |
|----------|----------|
| Config file not found | Print error to stderr, exit 1. |
| Invalid YAML | Print parse error to stderr, exit 1. |
| No default agent defined | Print error to stderr, exit 1. |
| No default LLM defined | Print error to stderr, exit 1. |
| nixdevkit not found (when needed) | Print error to stderr, exit 1. |
| LLM API unreachable | Print error to stderr, continue waiting for input. |
| LLM request timeout | Return timeout error as tool result or print to stderr. |
| MCP subprocess crash | Restart the subprocess. Log warning to stderr. |
| Tool execution error | Return error message as tool result, let the LLM decide how to proceed. |
| Tool execution timeout | Return timeout error as tool result. |

---

## 9. Project Structure (proposed)

```
lite-dev-agent/
├── main.go                  # CLI entry point, argument parsing
├── config.go                # YAML configuration loading and types
├── agent.go                 # Agent execution loop (LLM conversation)
├── llm.go                   # OpenAI-compatible API client (streaming)
├── tools_agents.go          # Agent-as-tool provider
├── tools_devkit.go          # MCP/nixdevkit tool provider
├── tools_online.go          # Online search/fetch tool provider
├── protocol.go              # I/O protocol: header/footer parsing and output formatting
├── conversation.go          # Conversation log file writing and reading
├── go.mod
├── go.sum
├── Makefile
└── SPEC.md                  # This file
```

nixdevkit is **not** a subdirectory or build dependency. It is a separate project that must be installed separately and available at runtime.

---

## 10. Dependencies (Go)

| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go` | MCP client for communicating with nixdevkit |
| `gopkg.in/yaml.v3` | YAML configuration parsing |
| Standard `net/http` | OpenAI API calls and online tools |
| Standard `encoding/json` | JSON handling |

Additional dependency for online tools:

| Package | Purpose |
|---------|---------|
| Mozilla Readability library (Go) | Content extraction for `online_fetch` |
| HTTP client | DuckDuckGo HTML scraping for `online_search` |

---

## 11. Design Decisions

### Sequential tool calls only

Tool calls are processed one at a time. If an LLM returns multiple tool calls in a single response, they are executed sequentially in order. This ensures:
- Predictable, debuggable output.
- Clear causal ordering in conversation logs.
- No concurrency complexity for stateful tools (file system operations).

### No config hot-reload

Configuration is loaded once at startup. To apply config changes, restart the program.

---

## 12. Conversation Logging and Session Resume

### 12.1 Conversation logs

All I/O (both stdin input and stdout output) is logged to conversation files stored in the project's `.lite-dev-agent/` directory.

**File path**: `ROOT_PATH/.lite-dev-agent/conversations/YYYY-MM-DD_HH-mm-ss.txt`

The log format is identical to the stdout protocol format. The file contains a full transcript of the session, including all headers, content blocks, and footers.

### 12.2 Session resume

The `--resume` flag restores a session from a previous conversation log:

```
./lite-dev-agent --resume .lite-dev-agent/conversations/2026-04-18_14-30-00.txt [ROOT_PATH]
```

When resuming:
1. The conversation log is parsed to extract the message history of the default agent.
2. `system_prompt` and `user_message` blocks are converted back into the LLM message history.
3. `agent_response` blocks are added as `assistant` messages.
4. `tools_input` / `tools_output` pairs are reconstructed as assistant tool-call messages and tool result messages.
5. The resumed session continues from the last state — the agent has full context of what happened before.
6. New conversation output is **appended** to the same log file.

### 12.3 Log format details

The conversation log file uses the same header/content/footer format as stdout. This ensures:
- Logs are human-readable.
- Logs can be used directly for debugging.
- The resume parser reuses the same protocol parser.
