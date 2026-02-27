package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

const (
	ModeRelay  = "relay"
	ModeKeeper = "keeper"
	ModeBoth   = "both"
)

// ValidateMode normalizes and validates a service mode.
func ValidateMode(mode string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		m = ModeRelay
	}
	switch m {
	case ModeRelay, ModeKeeper, ModeBoth:
		return m, nil
	default:
		return "", fmt.Errorf("invalid mode %q: must be one of relay, keeper, both", mode)
	}
}

// ServiceID returns launchd label (darwin) or systemd unit base name (linux).
func ServiceID(mode string) (string, error) {
	m, err := ValidateMode(mode)
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("com.kayz.coco.%s", m), nil
	case "linux":
		return fmt.Sprintf("coco-%s", m), nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Paths returns installation paths for the selected mode.
func Paths(mode string) (binaryPath, configPath string, err error) {
	m, err := ValidateMode(mode)
	if err != nil {
		return "", "", err
	}

	switch runtime.GOOS {
	case "darwin":
		return "/Library/PrivilegedHelperTools/com.kayz.coco",
			fmt.Sprintf("/Library/LaunchDaemons/com.kayz.coco.%s.plist", m), nil
	case "linux":
		return "/usr/local/bin/coco",
			fmt.Sprintf("/etc/systemd/system/coco-%s.service", m), nil
	default:
		return "", "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// IsInstalled checks whether a mode service is installed.
func IsInstalled(mode string) bool {
	binaryPath, configPath, err := Paths(mode)
	if err != nil {
		return false
	}

	if _, err := os.Stat(configPath); err != nil {
		return false
	}
	_, err = os.Stat(binaryPath)
	return err == nil
}

// IsRunning checks if a mode service is running.
func IsRunning(mode string) bool {
	serviceID, err := ServiceID(mode)
	if err != nil {
		return false
	}

	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("launchctl", "list", serviceID)
		return cmd.Run() == nil
	case "linux":
		cmd := exec.Command("systemctl", "is-active", "--quiet", serviceID)
		return cmd.Run() == nil
	default:
		return false
	}
}

// Install installs service for a specific mode.
func Install(sourceBinary, mode string) error {
	m, err := ValidateMode(mode)
	if err != nil {
		return err
	}
	binaryPath, configPath, err := Paths(m)
	if err != nil {
		return err
	}

	// Copy binary
	if err := copyBinary(sourceBinary, binaryPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Create service config
	if err := createServiceConfig(configPath, binaryPath, m); err != nil {
		return fmt.Errorf("failed to create service config: %w", err)
	}

	// Load/enable service
	if err := enableService(m); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	return nil
}

// Uninstall removes service for a specific mode.
func Uninstall(mode string) error {
	m, err := ValidateMode(mode)
	if err != nil {
		return err
	}

	// Stop service first
	_ = Stop(m)

	binaryPath, configPath, err := Paths(m)
	if err != nil {
		return err
	}
	serviceID, err := ServiceID(m)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		exec.Command("launchctl", "unload", configPath).Run()
	case "linux":
		exec.Command("systemctl", "disable", serviceID).Run()
		exec.Command("systemctl", "daemon-reload").Run()
	}

	// Remove mode config.
	os.Remove(configPath)

	// Remove shared binary only when no other mode service remains.
	if !hasOtherInstalledMode(m) {
		os.Remove(binaryPath)
	}

	return nil
}

// Start starts service for a specific mode.
func Start(mode string) error {
	m, err := ValidateMode(mode)
	if err != nil {
		return err
	}
	serviceID, err := ServiceID(m)
	if err != nil {
		return err
	}
	_, configPath, err := Paths(m)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("launchctl", "load", configPath).Run()
	case "linux":
		return exec.Command("systemctl", "start", serviceID).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Stop stops service for a specific mode.
func Stop(mode string) error {
	m, err := ValidateMode(mode)
	if err != nil {
		return err
	}
	serviceID, err := ServiceID(m)
	if err != nil {
		return err
	}
	_, configPath, err := Paths(m)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("launchctl", "unload", configPath).Run()
	case "linux":
		return exec.Command("systemctl", "stop", serviceID).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Restart restarts service for a specific mode.
func Restart(mode string) error {
	if err := Stop(mode); err != nil {
		// Ignore stop error, service might not be running
	}
	return Start(mode)
}

func copyBinary(src, dst string) error {
	// Ensure directory exists
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Read source
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Write destination
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return err
	}

	return nil
}

func createServiceConfig(configPath, binaryPath, mode string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return createLaunchdPlist(configPath, binaryPath, mode)
	case "linux":
		return createSystemdUnit(configPath, binaryPath, mode)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func enableService(mode string) error {
	m, err := ValidateMode(mode)
	if err != nil {
		return err
	}
	serviceID, err := ServiceID(m)
	if err != nil {
		return err
	}
	_, configPath, err := Paths(m)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("launchctl", "load", configPath).Run()
	case "linux":
		if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
			return err
		}
		return exec.Command("systemctl", "enable", serviceID).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>{{.Mode}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/coco.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/coco.log</string>
</dict>
</plist>
`

func createLaunchdPlist(configPath, binaryPath, mode string) error {
	tmpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		return err
	}

	f, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	serviceID, err := ServiceID(mode)
	if err != nil {
		return err
	}

	return tmpl.Execute(f, map[string]string{
		"Label":      serviceID,
		"BinaryPath": binaryPath,
		"Mode":       mode,
	})
}

const systemdUnitTemplate = `[Unit]
Description=Coco {{.ModeTitle}} Service
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} {{.Mode}}
Restart=always
RestartSec=5
StandardOutput=append:/tmp/coco.log
StandardError=append:/tmp/coco.log

[Install]
WantedBy=multi-user.target
`

func createSystemdUnit(configPath, binaryPath, mode string) error {
	tmpl, err := template.New("unit").Parse(systemdUnitTemplate)
	if err != nil {
		return err
	}

	f, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, map[string]string{
		"BinaryPath": binaryPath,
		"Mode":       mode,
		"ModeTitle":  modeTitle(mode),
	})
}

func hasOtherInstalledMode(currentMode string) bool {
	for _, mode := range []string{ModeRelay, ModeKeeper, ModeBoth} {
		if mode == currentMode {
			continue
		}
		_, configPath, err := Paths(mode)
		if err != nil {
			continue
		}
		if _, err := os.Stat(configPath); err == nil {
			return true
		}
	}
	return false
}

func modeTitle(mode string) string {
	if mode == "" {
		return ""
	}
	return strings.ToUpper(mode[:1]) + mode[1:]
}
