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

type TavilyEngine struct {
	name     string
	apiKey   string
	baseURL  string
	enabled  bool
	priority int
	client   *http.Client
}

func NewTavilyEngine(config SearchEngineConfig) (Engine, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.tavily.com"
	}

	return &TavilyEngine{
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

func (e *TavilyEngine) Name() string {
	return e.name
}

func (e *TavilyEngine) Type() string {
	return "tavily"
}

func (e *TavilyEngine) IsEnabled() bool {
	return e.enabled
}

func (e *TavilyEngine) Priority() int {
	return e.priority
}

func (e *TavilyEngine) Configure(config map[string]interface{}) error {
	if apiKey, ok := config["api_key"].(string); ok {
		e.apiKey = apiKey
	}
	if baseURL, ok := config["base_url"].(string); ok {
		e.baseURL = baseURL
	}
	return nil
}

func (e *TavilyEngine) Search(ctx context.Context, query string, limit int) (*SearchResponse, error) {
	startTime := time.Now()

	searchURL := fmt.Sprintf("%s/search", e.baseURL)

	requestBody := map[string]interface{}{
		"api_key":        e.apiKey,
		"query":          query,
		"search_depth":   "basic",
		"include_answer": false,
		"include_images": false,
		"max_results":    limit,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", searchURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
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

	var apiResponse struct {
		Results []struct {
			Title     string  `json:"title"`
			URL       string  `json:"url"`
			Content   string  `json:"content"`
			Score     float64 `json:"score"`
			Published string  `json:"published_date,omitempty"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(apiResponse.Results))
	retrievedAt := time.Now()

	for _, r := range apiResponse.Results {
		publishedAt := time.Now()
		if r.Published != "" {
			if t, err := time.Parse(time.RFC3339, r.Published); err == nil {
				publishedAt = t
			}
		}

		results = append(results, SearchResult{
			Title:       r.Title,
			URL:         r.URL,
			Snippet:     r.Content,
			Source:      e.name,
			PublishedAt: publishedAt,
			RetrievedAt: retrievedAt,
			Score:       r.Score,
		})
	}

	return &SearchResponse{
		Query:    query,
		Results:  results,
		Engine:   e.name,
		Duration: time.Since(startTime),
	}, nil
}
