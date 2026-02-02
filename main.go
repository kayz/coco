package main

import "github.com/pltanton/lingti-bot/cmd"

// Build is set via ldflags at build time
var Build = "unknown"

func main() {
	cmd.SetBuild(Build)
	cmd.Execute()
}
