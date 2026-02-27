package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/kayz/coco/internal/service"
)

const serviceActionHelp = "Service action: install, uninstall, start, stop, restart, status"

func runModeServiceAction(mode, action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return false
	}

	normalizedMode, err := service.ValidateMode(mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch action {
	case "install":
		execPath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting executable path: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Installing %s service...\n", normalizedMode)
		if err := service.Install(execPath, normalizedMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error installing service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service installed successfully!")
	case "uninstall":
		fmt.Printf("Uninstalling %s service...\n", normalizedMode)
		if err := service.Uninstall(normalizedMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error uninstalling service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service uninstalled successfully!")
	case "start":
		if err := service.Start(normalizedMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service started!")
	case "stop":
		if err := service.Stop(normalizedMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service stopped!")
	case "restart":
		if err := service.Restart(normalizedMode); err != nil {
			fmt.Fprintf(os.Stderr, "Error restarting service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service restarted!")
	case "status":
		installed := service.IsInstalled(normalizedMode)
		running := service.IsRunning(normalizedMode)
		serviceID, _ := service.ServiceID(normalizedMode)
		binaryPath, configPath, err := service.Paths(normalizedMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading service paths: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("=== coco Service Status ===")
		fmt.Println()
		fmt.Printf("Mode:      %s\n", normalizedMode)
		fmt.Printf("Service:   %s\n", serviceID)
		fmt.Printf("Installed: %v\n", installed)
		fmt.Printf("Running:   %v\n", running)
		fmt.Println()
		fmt.Printf("Binary:    %s\n", binaryPath)
		fmt.Printf("Config:    %s\n", configPath)
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported service action %q\n", action)
		fmt.Fprintln(os.Stderr, "Supported actions: install, uninstall, start, stop, restart, status")
		os.Exit(1)
	}

	return true
}
