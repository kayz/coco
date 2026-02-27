package debug

import (
	"log"
	"os"
)

// enabled is set via ldflags for debug builds
var enabled = ""

// Enabled controls whether debug messages are printed
var Enabled = false

func init() {
	// Enable debug via ldflags (build-debug target)
	if enabled == "true" {
		Enabled = true
	}
	// Enable debug via environment variable (overrides ldflags)
	if os.Getenv("COCO_DEBUG") == "1" {
		Enabled = true
	}
	// Log startup message if debug is enabled
	if Enabled {
		log.Printf("[DEBUG] Debug mode enabled")
	}
}

// Log prints a debug message if debug mode is enabled
func Log(format string, args ...any) {
	if Enabled {
		log.Printf("[DEBUG] "+format, args...)
	}
}
