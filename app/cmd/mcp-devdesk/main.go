package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/web"
)

func main() {
	rootDir, err := locateRoot()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(rootDir, "data", "devdesk")

	app, err := application.New(rootDir, dataDir)
	if err != nil {
		log.Fatalf("initialize application: %v", err)
	}
	defer app.Close()

	cfg := app.Config()
	address := cfg.AdminHost + ":" + strconv.Itoa(cfg.AdminPort)
	server, err := web.New(app, address)
	if err != nil {
		log.Fatalf("initialize web server: %v", err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("MCP DevDesk %s listening on http://%s", application.Version, address)
		serverErrors <- server.ListenAndServe()
	}()

	if cfg.OpenBrowserOnStart {
		go func() {
			if waitForHTTP("http://"+address+"/api/health", 5*time.Second) {
				_ = openBrowser("http://" + address)
			}
		}()
	}

	if cfg.AutoStart {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			if err := app.StartServices(ctx); err != nil {
				log.Printf("auto-start failed: %v", err)
			}
		}()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-signals:
		log.Printf("received signal %s", sig)
	case err := <-serverErrors:
		if err != nil {
			log.Printf("web server stopped: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("shutdown web server: %v", err)
	}
}

func locateRoot() (string, error) {
	if configured := os.Getenv("MCP_DEVDESK_ROOT"); configured != "" {
		return filepath.Abs(configured)
	}

	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd, filepath.Dir(cwd))
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil || seen[absolute] {
			continue
		}
		seen[absolute] = true
		if fileExists(filepath.Join(absolute, "coding-tools-mcp.exe")) && fileExists(filepath.Join(absolute, "cloudflared.exe")) {
			return absolute, nil
		}
	}

	if len(candidates) > 0 {
		return filepath.Abs(candidates[0])
	}
	return "", fmt.Errorf("cannot determine application root")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func waitForHTTP(url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}
