package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	baseURL := strings.TrimRight(os.Getenv("MCP_DEVDESK_ADMIN_URL"), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:17860"
	}

	command := "status"
	if len(os.Args) > 1 {
		command = strings.ToLower(os.Args[1])
	}

	method, path, body, err := commandRequest(command)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: 35 * time.Second}
	request, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		fatal(err)
	}
	if response.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "HTTP %d: %s\n", response.StatusCode, strings.TrimSpace(string(raw)))
		os.Exit(1)
	}

	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		pretty, _ := json.MarshalIndent(decoded, "", "  ")
		fmt.Println(string(pretty))
		return
	}
	fmt.Print(string(raw))
}

func commandRequest(command string) (method, path string, body io.Reader, err error) {
	switch command {
	case "status":
		return http.MethodGet, "/api/status", nil, nil
	case "health":
		return http.MethodGet, "/api/health", nil, nil
	case "config":
		return http.MethodGet, "/api/config", nil, nil
	case "diagnostics", "diag":
		return http.MethodGet, "/api/diagnostics", nil, nil
	case "desktop":
		return http.MethodGet, "/api/system/desktop", nil, nil
	case "tunnels":
		return http.MethodGet, "/api/tunnels/processes", nil, nil
	case "sync-tunnel":
		return http.MethodPost, "/api/tunnels/sync-port", bytes.NewReader([]byte("{}")), nil
	case "open":
		return http.MethodPost, "/api/ui/open", bytes.NewReader([]byte("{}")), nil
	case "startup-on":
		return http.MethodPut, "/api/system/startup", bytes.NewReader([]byte{123, 34, 101, 110, 97, 98, 108, 101, 100, 34, 58, 116, 114, 117, 101, 125}), nil
	case "startup-off":
		return http.MethodPut, "/api/system/startup", bytes.NewReader([]byte{123, 34, 101, 110, 97, 98, 108, 101, 100, 34, 58, 102, 97, 108, 115, 101, 125}), nil
	case "start", "stop", "restart", "takeover":
		return http.MethodPost, "/api/services/" + command, bytes.NewReader([]byte("{}")), nil
	default:
		return "", "", nil, errors.New("usage: devdeskctl [status|health|config|diagnostics|desktop|tunnels|sync-tunnel|open|startup-on|startup-off|start|stop|restart|takeover]")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
