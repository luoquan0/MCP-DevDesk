//go:build linux

// MCP DevDesk Server uses the same API and embedded Vue application as Windows.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/buildinfo"
	"mcp-devdesk/internal/config"
	"mcp-devdesk/internal/maintenance"
	"mcp-devdesk/internal/secrets"
	"mcp-devdesk/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	defaultRoot := os.Getenv("MCP_DEVDESK_ROOT")
	if defaultRoot == "" {
		defaultRoot = filepath.Dir(exe)
	}
	rootFlag := flag.String("root", defaultRoot, "installation root (contains binaries and data/devdesk)")
	initFlag := flag.Bool("init", false, "initialize Linux configuration and a web password; then exit")
	showFlag := flag.Bool("show-mcp-credentials", false, "print MCP OAuth credentials to a private local terminal")
	resetFlag := flag.Bool("reset-web-password", false, "reset web password while the server is stopped; then exit")
	lan := flag.Bool("lan", false, "enable private IPv4 LAN access during --init")
	port := flag.Int("web-port", 17861, "web port during --init")
	version := flag.Bool("version", false, "print version and exit")
	_ = flag.Bool("background", false, "compatibility flag; use systemd for background operation")
	flag.Parse()
	if *version {
		fmt.Println("MCP DevDesk Server", buildinfo.Version)
		return nil
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	data := filepath.Join(root, "data", "devdesk")
	if err := os.MkdirAll(data, 0700); err != nil {
		return err
	}
	if err := os.Chmod(data, 0700); err != nil {
		return err
	}
	if err := os.Setenv("MCP_DEVDESK_ROOT", root); err != nil {
		return err
	}
	// The path, not the key, is inherited by managed MCP cores. Keep this file
	// outside the configured workspace and back it up together with encrypted data.
	if os.Getenv("MCP_DEVDESK_KEY_FILE") == "" {
		if err := os.Setenv("MCP_DEVDESK_KEY_FILE", filepath.Join(data, "master.key")); err != nil {
			return err
		}
	}
	if *showFlag {
		if _, err := os.Stat(filepath.Join(data, "secrets.json")); err != nil {
			return errors.New("initialize this installation first")
		}
		values, err := secrets.NewStore(data).GetOrCreate()
		if err != nil {
			return err
		}
		fmt.Println("MCP owner password:", values.OwnerPassword)
		fmt.Println("MCP client ID:", values.ClientID)
		fmt.Println("MCP client secret:", values.ClientSecret)
		return nil
	}
	lock, err := os.OpenFile(filepath.Join(data, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another server is using this data directory; stop it before initializing or resetting credentials")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	cfgPath := filepath.Join(data, "config.json")
	if *initFlag {
		if _, err := os.Stat(cfgPath); err == nil {
			return errors.New("already initialized; existing configuration was not overwritten (use --reset-web-password if needed)")
		} else if !os.IsNotExist(err) {
			return err
		}
		workspace := filepath.Join(root, "workspace")
		if err := os.MkdirAll(workspace, 0700); err != nil {
			return err
		}
		store, err := config.NewStore(root, data)
		if err != nil {
			return err
		}
		cfg := store.Get()
		cfg.Workspace = workspace
		cfg.AllowedRoots = []string{workspace}
		cfg.CoreMode = "go"
		cfg.GoCoreExecutable = filepath.Join(root, "mcp-core")
		cfg.CoreExecutable = filepath.Join(root, "legacy-core-not-supported")
		cfg.CloudflaredExecutable = filepath.Join(root, "cloudflared")
		cfg.OpenBrowserOnStart = false
		cfg.ScreenCaptureEnabled = false
		cfg.WebControlEnabled = true
		cfg.WebControlAuthEnabled = true
		cfg.WebControlLANEnabled = *lan
		cfg.WebControlPort = *port
		if _, err := store.Replace(cfg); err != nil {
			return err
		}
	}
	if *initFlag || *resetFlag {
		password := strings.TrimSpace(os.Getenv("MCP_DEVDESK_WEB_PASSWORD"))
		generated := password == ""
		if generated {
			raw := make([]byte, 24)
			if _, err := rand.Read(raw); err != nil {
				return err
			}
			password = base64.RawURLEncoding.EncodeToString(raw)
		}
		if err := secrets.NewStore(data).SetWebControlPassword(password); err != nil {
			return err
		}
		fmt.Println("Web credentials saved with Linux AES-256-GCM protection.")
		if generated {
			fmt.Println("Web password (save it now):", password)
		}
		fmt.Println("Start: ./mcp-devdesk --root", root)
		return nil
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return errors.New("run ./mcp-devdesk --init first (add --lan for private LAN access)")
	}
	// Do not keep a bootstrap password in the environment of launched tools.
	_ = os.Unsetenv("MCP_DEVDESK_WEB_PASSWORD")
	app, err := application.New(root, data)
	if err != nil {
		return err
	}
	defer app.Close()
	cfg := app.Config()
	if !cfg.WebControlEnabled || !cfg.WebControlAuthEnabled {
		return errors.New("Linux server requires enabled, password-protected Web Control in config.json")
	}
	configured, err := secrets.NewStore(data).WebControlPasswordConfigured()
	if err != nil {
		return err
	}
	if !configured {
		return errors.New("web password is missing; stop the server and run --reset-web-password")
	}
	if cfg.CoreMode != "go" {
		return errors.New("Linux server supports Go core only; set coreMode to go")
	}
	for _, p := range []string{cfg.GoCoreExecutable, cfg.CloudflaredExecutable} {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("not an executable file: %s", p)
		}
	}
	address := cfg.AdminHost + ":" + strconv.Itoa(cfg.AdminPort)
	server, err := web.NewWithDesktop(app, address, nil)
	if err != nil {
		return err
	}
	control := web.NewControlServer(app)
	server.SetControlServer(control)
	handler, err := server.ControlHandler()
	if err != nil {
		return err
	}
	control.SetHandler(handler)
	if err := control.Apply(true, cfg.WebControlPort, cfg.WebControlLANEnabled); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server.SetExitRequest(cancel)
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	go maintenance.RunPeriodic(ctx, data)
	log.Printf("MCP DevDesk Server %s; web http://127.0.0.1:%d/#/ (LAN=%t); internal API %s", buildinfo.Version, cfg.WebControlPort, cfg.WebControlLANEnabled, address)
	go func() {
		if cfg.AutoStart {
			startCtx, done := context.WithTimeout(ctx, 30*time.Second)
			defer done()
			if err := app.StartServices(startCtx); err != nil {
				log.Printf("auto-start: %v", err)
			}
		}
		for _, err := range app.StartAutoProjectInstances(ctx) {
			log.Printf("instance auto-start: %v", err)
		}
	}()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-failures:
	}
	cancel()
	server.BeginShutdown()
	shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	_ = control.Shutdown(shutdownCtx)
	_ = server.Shutdown(shutdownCtx)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}
