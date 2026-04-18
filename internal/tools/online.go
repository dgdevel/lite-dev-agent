package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/dgdevel/lite-dev-agent/internal/agent"
	"github.com/dgdevel/lite-dev-agent/internal/llm"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

type OnlineProvider struct {
	timeout time.Duration
}

func NewOnlineProvider(timeout time.Duration) *OnlineProvider {
	return &OnlineProvider{timeout: timeout}
}

func (p *OnlineProvider) ToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Type: "function",
			Function: llm.Function{
				Name:        "online_search",
				Description: "Search the web using DuckDuckGo. Returns a list of results with titles, URLs, and snippets.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The search query",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "online_fetch",
				Description: "Download a URL and extract the main content as Markdown using readability.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "The URL to fetch",
						},
					},
					"required": []string{"url"},
				},
			},
		},
	}
}

func (p *OnlineProvider) CallTool(ctx context.Context, name string, arguments string) (agent.ToolResult, error) {
	args, err := llm.ParseToolCallArguments(arguments)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	switch name {
	case "online_search":
		query := llm.GetArgumentString(args, "query")
		if query == "" {
			return agent.ToolResult{Content: "missing query argument", IsError: true}, nil
		}
		return p.search(callCtx, query)
	case "online_fetch":
		fetchURL := llm.GetArgumentString(args, "url")
		if fetchURL == "" {
			return agent.ToolResult{Content: "missing url argument", IsError: true}, nil
		}
		return p.fetch(callCtx, fetchURL)
	default:
		return agent.ToolResult{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}, nil
	}
}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (p *OnlineProvider) search(ctx context.Context, query string) (agent.ToolResult, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("search request error: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; lite-dev-agent/1.0)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("search error: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}

	results := parseDDGResults(string(body))
	if len(results) == 0 {
		return agent.ToolResult{Content: "No results found."}, nil
	}

	var lines []string
	for i, r := range results {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, r.Title, r.URL, r.Snippet))
	}

	return agent.ToolResult{Content: strings.Join(lines, "\n\n")}, nil
}

func (p *OnlineProvider) fetch(ctx context.Context, fetchURL string) (agent.ToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("fetch request error: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; lite-dev-agent/1.0)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("fetch error: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agent.ToolResult{Content: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status), IsError: true}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		markdown := htmlToMarkdown(string(body), fetchURL)
		return agent.ToolResult{Content: markdown}, nil
	}

	return agent.ToolResult{Content: string(body)}, nil
}

var (
	reResultBlock = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>.*?<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	reStripTags   = regexp.MustCompile(`<[^>]+>`)
	reDecodeAmp   = regexp.MustCompile(`&amp;`)
	reDDGRedirect = regexp.MustCompile(`//duckduckgo.com/l/\?uddg=([^&"']+)`)
)

func parseDDGResults(html string) []searchResult {
	var results []searchResult

	matches := reResultBlock.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		href := m[1]
		title := stripTags(m[2])
		snippet := stripTags(m[3])

		href = decodeDDGRedirect(href)

		if href == "" || title == "" {
			continue
		}

		results = append(results, searchResult{
			Title:   strings.TrimSpace(title),
			URL:     strings.TrimSpace(href),
			Snippet: strings.TrimSpace(snippet),
		})
	}

	return results
}

func decodeDDGRedirect(href string) string {
	m := reDDGRedirect.FindStringSubmatch(href)
	if len(m) > 1 {
		if decoded, err := url.QueryUnescape(m[1]); err == nil {
			return decoded
		}
	}
	return href
}

func stripTags(s string) string {
	s = reStripTags.ReplaceAllString(s, "")
	s = reDecodeAmp.ReplaceAllString(s, "&")
	s = unescapeHTML(s)
	return s
}

func htmlToMarkdown(html, pageURL string) string {
	title := extractTag(html, "title")

	bodyStart := strings.Index(html, "<body")
	if bodyStart == -1 {
		bodyStart = 0
	}
	body := html[bodyStart:]

	paragraphs := extractParagraphs(body)
	content := strings.Join(paragraphs, "\n\n")

	if len(content) > 50000 {
		content = content[:50000] + "\n\n... (truncated)"
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
		sb.WriteString("Source: ")
		sb.WriteString(pageURL)
		sb.WriteString("\n\n---\n\n")
	}
	sb.WriteString(content)

	return sb.String()
}

func extractTag(html, tag string) string {
	re := regexp.MustCompile(`(?i)<` + tag + `[^>]*>(.*?)</` + tag + `>`)
	m := re.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(stripTags(m[1]))
	}
	return ""
}

func extractParagraphs(html string) []string {
	re := regexp.MustCompile(`(?is)<(?:p|h[1-6]|li|blockquote)[^>]*>(.*?)</(?:p|h[1-6]|li|blockquote)>`)
	matches := re.FindAllStringSubmatch(html, -1)

	var paragraphs []string
	for _, m := range matches {
		text := stripTags(m[1])
		text = strings.TrimSpace(text)
		text = compressWhitespace(text)
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}

	if len(paragraphs) == 0 {
		text := stripTags(html)
		text = compressWhitespace(text)
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}

	return paragraphs
}

func compressWhitespace(s string) string {
	re := regexp.MustCompile(`\s+`)
	s = re.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func unescapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}
