package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/pltanton/lingti-bot/internal/voice"
	"github.com/spf13/cobra"
)

var (
	setupComponent string
	setupAll       bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup voice dependencies",
	Long: `Setup and install dependencies for voice features.

This command helps install:
  - Audio recording tools (sox, ffmpeg, alsa-utils)
  - Speech-to-text engine (whisper-cpp)
  - Whisper models for transcription

Examples:
  lingti-bot setup              # Check status and show instructions
  lingti-bot setup --all        # Install all dependencies
  lingti-bot setup -c whisper   # Download whisper model only
  lingti-bot setup -c audio     # Install audio tools only`,
	Run: runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)

	setupCmd.Flags().StringVarP(&setupComponent, "component", "c", "", "Component to setup: whisper, audio, all")
	setupCmd.Flags().BoolVar(&setupAll, "all", false, "Install all dependencies")
}

func runSetup(cmd *cobra.Command, args []string) {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Lingti Bot - Voice Setup                      ║")
	fmt.Printf("║              Platform: %-35s ║\n", runtime.GOOS+"/"+runtime.GOARCH)
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	if setupAll {
		setupComponent = "all"
	}

	switch setupComponent {
	case "whisper":
		setupWhisperModel()
	case "audio":
		setupAudioTools()
	case "all":
		setupAudioTools()
		fmt.Println()
		setupWhisper()
		fmt.Println()
		setupWhisperModel()
	default:
		// Show status
		checkStatus()
	}
}

func checkStatus() {
	fmt.Println("📋 Checking dependencies...")
	fmt.Println()

	// Check audio recording
	fmt.Println("🎤 Audio Recording:")
	audioOK := false
	switch runtime.GOOS {
	case "darwin":
		if checkCommand("rec") {
			fmt.Println("   ✅ sox (rec) - installed")
			audioOK = true
		} else if checkCommand("ffmpeg") {
			fmt.Println("   ✅ ffmpeg - installed")
			audioOK = true
		} else {
			fmt.Println("   ❌ No audio recorder found")
		}
	case "linux":
		if checkCommand("arecord") {
			fmt.Println("   ✅ alsa-utils (arecord) - installed")
			audioOK = true
		} else if checkCommand("rec") {
			fmt.Println("   ✅ sox (rec) - installed")
			audioOK = true
		} else {
			fmt.Println("   ❌ No audio recorder found")
		}
	case "windows":
		if checkCommand("ffmpeg") {
			fmt.Println("   ✅ ffmpeg - installed")
			audioOK = true
		} else {
			fmt.Println("   ❌ ffmpeg not found")
		}
	}

	// Check whisper
	fmt.Println()
	fmt.Println("🗣️ Speech-to-Text (Whisper):")
	whisperOK := false
	for _, name := range []string{"whisper-cli", "whisper", "whisper-cpp"} {
		if checkCommand(name) {
			fmt.Printf("   ✅ %s - installed\n", name)
			whisperOK = true
			break
		}
	}
	if !whisperOK {
		fmt.Println("   ❌ whisper not found")
	}

	// Check whisper model
	fmt.Println()
	fmt.Println("📦 Whisper Model:")
	modelPath := voice.FindWhisperModel()
	if modelPath != "" {
		fmt.Printf("   ✅ Model found: %s\n", modelPath)
	} else {
		fmt.Println("   ❌ No model found")
	}

	// Check audio playback
	fmt.Println()
	fmt.Println("🔊 Audio Playback:")
	playbackOK := false
	switch runtime.GOOS {
	case "darwin":
		if checkCommand("afplay") {
			fmt.Println("   ✅ afplay - installed (built-in)")
			playbackOK = true
		}
	case "linux":
		if checkCommand("aplay") {
			fmt.Println("   ✅ aplay - installed")
			playbackOK = true
		} else if checkCommand("paplay") {
			fmt.Println("   ✅ paplay - installed")
			playbackOK = true
		} else if checkCommand("mpv") {
			fmt.Println("   ✅ mpv - installed")
			playbackOK = true
		}
	case "windows":
		fmt.Println("   ✅ PowerShell - built-in")
		playbackOK = true
	}
	if !playbackOK && runtime.GOOS == "linux" {
		fmt.Println("   ❌ No audio player found")
	}

	// Summary
	fmt.Println()
	fmt.Println("────────────────────────────────────────────────────────────────")
	if audioOK && whisperOK && modelPath != "" {
		fmt.Println("✅ All dependencies are installed! Run: lingti-bot voice")
	} else {
		fmt.Println("❌ Some dependencies are missing. Run: lingti-bot setup --all")
	}
	fmt.Println()

	// Show platform-specific instructions
	if !audioOK || !whisperOK || modelPath == "" {
		showInstallInstructions()
	}
}

func setupAudioTools() {
	fmt.Println("🎤 Setting up audio tools...")

	switch runtime.GOOS {
	case "darwin":
		if checkCommand("rec") {
			fmt.Println("   ✅ sox already installed")
			return
		}
		if checkCommand("brew") {
			fmt.Println("   Installing sox via Homebrew...")
			runCommand("brew", "install", "sox")
		} else {
			fmt.Println("   ❌ Homebrew not found. Install manually:")
			fmt.Println("      brew install sox")
		}

	case "linux":
		if checkCommand("arecord") {
			fmt.Println("   ✅ alsa-utils already installed")
			return
		}
		// Try different package managers
		if checkCommand("apt-get") {
			fmt.Println("   Installing alsa-utils via apt...")
			runCommand("sudo", "apt-get", "install", "-y", "alsa-utils", "sox")
		} else if checkCommand("dnf") {
			fmt.Println("   Installing alsa-utils via dnf...")
			runCommand("sudo", "dnf", "install", "-y", "alsa-utils", "sox")
		} else if checkCommand("pacman") {
			fmt.Println("   Installing alsa-utils via pacman...")
			runCommand("sudo", "pacman", "-S", "--noconfirm", "alsa-utils", "sox")
		} else {
			fmt.Println("   ❌ Could not detect package manager. Install manually:")
			fmt.Println("      sudo apt install alsa-utils sox  # Debian/Ubuntu")
			fmt.Println("      sudo dnf install alsa-utils sox  # Fedora")
			fmt.Println("      sudo pacman -S alsa-utils sox    # Arch")
		}

	case "windows":
		if checkCommand("ffmpeg") {
			fmt.Println("   ✅ ffmpeg already installed")
			return
		}
		if checkCommand("winget") {
			fmt.Println("   Installing ffmpeg via winget...")
			runCommand("winget", "install", "ffmpeg")
		} else if checkCommand("choco") {
			fmt.Println("   Installing ffmpeg via chocolatey...")
			runCommand("choco", "install", "ffmpeg", "-y")
		} else {
			fmt.Println("   ❌ Install ffmpeg manually:")
			fmt.Println("      winget install ffmpeg")
			fmt.Println("      # or download from https://ffmpeg.org/download.html")
		}
	}
}

func setupWhisper() {
	fmt.Println("🗣️ Setting up Whisper...")

	// Check if already installed
	for _, name := range []string{"whisper-cli", "whisper", "whisper-cpp"} {
		if checkCommand(name) {
			fmt.Printf("   ✅ %s already installed\n", name)
			return
		}
	}

	switch runtime.GOOS {
	case "darwin":
		if checkCommand("brew") {
			fmt.Println("   Installing whisper-cpp via Homebrew...")
			runCommand("brew", "install", "whisper-cpp")
		} else {
			fmt.Println("   ❌ Homebrew not found. Install manually:")
			fmt.Println("      brew install whisper-cpp")
		}

	case "linux":
		// whisper-cpp is not in most distro repos, suggest pip or build from source
		if checkCommand("pip3") {
			fmt.Println("   Installing openai-whisper via pip...")
			runCommand("pip3", "install", "openai-whisper")
		} else {
			fmt.Println("   ❌ Install whisper manually:")
			fmt.Println("      pip3 install openai-whisper")
			fmt.Println("      # or build whisper.cpp from source:")
			fmt.Println("      git clone https://github.com/ggerganov/whisper.cpp")
			fmt.Println("      cd whisper.cpp && make")
		}

	case "windows":
		if checkCommand("pip") {
			fmt.Println("   Installing openai-whisper via pip...")
			runCommand("pip", "install", "openai-whisper")
		} else {
			fmt.Println("   ❌ Install whisper manually:")
			fmt.Println("      pip install openai-whisper")
			fmt.Println("      # or download from https://github.com/ggerganov/whisper.cpp/releases")
		}
	}
}

func setupWhisperModel() {
	fmt.Println("📦 Setting up Whisper model...")

	// Check if model already exists
	modelPath := voice.FindWhisperModel()
	if modelPath != "" {
		fmt.Printf("   ✅ Model already exists: %s\n", modelPath)
		return
	}

	// Download model
	fmt.Println("   Downloading ggml-base.bin (141MB)...")
	err := voice.DownloadWhisperModel("base")
	if err != nil {
		fmt.Printf("   ❌ Download failed: %v\n", err)
		fmt.Println("   Download manually from:")
		fmt.Println("      https://huggingface.co/ggerganov/whisper.cpp/tree/main")
		return
	}

	fmt.Println("   ✅ Model downloaded successfully")
}

func showInstallInstructions() {
	fmt.Println()
	fmt.Println("📖 Installation Instructions:")
	fmt.Println()

	switch runtime.GOOS {
	case "darwin":
		fmt.Println("macOS:")
		fmt.Println("  # Install Homebrew (if not installed)")
		fmt.Println("  /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"")
		fmt.Println()
		fmt.Println("  # Install dependencies")
		fmt.Println("  brew install sox whisper-cpp")
		fmt.Println()
		fmt.Println("  # Download whisper model")
		fmt.Println("  lingti-bot setup -c whisper")

	case "linux":
		fmt.Println("Linux (Debian/Ubuntu):")
		fmt.Println("  sudo apt update")
		fmt.Println("  sudo apt install alsa-utils sox python3-pip")
		fmt.Println("  pip3 install openai-whisper")
		fmt.Println()
		fmt.Println("Linux (Fedora):")
		fmt.Println("  sudo dnf install alsa-utils sox python3-pip")
		fmt.Println("  pip3 install openai-whisper")
		fmt.Println()
		fmt.Println("Linux (Arch):")
		fmt.Println("  sudo pacman -S alsa-utils sox python-pip")
		fmt.Println("  pip install openai-whisper")
		fmt.Println()
		fmt.Println("  # Download whisper model")
		fmt.Println("  lingti-bot setup -c whisper")

	case "windows":
		fmt.Println("Windows:")
		fmt.Println("  # Install via winget (Windows 11 / Windows 10 with winget)")
		fmt.Println("  winget install ffmpeg")
		fmt.Println("  winget install Python.Python.3")
		fmt.Println("  pip install openai-whisper")
		fmt.Println()
		fmt.Println("  # Or install via Chocolatey")
		fmt.Println("  choco install ffmpeg python")
		fmt.Println("  pip install openai-whisper")
		fmt.Println()
		fmt.Println("  # Download whisper model")
		fmt.Println("  lingti-bot setup -c whisper")
	}
}

func checkCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Printf("   ⚠️ Command failed: %v\n", err)
	}
}
