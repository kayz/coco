package search

import (
	"sync"

	"github.com/pltanton/lingti-bot/internal/config"
)

var (
	globalManager *Manager
	managerOnce   sync.Once
	managerErr    error
)

func InitGlobalManager(cfg config.SearchConfig) error {
	managerOnce.Do(func() {
		registry := NewRegistry()
		globalManager, managerErr = NewManager(cfg, registry)
	})
	return managerErr
}

func GetGlobalManager() *Manager {
	return globalManager
}

func IsSearchManagerReady() bool {
	return globalManager != nil
}
