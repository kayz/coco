package search

import (
	"fmt"
	"sync"
)

type Registry struct {
	factories map[string]EngineFactory
	mu        sync.RWMutex
}

func NewRegistry() *Registry {
	r := &Registry{
		factories: make(map[string]EngineFactory),
	}

	r.Register("metaso", NewMetasoEngine)
	r.Register("tavily", NewTavilyEngine)
	r.Register("custom", NewCustomHTTPEngine)
	r.Register("custom_http", NewCustomHTTPEngine)

	return r
}

func (r *Registry) Register(engineType string, factory EngineFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[engineType] = factory
}

func (r *Registry) CreateEngine(config SearchEngineConfig) (Engine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.factories[config.Type]
	if !ok {
		return nil, fmt.Errorf("unknown engine type: %s", config.Type)
	}

	return factory(config)
}

func (r *Registry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}
