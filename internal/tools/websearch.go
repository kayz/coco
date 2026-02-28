package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kayz/coco/internal/config"
	"github.com/kayz/coco/internal/search"
	"github.com/kayz/coco/internal/security"
	"github.com/mark3labs/mcp-go/mcp"
)

var (
	searchInitialized bool
)

func initSearch() error {
	if searchInitialized {
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := search.InitGlobalManager(cfg.Search); err != nil {
		return err
	}

	searchInitialized = true
	return nil
}

func WebSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := req.Params.Arguments["query"].(string)
	if !ok || query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	limit := 5
	if l, ok := req.Params.Arguments["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	if err := initSearch(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search engines not configured: %v\n\nPlease run 'coco.exe onboard' to configure your search engines.", err)), nil
	}

	manager := search.GetGlobalManager()
	if manager == nil {
		return mcp.NewToolResultError("search manager not available. Please configure search engines via 'coco.exe onboard'."), nil
	}

	var resultText string

	if search.IsExplicitSearchRequest(query) {
		resp, err := manager.SearchAll(ctx, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		resultText = search.FormatCombinedResults(resp)
	} else {
		resp, err := manager.Search(ctx, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}
		resultText = search.FormatSearchResults(resp)
	}

	return mcp.NewToolResultText(resultText), nil
}

func WebFetch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	urlStr, ok := req.Params.Arguments["url"].(string)
	if !ok || urlStr == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}
	if cfg.Security.EnableSSRFProtection {
		if err := security.ValidateFetchURL(urlStr); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("url blocked by SSRF protection: %v", err)), nil
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req2, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid URL: %v", err)), nil
	}
	req2.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Coco/1.0)")

	resp, err := client.Do(req2)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetch failed: %v", err)), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read response: %v", err)), nil
	}

	content := string(body)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		content = extractTextFromHTML(content)
	}

	if len(content) > 10000 {
		content = content[:10000] + "\n... (truncated)"
	}

	return mcp.NewToolResultText(content), nil
}

func extractTextFromHTML(html string) string {
	for _, tag := range []string{"script", "style", "noscript"} {
		for {
			start := strings.Index(strings.ToLower(html), "<"+tag)
			if start == -1 {
				break
			}
			end := strings.Index(strings.ToLower(html[start:]), "</"+tag+">")
			if end == -1 {
				break
			}
			html = html[:start] + html[start+end+len("</"+tag+">"):]
		}
	}

	text := stripTags(html)

	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}

func stripTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}
