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
	"strings"
	"sync"
	"syscall"
	"time"

	"mcp-devdesk/internal/application"
	"mcp-devdesk/internal/desktop"
	devlogging "mcp-devdesk/internal/logging"
	"mcp-devdesk/internal/web"
)

func main() {
	background := hasArgument("--background")
	rootDir, err := locateRoot()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(rootDir, "data", "devdesk")

	app, err := application.New(rootDir, dataDir)
	if err != nil {
		log.Fatalf("initialize application: %v", err)
	}
	closeLog := configureLogging(dataDir, func() bool { return app.Config().LoggingEnabled })
	defer closeLog()
	defer app.Close()

	cfg := app.Config()
	address := cfg.AdminHost + ":" + strconv.Itoa(cfg.AdminPort)
	dashboardURL := "http://" + address

	alreadyRunning, releaseInstance, instanceErr := desktop.AcquireSingleInstance()
	if instanceErr != nil {
		log.Printf("single-instance protection unavailable: %v", instanceErr)
	} else {
		defer releaseInstance()
	}
	if alreadyRunning {
		signaled := desktop.SignalExistingInstance()
		if signaled {
			return
		}
		if waitForHTTP(dashboardURL+"/api/health", 3*time.Second) {
			if err := desktop.OpenDashboard(dashboardURL); err != nil {
				log.Printf("open existing dashboard: %v", err)
			}
		} else {
			log.Printf("existing instance did not expose its tray window or management endpoint")
		}
		return
	}

	executable, _ := os.Executable()
	controller := desktop.New(dashboardURL, executable, dataDir, desktop.Callbacks{
		Start: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := app.StartServices(ctx); err != nil {
				log.Printf("tray start services: %v", err)
			}
		},
		Stop: func() {
			if err := app.StopServices(); err != nil {
				log.Printf("tray stop services: %v", err)
			}
		},
		Restart: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			if err := app.RestartServices(ctx); err != nil {
				log.Printf("tray restart services: %v", err)
			}
		},
	})
	if err := controller.Start(); err != nil {
		log.Printf("desktop tray unavailable: %v", err)
	} else {
		log.Printf("desktop tray initialized: %s", controller.Status().WindowModeLabel)
	}
	defer controller.Close()

	server, err := web.NewWithDesktop(app, address, controller)
	if err != nil {
		log.Fatalf("initialize web server: %v", err)
	}
	exitRequested := make(chan struct{}, 1)
	server.SetExitRequest(func() {
		select {
		case exitRequested <- struct{}{}:
		default:
		}
	})
	controlServer := web.NewControlServer(app)
	server.SetControlServer(controlServer)
	controlHandler, err := server.ControlHandler()
	if err != nil {
		log.Fatalf("initialize web control handler: %v", err)
	}
	controlServer.SetHandler(controlHandler)
	if err := controlServer.Apply(cfg.WebControlEnabled, cfg.WebControlPort, cfg.WebControlLANEnabled); err != nil {
		log.Printf("web control startup failed: %v", err)
	} else if cfg.WebControlEnabled {
		log.Printf("web control listening on port %d (LAN=%t)", cfg.WebControlPort, cfg.WebControlLANEnabled)
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("MCP DevDesk %s listening on %s", application.Version, dashboardURL)
		serverErrors <- server.ListenAndServe()
	}()
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	var backgroundWG sync.WaitGroup
	launchBackground := func(run func(context.Context)) {
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			run(backgroundCtx)
		}()
	}

	if cfg.OpenBrowserOnStart && !background {
		launchBackground(func(ctx context.Context) {
			if waitForHTTP(dashboardURL+"/api/health", 5*time.Second) {
				if ctx.Err() != nil {
					return
				}
				if err := controller.Open(); err != nil {
					log.Printf("open desktop window: %v", err)
				}
			}
		})
	}

	if cfg.AutoStart {
		launchBackground(func(parent context.Context) {
			ctx, cancel := context.WithTimeout(parent, 25*time.Second)
			defer cancel()
			if err := app.StartServices(ctx); err != nil {
				log.Printf("auto-start failed: %v", err)
			}
		})
	}
	launchBackground(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		for _, err := range app.StartAutoInstances(ctx) {
			log.Printf("instance auto-start failed: %v", err)
		}
	})

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-signals:
		log.Printf("received signal %s", sig)
	case <-exitRequested:
		log.Printf("application exit requested for update")
	case <-controller.Done():
		log.Printf("desktop exit requested")
	case err := <-serverErrors:
		if err != nil {
			log.Printf("web server stopped: %v", err)
		}
	}
	backgroundCancel()
	backgroundDone := make(chan struct{})
	go func() {
		backgroundWG.Wait()
		close(backgroundDone)
	}()
	select {
	case <-backgroundDone:
	case <-time.After(5 * time.Second):
		log.Printf("background startup tasks did not stop within 5 seconds")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := controlServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("shutdown web control server: %v", err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("shutdown web server: %v", err)
	}
}

func hasArgument(wanted string) bool {
	for _, argument := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(argument), wanted) {
			return true
		}
	}
	return false
}

func configureLogging(dataDir string, enabled devlogging.EnabledFunc) func() {
	file, err := devlogging.NewFileWriter(filepath.Join(dataDir, "logs", "manager.log"), enabled)
	if err != nil {
		return func() {}
	}
	log.SetOutput(file)
	return func() { _ = file.Close() }
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
