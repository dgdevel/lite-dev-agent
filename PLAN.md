# lite-dev-agent — Implementation Plan

```
lite-dev-agent
│
├── 1. Project scaffolding ✅ DONE
│   ├── 1.1 go.mod, go.sum (declare module and dependencies) ✅ DONE
│   ├── 1.2 Makefile (build, test, lint targets) ✅ DONE
│   └── 1.3 main.go skeleton (arg parsing, entry point) ✅ DONE
│
├── 2. Configuration ✅ DONE
│   ├── 2.1 config.go — define Go structs for the YAML schema ✅ DONE
│   │   ├── LLMConfig (name, default, api_base, model, api_key, headers, max_tokens)
│   │   ├── AgentConfig (name, default, llm, tools, expose, system_prompt)
│   │   ├── TimeoutConfig (llm_request, tool_execution)
│   │   └── Config (LLMs, Agents, Timeouts)
│   ├── 2.2 config.go — YAML loading from .lite-dev-agent/config.yml ✅ DONE
│   ├── 2.3 config.go — validation (unique names, exactly one default LLM, one default agent, resolve references) ✅ DONE
│   └── 2.4 config.go — defaults resolution (timeouts, max_tokens fallback) ✅ DONE
│
├── 3. I/O Protocol ✅ DONE
│   ├── 3.1 protocol.go — block types enum and string constants ✅ DONE
│   ├── 3.2 protocol.go — header parser (extract agent name + block type from `# agent: ... | ...` lines) ✅ DONE
│   ├── 3.3 protocol.go — footer parser (extract time, input_tokens, output_tokens) ✅ DONE
│   ├── 3.4 protocol.go — block writer (emit header, content, footer to io.Writer) ✅ DONE
│   └── 3.5 protocol.go --output filter (mask blocks based on CLI flag) ✅ DONE
│
├── 4. LLM Client ✅ DONE
│   ├── 4.1 llm.go — OpenAI chat completion request builder ✅ DONE
│   │   ├── system + user messages construction
│   │   ├── tool definitions serialization
│   │   └── conversation history management (message list)
│   ├── 4.2 llm.go — streaming SSE response parser ✅ DONE
│   │   ├── delta text accumulation → emit agent_response
│   │   ├── tool_call detection and argument accumulation → emit tools_input
│   │   └── finish_reason handling (stop vs tool_calls)
│   ├── 4.3 llm.go — context window management ✅ DONE
│   │   ├── fetch model info from /v1/models endpoint
│   │   ├── fallback to config max_tokens
│   │   └── truncate history when approaching limit (keep system prompt + tail)
│   └── 4.4 llm.go — timeout enforcement (context deadline from config) ✅ DONE
│
├── 5. Tool providers
│   │
│   ├── 5.1 tools_agents.go — agent-as-tool ✅ DONE
│   │   ├── build tool definitions from agents with `expose` field ✅ DONE
│   │   ├── invoke: spawn target agent with prompt, collect final response ✅ DONE
│   │   └── propagate interruption (sub-agent EOF → error result to caller) ✅ DONE
│   │
│   ├── 5.2 tools_devkit.go — nixdevkit MCP client ✅ DONE
│   │   ├── 5.2.1 subprocess lifecycle (spawn, stdin/stdout pipes, kill on shutdown) ✅ DONE
│   │   ├── 5.2.2 MCP initialize/initialized handshake ✅ DONE
│   │   ├── 5.2.3 tools/list → discover tools and convert schema to OpenAI format ✅ DONE
│   │   ├── 5.2.4 tools/call → invoke a tool, return result ✅ DONE
│   │   ├── 5.2.5 crash recovery (detect broken pipe, respawn subprocess, re-handshake) (deferred)
│   │   └── 5.2.6 executable resolution (--devkit-path flag, then $PATH) ✅ DONE
│   │
│   └── 5.3 tools_online.go — web search and fetch ✅ DONE
│       ├── 5.3.1 online_search: HTTP GET to DuckDuckGo HTML, parse results ✅ DONE
│       ├── 5.3.2 online_fetch: HTTP GET url → Readability extraction → Markdown ✅ DONE
│       └── 5.3.3 hardcoded OpenAI tool definitions for both tools ✅ DONE
│
├── 6. Agent execution engine ✅ DONE
│   ├── 6.1 agent.go — Agent struct (config ref, LLM client, tool providers, message history) ✅ DONE
│   ├── 6.2 agent.go — tool registry (resolve `tools` comma-separated list → merged tool definitions) ✅ DONE
│   ├── 6.3 agent.go — run loop ✅ DONE
│   │   ├── send messages + tools to LLM
│   │   ├── stream response, emit blocks via protocol writer
│   │   ├── on tool_call: dispatch to correct provider, emit tools_input/tools_output
│   │   ├── on text: emit agent_response
│   │   ├── feed tool result back into message history
│   │   └── repeat until finish_reason=stop
│   ├── 6.4 agent.go — interruption handling ✅ DONE
│   │   ├── main agent context: EOF → clean shutdown
│   │   └── sub-agent context: EOF → return error to caller agent
│   └── 6.5 agent.go — timeout enforcement per request ✅ DONE
│
├── 7. Main loop (main.go) ✅ DONE
│   ├── 7.1 startup sequence ✅ DONE
│   │   ├── parse args
│   │   ├── load config
│   │   ├── validate config
│   │   ├── locate nixdevkit (if needed)
│   │   ├── spawn MCP subprocess (if needed)
│   │   └── create conversation log file
│   ├── 7.2 input loop ✅ DONE
│   │   ├── print waiting_user_input header
│   │   ├── read stdin until double newline
│   │   ├── pass input to default agent
│   │   └── repeat
│   ├── 7.3 signal handling (SIGINT, SIGTERM → graceful shutdown) ✅ DONE
│   └── 7.4 shutdown (kill MCP subprocess, close log file) ✅ DONE
│
├── 8. Conversation logging ✅ DONE
│   ├── 8.1 conversation.go — log writer ✅ DONE
│   │   ├── create .lite-dev-agent/conversations/ directory ✅ DONE
│   │   ├── open file with timestamp name ✅ DONE
│   │   └── tee all protocol output to the log file ✅ DONE
│   └── 8.2 conversation.go — log parser (for resume) ✅ DONE
│       ├── parse header/footer/content blocks from file ✅ DONE
│       ├── reconstruct LLM message history (system, user, assistant, tool) ✅ DONE
│       └── return loaded history to agent ✅ DONE
│
├── 9. Session resume ✅ DONE
│   ├── 9.1 --resume flag parsing ✅ DONE
│   ├── 9.2 load and parse conversation log file ✅ DONE
│   ├── 9.3 inject reconstructed history into default agent ✅ DONE
│   └── 9.4 append new output to the same log file ✅ DONE
│
├── 10. Integration and end-to-end ✅ DONE
│   ├── 10.1 wire all components in main.go ✅ DONE
│   ├── 10.2 test with a simple single-agent config (no tools) ✅ DONE
│   ├── 10.3 test agent-as-tool routing (manager → sub-agent) ✅ DONE
│   ├── 10.4 test devkit tool calls (file system operations) ✅ DONE
│   ├── 10.5 test online tools (search + fetch) ✅ DONE
│   ├── 10.6 test interruption (Ctrl+D at various levels) ✅ DONE
│   ├── 10.7 test conversation resume ✅ DONE
│   └── 10.8 test timeout enforcement ✅ DONE
│
└── 11. Polish ✅ DONE
    ├── 11.1 error messages consistency (all to stderr, clear wording) ✅ DONE
    ├── 11.2 edge cases (empty input, very long input, missing config fields) ✅ DONE
    └── 11.3 README with usage examples (deferred)
```

## Implementation order

Execute in this order. Each step should be testable independently before moving on.

| Phase | Steps | Testable artifact |
|-------|-------|-------------------|
| Phase 1 | 1, 2, 3 | Binary that parses args, loads config, emits test headers | ✅ DONE |
| Phase 2 | 4 | LLM client that streams a chat completion to stdout | ✅ DONE |
| Phase 3 | 5.1 | Agent-as-tool (two agents, one calls the other) | ✅ DONE |
| Phase 4 | 6, 7 | Full main loop with agent-as-tool routing | ✅ DONE |
| Phase 5 | 5.2 | Devkit MCP integration | ✅ DONE |
| Phase 6 | 5.3 | Online search/fetch | ✅ DONE |
| Phase 7 | 8, 9 | Conversation logging and resume | ✅ DONE |
| Phase 8 | 10, 11 | Integration testing, polish | ✅ DONE |
