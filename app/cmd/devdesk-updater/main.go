package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"mcp-devdesk/internal/selfupdate"
)

func main() {
	packagePath := flag.String("package", "", "verified update package")
	rootDir := flag.String("root", "", "application root directory")
	currentExe := flag.String("current-exe", "", "currently installed manager executable")
	goCore := flag.String("go-core", "", "configured Go core executable")
	legacyCore := flag.String("legacy-core", "", "configured compatibility core executable")
	cloudflared := flag.String("cloudflared", "", "configured cloudflared executable")
	updaterTarget := flag.String("updater-target", "", "installed updater executable")
	waitPID := flag.Int("wait-pid", 0, "manager PID to wait for")
	logPath := flag.String("log", "", "updater log path")
	restartBackground := flag.Bool("background", false, "restart manager in background mode")
	flag.Parse()

	if strings.TrimSpace(*logPath) != "" {
		if err := os.MkdirAll(filepath.Dir(*logPath), 0o700); err == nil {
			if file, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				defer file.Close()
				log.SetOutput(file)
			}
		}
	}
	restartArgs := []string{}
	if *restartBackground {
		restartArgs = append(restartArgs, "--background")
	}
	options := selfupdate.Options{
		PackagePath:       *packagePath,
		RootDir:           *rootDir,
		CurrentExe:        *currentExe,
		GoCoreTarget:      *goCore,
		LegacyCoreTarget:  *legacyCore,
		CloudflaredTarget: *cloudflared,
		UpdaterTarget:     *updaterTarget,
		WaitPID:           *waitPID,
		RestartArgs:       restartArgs,
		LogPath:           *logPath,
	}
	log.Printf("starting update from %s", options.PackagePath)
	if err := selfupdate.Install(options); err != nil {
		log.Printf("update failed: %v", err)
		_, _ = fmt.Fprintf(os.Stderr, "MCP DevDesk update failed: %v\n", err)
		os.Exit(1)
	}
	log.Printf("update completed successfully")
}
