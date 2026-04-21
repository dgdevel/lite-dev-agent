package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var buildOnce sync.Once
var binaryPath string

func buildBinary(t *testing.T) {
	t.Helper()
	buildOnce.Do(func() {
		bin, err := filepath.Abs("lite-dev-agent-test")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "build", "-o", bin, ".")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build failed: %v\n%s", err, output)
		}
		binaryPath = bin
	})
}

func writeTestConfig(t *testing.T, dir, apiBase string) {
	t.Helper()
	cfgDir := filepath.Join(dir, ".lite-dev-agent")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`llms:
  - name: test-llm
    default: true
    api_base: %s
    model: test-model
agents:
  - name: dev
    default: true
    tools: ask
    system_prompt: You are a test assistant.
`, apiBase)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
}

func newMockLLMServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		t.Logf("LLM request body length: %d", len(body))

		w.Header().Set("Content-Type", "text/event-stream")
		sseData := fmt.Sprintf(`data: {"choices":[{"finish_reason":null,"index":0,"delta":{"role":"assistant","content":null}}]}

data: {"choices":[{"finish_reason":null,"index":0,"delta":{"content":"%s"}}]}

data: {"choices":[{"finish_reason":"stop","index":0,"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}

data: [DONE]
`, responseContent)
		fmt.Fprint(w, sseData)
	}))
}

func TestPromptFlagSendsMessage(t *testing.T) {
	buildBinary(t)

	server := newMockLLMServer(t, "Hello from agent")
	defer server.Close()

	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, server.URL)

	cmd := exec.Command(binaryPath, "--prompt", "test prompt", "--color", "false", tmpDir)
	cmd.Stdin = strings.NewReader("")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, output)
	}

	stdout := string(output)
	if !strings.Contains(stdout, "Hello from agent") {
		t.Fatalf("expected agent response in output, got:\n%s", stdout)
	}
}

func TestPromptFlagNoAskTools(t *testing.T) {
	buildBinary(t)

	var receivedBody []byte
	var bodyOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyOnce.Do(func() {
			receivedBody = body
		})

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"finish_reason":null,"index":0,"delta":{"role":"assistant","content":null}}]}

data: {"choices":[{"finish_reason":"stop","index":0,"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}

data: [DONE]
`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, server.URL)

	cmd := exec.Command(binaryPath, "--prompt", "test", "--color", "false", tmpDir)
	cmd.Stdin = strings.NewReader("")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, output)
	}

	var reqBody map[string]any
	if err := json.Unmarshal(receivedBody, &reqBody); err != nil {
		t.Fatalf("parse request body: %v", err)
	}

	tools, ok := reqBody["tools"].([]any)
	if !ok {
		return
	}

	for _, tool := range tools {
		if tm, ok := tool.(map[string]any); ok {
			if fn, ok := tm["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok && strings.HasPrefix(name, "ask_") {
					t.Fatalf("ask tool %q should not be present with -prompt flag", name)
				}
			}
		}
	}
}

func TestPromptFlagNoWaitingInput(t *testing.T) {
	buildBinary(t)

	server := newMockLLMServer(t, "response")
	defer server.Close()

	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, server.URL)

	cmd := exec.Command(binaryPath, "--prompt", "test", "--color", "false", tmpDir)
	cmd.Stdin = strings.NewReader("")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, output)
	}

	if strings.Contains(string(output), "waiting_user_input") {
		t.Fatalf("waiting_user_input should not appear with -prompt flag, got:\n%s", output)
	}
}

func TestPromptFlagTerminates(t *testing.T) {
	buildBinary(t)

	server := newMockLLMServer(t, "done")
	defer server.Close()

	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, server.URL)

	done := make(chan struct{})
	var output []byte
	var cmdErr error

	go func() {
		cmd := exec.Command(binaryPath, "--prompt", "test", "--color", "false", tmpDir)
		cmd.Stdin = strings.NewReader("")
		output, cmdErr = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
		if cmdErr != nil {
			t.Fatalf("command failed: %v\noutput:\n%s", cmdErr, output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command did not terminate within 10s")
	}
}

func TestPromptFlagInHelp(t *testing.T) {
	buildBinary(t)

	cmd := exec.Command(binaryPath, "--help")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "-prompt") {
		t.Fatalf("-prompt flag not found in help output:\n%s", output)
	}
}

func TestPromptFlagWithEmptyString(t *testing.T) {
	buildBinary(t)

	tmpDir := t.TempDir()
	writeTestConfig(t, tmpDir, "http://127.0.0.1:1")

	cmd := exec.Command(binaryPath, "--prompt", "", "--color", "false", tmpDir)
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(), "TMPDIR="+tmpDir)

	done := make(chan struct{})
	go func() {
		cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("empty -prompt should start interactive mode (hang reading stdin)")
	}
}
