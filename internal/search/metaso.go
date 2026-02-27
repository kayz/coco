package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type MetasoEngine struct {
	name     string
	apiKey   string
	baseURL  string
	enabled  bool
	priority int
	client   *http.Client
}

func NewMetasoEngine(config SearchEngineConfig) (Engine, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://metaso.cn/api/mcp"
	}

	return &MetasoEngine{
		name:     config.Name,
		apiKey:   config.APIKey,
		baseURL:  baseURL,
		enabled:  config.Enabled,
		priority: config.Priority,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (e *MetasoEngine) Name() string {
	return e.name
}

func (e *MetasoEngine) Type() string {
	return "metaso"
}

func (e *MetasoEngine) IsEnabled() bool {
	return e.enabled
}

func (e *MetasoEngine) Priority() int {
	return e.priority
}

func (e *MetasoEngine) Configure(config map[string]interface{}) error {
	if apiKey, ok := config["api_key"].(string); ok {
		e.apiKey = apiKey
	}
	if baseURL, ok := config["base_url"].(string); ok {
		e.baseURL = baseURL
	}
	return nil
}

func (e *MetasoEngine) Search(ctx context.Context, query string, limit int) (*SearchResponse, error) {
	startTime := time.Now()

	requestBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "metaso_web_search",
			"arguments": map[string]interface{}{
				"q":         query,
				"size":      limit,
				"scope":     "webpage",
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	req.Header.Set("User-Agent", "Coco/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jsonrpcResponse struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &jsonrpcResponse); err != nil {
		return nil, fmt.Errorf("failed to parse metaso response: %w", err)
	}

	if jsonrpcResponse.Error != nil {
		return nil, fmt.Errorf("metaso API error: %s", jsonrpcResponse.Error.Message)
	}

	results := make([]SearchResult, 0)
	retrievedAt := time.Now()

	if len(jsonrpcResponse.Result.Content) > 0 {
		var searchResults []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet,omitempty"`
		}
		
		var combinedText string
		for _, c := range jsonrpcResponse.Result.Content {
			if c.Type == "text" {
				combinedText += c.Text
			}
		}
		
		if err := json.Unmarshal([]byte(combinedText), &searchResults); err == nil && len(searchResults) > 0 {
			for _, r := range searchResults {
				results = append(results, SearchResult{
					Title:       r.Title,
					URL:         r.URL,
					Snippet:     r.Snippet,
					Source:      e.name,
					PublishedAt: time.Now(),
					RetrievedAt: retrievedAt,
				})
			}
		} else {
			results = append(results, SearchResult{
				Title:       "秘塔搜索结果",
				URL:         "https://metaso.cn",
				Snippet:     combinedText,
				Source:      e.name,
				PublishedAt: time.Now(),
				RetrievedAt: retrievedAt,
			})
		}
	}

	if len(results) == 0 {
		results = append(results, SearchResult{
			Title:       "搜索成功",
			URL:         "https://metaso.cn",
			Snippet:     "请访问秘塔查看详细结果",
			Source:      e.name,
			PublishedAt: time.Now(),
			RetrievedAt: retrievedAt,
		})
	}

	return &SearchResponse{
		Query:    query,
		Results:  results,
		Engine:   e.name,
		Duration: time.Since(startTime),
	}, nil
}
