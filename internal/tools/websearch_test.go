package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestWebFetchBlocksSSRFLocalhost(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"url": "http://127.0.0.1:8080",
	}

	result, err := WebFetch(context.Background(), req)
	if err != nil {
		t.Fatalf("WebFetch returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected tool result")
	}
	if !result.IsError {
		t.Fatalf("expected SSRF-blocked URL to return tool error")
	}
}
