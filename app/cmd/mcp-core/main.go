package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"mcp-devdesk/internal/buildinfo"
	"mcp-devdesk/internal/mcpcore"
)

func main() {
	workspace := flag.String("workspace", ".", "workspace exposed by the Go MCP preview core")
	host := flag.String("host", "127.0.0.1", "listen host")
	port := flag.Int("port", 18765, "listen port")
	flag.Parse()

	resolvedWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatal(err)
	}
	info, err := os.Stat(resolvedWorkspace)
	if err != nil || !info.IsDir() {
		log.Fatalf("workspace is not an existing directory: %s", resolvedWorkspace)
	}
	if *port < 1024 || *port > 65535 {
		log.Fatalf("port must be between 1024 and 65535")
	}
	if ip := net.ParseIP(*host); ip == nil || !ip.IsLoopback() {
		log.Fatalf("preview core host must be a loopback IP")
	}

	core := mcpcore.New(mcpcore.Options{
		Name:      "mcp-devdesk-go-core",
		Version:   buildinfo.Version,
		Workspace: resolvedWorkspace,
	})
	address := net.JoinHostPort(*host, strconv.Itoa(*port))
	server := &http.Server{
		Addr:              address,
		Handler:           core.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Go MCP preview core %s listening on http://%s/mcp", buildinfo.Version, address)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case signalValue := <-signals:
		log.Printf("received %s, shutting down", signalValue)
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			log.Fatal(serveErr)
		}
		return
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		if closeErr := server.Close(); closeErr != nil {
			log.Printf("forced close failed: %v", closeErr)
		}
	}
	if err := mcpcore.NormalizeServeError(<-serverErrors); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
