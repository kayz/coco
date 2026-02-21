package search

import "context"

type Engine interface {
	Name() string
	Type() string
	Search(ctx context.Context, query string, limit int) (*SearchResponse, error)
	IsEnabled() bool
	Priority() int
	Configure(config map[string]interface{}) error
}

type EngineFactory func(config SearchEngineConfig) (Engine, error)

type SearchEngineConfig struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`
	APIKey     string                 `yaml:"api_key,omitempty"`
	BaseURL    string                 `yaml:"base_url,omitempty"`
	Enabled    bool                   `yaml:"enabled"`
	Priority   int                    `yaml:"priority"`
	Options    map[string]interface{} `yaml:"options,omitempty"`
}
