package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOnlineToolDefinitions(t *testing.T) {
	p := NewOnlineProvider(10 * time.Second)
	defs := p.ToolDefinitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	if defs[0].Function.Name != "online_search" {
		t.Fatalf("first tool: %s", defs[0].Function.Name)
	}
	if defs[1].Function.Name != "online_fetch" {
		t.Fatalf("second tool: %s", defs[1].Function.Name)
	}
}

func TestOnlineSearchInvalidArgs(t *testing.T) {
	p := NewOnlineProvider(5 * time.Second)
	result, err := p.CallTool(context.Background(), "online_search", "not json")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error")
	}
}

func TestOnlineSearchMissingQuery(t *testing.T) {
	p := NewOnlineProvider(5 * time.Second)
	result, err := p.CallTool(context.Background(), "online_search", `{"wrong": "arg"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for missing query")
	}
}

func TestOnlineFetchMissingURL(t *testing.T) {
	p := NewOnlineProvider(5 * time.Second)
	result, err := p.CallTool(context.Background(), "online_fetch", `{"wrong": "arg"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for missing url")
	}
}

func TestOnlineUnknownTool(t *testing.T) {
	p := NewOnlineProvider(5 * time.Second)
	result, err := p.CallTool(context.Background(), "unknown_tool", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for unknown tool")
	}
}

func TestOnlineSearchWithMock(t *testing.T) {
	ddgHTML := `
<html><body>
<div>
<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage1">Example Result 1</a>
<a class="result__snippet">This is snippet one</a>
</div>
<div>
<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage2">Example Result 2</a>
<a class="result__snippet">This is snippet two</a>
</div>
</body></html>
`
	results := parseDDGResults(ddgHTML)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Example Result 1" {
		t.Fatalf("title: %s", results[0].Title)
	}
	if results[0].URL != "https://example.com/page1" {
		t.Fatalf("url: %s", results[0].URL)
	}
	if results[0].Snippet != "This is snippet one" {
		t.Fatalf("snippet: %s", results[0].Snippet)
	}
}

func TestOnlineFetchWithMock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><p>Hello world paragraph</p></body></html>`))
	}))
	defer server.Close()

	p := NewOnlineProvider(5 * time.Second)

	oldClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = oldClient }()

	result, err := p.CallTool(context.Background(), "online_fetch", `{"url": "`+server.URL+`/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Test Page") {
		t.Fatalf("missing title: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello world") {
		t.Fatalf("missing content: %s", result.Content)
	}
}

func TestOnlineFetchPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text content"))
	}))
	defer server.Close()

	p := NewOnlineProvider(5 * time.Second)

	oldClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = oldClient }()

	result, err := p.CallTool(context.Background(), "online_fetch", `{"url": "`+server.URL+`/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "plain text content") {
		t.Fatalf("missing content: %s", result.Content)
	}
}

func TestOnlineFetchHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	p := NewOnlineProvider(5 * time.Second)

	oldClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = oldClient }()

	result, err := p.CallTool(context.Background(), "online_fetch", `{"url": "`+server.URL+`/test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("should be error for 404")
	}
}

func TestParseDDGResultsEmpty(t *testing.T) {
	results := parseDDGResults("<html><body></body></html>")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseDDGResults(t *testing.T) {
	html := `
<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com">Example Title</a>
<a class="result__snippet">Example snippet text</a>
`
	results := parseDDGResults(html)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Example Title" {
		t.Fatalf("title: %s", results[0].Title)
	}
	if results[0].URL != "https://example.com" {
		t.Fatalf("url: %s", results[0].URL)
	}
	if results[0].Snippet != "Example snippet text" {
		t.Fatalf("snippet: %s", results[0].Snippet)
	}
}

func TestStripTags(t *testing.T) {
	if stripTags("<b>hello</b>") != "hello" {
		t.Fatal("stripTags failed")
	}
	if stripTags("no tags") != "no tags" {
		t.Fatal("stripTags failed on plain text")
	}
}

func TestCompressWhitespace(t *testing.T) {
	got := compressWhitespace("  hello  \n  world  ")
	if got != "hello world" {
		t.Fatalf("compressWhitespace: %q", got)
	}
}

func TestExtractTag(t *testing.T) {
	html := "<html><head><title>My Title</title></head></html>"
	if extractTag(html, "title") != "My Title" {
		t.Fatalf("extractTag: %q", extractTag(html, "title"))
	}
}

func TestUnescapeHTML(t *testing.T) {
	if unescapeHTML("&lt;b&gt;hello&amp;world&lt;/b&gt;") != "<b>hello&world</b>" {
		t.Fatalf("unescapeHTML: %q", unescapeHTML("&lt;b&gt;hello&amp;world&lt;/b&gt;"))
	}
}
