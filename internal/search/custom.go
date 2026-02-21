package search

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type CustomHTTPEngine struct {
	name     string
	apiKey   string
	baseURL  string
	enabled  bool
	priority int
	options  map[string]interface{}
	client   *http.Client
}

func NewCustomHTTPEngine(config SearchEngineConfig) (Engine, error) {
	return &CustomHTTPEngine{
		name:     config.Name,
		apiKey:   config.APIKey,
		baseURL:  config.BaseURL,
		enabled:  config.Enabled,
		priority: config.Priority,
		options:  config.Options,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (e *CustomHTTPEngine) Name() string {
	return e.name
}

func (e *CustomHTTPEngine) Type() string {
	return "custom"
}

func (e *CustomHTTPEngine) IsEnabled() bool {
	return e.enabled
}

func (e *CustomHTTPEngine) Priority() int {
	return e.priority
}

func (e *CustomHTTPEngine) Configure(config map[string]interface{}) error {
	if apiKey, ok := config["api_key"].(string); ok {
		e.apiKey = apiKey
	}
	if baseURL, ok := config["base_url"].(string); ok {
		e.baseURL = baseURL
	}
	for k, v := range config {
		e.options[k] = v
	}
	return nil
}

func (e *CustomHTTPEngine) Search(ctx context.Context, query string, limit int) (*SearchResponse, error) {
	return nil, fmt.Errorf("custom engine implementation depends on configuration")
}
