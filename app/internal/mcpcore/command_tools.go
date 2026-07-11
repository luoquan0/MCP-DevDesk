package mcpcore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxCommandOutputBytes = 4 * 1024 * 1024
	defaultCommandRead    = 128 * 1024
)

type commandManager struct {
	server   *Server
	mu       sync.RWMutex
	sessions map[string]*commandSession
}

type commandSession struct {
	mu        sync.RWMutex
	id        string
	command   string
	args      []string
	cwd       string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	output    *boundedOutput
	startedAt time.Time
	endedAt   time.Time
	running   bool
	exitCode  *int
	lastError string
	cancel    context.CancelFunc
}

type boundedOutput struct {
	mu         sync.RWMutex
	data       []byte
	baseOffset int64
	totalBytes int64
	maxBytes   int
}

type execCommandArgs struct {
	Command        string            `json:"command"`
	CommandLine    string            `json:"cmd,omitempty"`
	Args           []string          `json:"args,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	Workdir        string            `json:"workdir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	WaitMillis     int               `json:"waitMillis,omitempty"`
	YieldTimeMS    int               `json:"yield_time_ms,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	TTY            bool              `json:"tty,omitempty"`
}

type readOutputArgs struct {
	SessionID      string `json:"sessionId"`
	SessionIDSnake string `json:"session_id,omitempty"`
	Offset         int64  `json:"offset,omitempty"`
	MaxBytes       int    `json:"maxBytes,omitempty"`
	MaxBytesSnake  int    `json:"max_bytes,omitempty"`
}

type writeStdinArgs struct {
	SessionID      string `json:"sessionId"`
	SessionIDSnake string `json:"session_id,omitempty"`
	Chars          string `json:"chars"`
}

type killSessionArgs struct {
	SessionID      string `json:"sessionId"`
	SessionIDSnake string `json:"session_id,omitempty"`
	Signal         string `json:"signal,omitempty"`
	WaitMS         int    `json:"wait_ms,omitempty"`
}

func newCommandManager(server *Server) *commandManager {
	return &commandManager{server: server, sessions: make(map[string]*commandSession)}
}

func commandTools() []Tool {
	return []Tool{
		{
			Name:        "exec_command",
			Title:       "Execute Command",
			Description: "Start an executable with an argument array, or use the legacy cmd field for an explicit platform shell command line.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":        map[string]any{"type": "string", "minLength": 1},
					"cmd":            map[string]any{"type": "string", "minLength": 1, "description": "Legacy command-line form."},
					"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 256},
					"cwd":            map[string]any{"type": "string", "default": "."},
					"workdir":        map[string]any{"type": "string", "description": "Legacy alias for cwd."},
					"env":            map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"timeoutSeconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 86400, "default": 0},
					"timeout_ms":     map[string]any{"type": "integer", "minimum": 0, "maximum": 600000, "description": "Legacy timeout in milliseconds."},
					"waitMillis":     map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "default": 1000},
					"yield_time_ms":  map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "description": "Legacy alias for waitMillis."},
					"stdin":          map[string]any{"type": "string"},
					"tty":            map[string]any{"type": "boolean", "description": "Accepted for compatibility; PTY emulation is not enabled."},
				},
				"anyOf":                []any{map[string]any{"required": []string{"command"}}, map[string]any{"required": []string{"cmd"}}},
				"additionalProperties": false,
			},
		},
		{
			Name:        "read_output",
			Title:       "Read Command Output",
			Description: "Read bounded output from a running or completed command session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId":  map[string]any{"type": "string", "minLength": 1},
					"session_id": map[string]any{"type": "string", "minLength": 1, "description": "Legacy alias for sessionId."},
					"offset":     map[string]any{"type": "integer", "minimum": 0, "default": 0},
					"maxBytes":   map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576, "default": defaultCommandRead},
					"max_bytes":  map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576, "description": "Legacy alias for maxBytes."},
				},
				"anyOf":                []any{map[string]any{"required": []string{"sessionId"}}, map[string]any{"required": []string{"session_id"}}},
				"additionalProperties": false,
			},
		},
		{
			Name:        "write_stdin",
			Title:       "Write Command Input",
			Description: "Write UTF-8 text to the standard input of a running command session.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId":  map[string]any{"type": "string", "minLength": 1},
					"session_id": map[string]any{"type": "string", "minLength": 1, "description": "Legacy alias for sessionId."},
					"chars":      map[string]any{"type": "string"},
				},
				"required":             []string{"chars"},
				"anyOf":                []any{map[string]any{"required": []string{"sessionId"}}, map[string]any{"required": []string{"session_id"}}},
				"additionalProperties": false,
			},
		},
		{
			Name:        "kill_session",
			Title:       "Terminate Command Session",
			Description: "Terminate a running command and its child process tree.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sessionId":  map[string]any{"type": "string", "minLength": 1},
					"session_id": map[string]any{"type": "string", "minLength": 1, "description": "Legacy alias for sessionId."},
					"signal":     map[string]any{"type": "string", "enum": []string{"TERM", "KILL", "INT"}},
					"wait_ms":    map[string]any{"type": "integer", "minimum": 0, "maximum": 30000},
				},
				"anyOf":                []any{map[string]any{"required": []string{"sessionId"}}, map[string]any{"required": []string{"session_id"}}},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) executeCommandTool(name string, arguments map[string]any) (map[string]any, error) {
	if s.permissionMode == "safe" {
		return nil, errors.New("command execution is denied in safe permission mode")
	}
	switch name {
	case "exec_command":
		var args execCommandArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.commands.start(args)
	case "read_output":
		var args readOutputArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		if args.SessionID == "" {
			args.SessionID = args.SessionIDSnake
		}
		if args.MaxBytes <= 0 {
			args.MaxBytes = args.MaxBytesSnake
		}
		return s.commands.read(args)
	case "write_stdin":
		var args writeStdinArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		if args.SessionID == "" {
			args.SessionID = args.SessionIDSnake
		}
		return s.commands.write(args)
	case "kill_session":
		var args killSessionArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		if args.SessionID == "" {
			args.SessionID = args.SessionIDSnake
		}
		return s.commands.kill(args)
	default:
		return nil, fmt.Errorf("unknown command tool: %s", name)
	}
}

func (m *commandManager) start(args execCommandArgs) (map[string]any, error) {
	command := strings.TrimSpace(args.Command)
	displayCommand := command
	legacyLine := strings.TrimSpace(args.CommandLine)
	if command == "" {
		if legacyLine == "" {
			return nil, errors.New("command or cmd is required")
		}
		command, args.Args = shellCommand(legacyLine)
		displayCommand = legacyLine
	}
	if len(args.Args) > 256 {
		return nil, errors.New("too many command arguments")
	}
	networkCandidate := command
	if legacyLine != "" {
		if fields := strings.Fields(legacyLine); len(fields) > 0 {
			networkCandidate = fields[0]
		}
	}
	if !m.server.allowNetwork && knownNetworkCommand(networkCandidate) {
		return nil, errors.New("network command denied because allowNetwork is disabled")
	}
	cwdValue := strings.TrimSpace(args.CWD)
	if cwdValue == "" {
		cwdValue = strings.TrimSpace(args.Workdir)
	}
	if cwdValue == "" {
		cwdValue = "."
	}
	_, cwd, relativeCWD, err := m.server.resolveWorkspacePath(cwdValue)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, errors.New("command cwd must be an existing workspace directory")
	}
	if args.TimeoutSeconds < 0 || args.TimeoutSeconds > 86400 {
		return nil, errors.New("timeoutSeconds must be between 0 and 86400")
	}
	if args.TimeoutMS < 0 || args.TimeoutMS > 600000 {
		return nil, errors.New("timeout_ms must be between 0 and 600000")
	}
	if args.WaitMillis == 0 {
		args.WaitMillis = args.YieldTimeMS
	}
	if args.WaitMillis < 0 || args.WaitMillis > 30000 {
		return nil, errors.New("waitMillis must be between 0 and 30000")
	}
	if args.WaitMillis == 0 {
		args.WaitMillis = 1000
	}
	for key := range args.Env {
		if !validEnvironmentName(key) {
			return nil, fmt.Errorf("invalid environment variable name: %s", key)
		}
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if args.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(args.TimeoutMS)*time.Millisecond)
	} else if args.TimeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	cmd := exec.CommandContext(ctx, command, args.Args...)
	cmd.Dir = cwd
	cmd.Env = commandEnvironment(args.Env, m.server.allowNetwork)
	configureCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open command stdin: %w", err)
	}
	output := &boundedOutput{maxBytes: maxCommandOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("start command: %w", err)
	}
	if args.Stdin != "" {
		if _, err := io.WriteString(stdin, args.Stdin); err != nil {
			cancel()
			_ = terminateCommand(cmd)
			return nil, fmt.Errorf("write initial stdin: %w", err)
		}
	}
	sessionID, err := randomURLToken(18)
	if err != nil {
		cancel()
		_ = terminateCommand(cmd)
		return nil, err
	}
	session := &commandSession{
		id: sessionID, command: displayCommand, args: append([]string(nil), args.Args...), cwd: cwd,
		cmd: cmd, stdin: stdin, output: output, startedAt: time.Now(), running: true, cancel: cancel,
	}
	m.mu.Lock()
	m.cleanupLocked()
	m.sessions[sessionID] = session
	m.mu.Unlock()
	go session.wait()

	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(args.WaitMillis) * time.Millisecond):
		return session.snapshot(relativeCWD, 0, defaultCommandRead), nil
	case <-session.doneChannel():
	}
	return session.snapshot(relativeCWD, 0, defaultCommandRead), nil
}

func (s *commandSession) wait() {
	err := s.cmd.Wait()
	_ = s.stdin.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.endedAt = time.Now()
	if s.cmd.ProcessState != nil {
		code := s.cmd.ProcessState.ExitCode()
		s.exitCode = &code
	}
	if err != nil {
		s.lastError = err.Error()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *commandSession) doneChannel() <-chan struct{} {
	channel := make(chan struct{})
	go func() {
		for {
			s.mu.RLock()
			running := s.running
			s.mu.RUnlock()
			if !running {
				close(channel)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return channel
}

func (m *commandManager) read(args readOutputArgs) (map[string]any, error) {
	session, err := m.get(args.SessionID)
	if err != nil {
		return nil, err
	}
	if args.MaxBytes <= 0 {
		args.MaxBytes = defaultCommandRead
	}
	if args.MaxBytes > 1024*1024 {
		args.MaxBytes = 1024 * 1024
	}
	relativeCWD := session.cwd
	if root, rootErr := m.server.workspaceRoot(); rootErr == nil {
		if rel, relErr := filepath.Rel(root, session.cwd); relErr == nil {
			relativeCWD = filepath.ToSlash(rel)
		}
	}
	return session.snapshot(relativeCWD, args.Offset, args.MaxBytes), nil
}

func (m *commandManager) write(args writeStdinArgs) (map[string]any, error) {
	session, err := m.get(args.SessionID)
	if err != nil {
		return nil, err
	}
	session.mu.RLock()
	running := session.running
	stdin := session.stdin
	session.mu.RUnlock()
	if !running || stdin == nil {
		return nil, errors.New("command session is not running")
	}
	written, err := io.WriteString(stdin, args.Chars)
	if err != nil {
		return nil, fmt.Errorf("write command stdin: %w", err)
	}
	return map[string]any{"sessionId": args.SessionID, "bytesWritten": written}, nil
}

func (m *commandManager) kill(args killSessionArgs) (map[string]any, error) {
	session, err := m.get(args.SessionID)
	if err != nil {
		return nil, err
	}
	session.mu.RLock()
	running := session.running
	cmd := session.cmd
	cancel := session.cancel
	session.mu.RUnlock()
	if !running {
		return map[string]any{"sessionId": args.SessionID, "terminated": false, "message": "session already completed"}, nil
	}
	if cancel != nil {
		cancel()
	}
	if err := terminateCommand(cmd); err != nil {
		return nil, err
	}
	return map[string]any{"sessionId": args.SessionID, "terminated": true}, nil
}

func (m *commandManager) get(id string) (*commandSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("sessionId is required")
	}
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("command session not found")
	}
	return session, nil
}

func (m *commandManager) cleanupLocked() {
	cutoff := time.Now().Add(-time.Hour)
	for id, session := range m.sessions {
		session.mu.RLock()
		remove := !session.running && !session.endedAt.IsZero() && session.endedAt.Before(cutoff)
		session.mu.RUnlock()
		if remove {
			delete(m.sessions, id)
		}
	}
}

func (m *commandManager) close() {
	m.mu.RLock()
	sessions := make([]*commandSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()
	for _, session := range sessions {
		session.mu.RLock()
		running := session.running
		cmd := session.cmd
		cancel := session.cancel
		session.mu.RUnlock()
		if !running {
			continue
		}
		if cancel != nil {
			cancel()
		}
		_ = terminateCommand(cmd)
	}
}

func (s *commandSession) snapshot(relativeCWD string, offset int64, maxBytes int) map[string]any {
	data, nextOffset, truncated := s.output.read(offset, maxBytes)
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := map[string]any{
		"sessionId":   s.id,
		"session_id":  s.id,
		"command":     s.command,
		"args":        append([]string(nil), s.args...),
		"cwd":         relativeCWD,
		"running":     s.running,
		"output":      string(data),
		"nextOffset":  nextOffset,
		"next_offset": nextOffset,
		"truncated":   truncated,
		"startedAt":   s.startedAt.UTC().Format(time.RFC3339Nano),
	}
	if !s.endedAt.IsZero() {
		result["endedAt"] = s.endedAt.UTC().Format(time.RFC3339Nano)
	}
	if s.exitCode != nil {
		result["exitCode"] = *s.exitCode
		result["exit_code"] = *s.exitCode
	}
	if s.lastError != "" {
		result["lastError"] = s.lastError
		result["last_error"] = s.lastError
	}
	return result
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(data)
	b.totalBytes += int64(original)
	b.data = append(b.data, data...)
	if len(b.data) > b.maxBytes {
		drop := len(b.data) - b.maxBytes
		b.data = append([]byte(nil), b.data[drop:]...)
		b.baseOffset += int64(drop)
	}
	return original, nil
}

func (b *boundedOutput) read(offset int64, maxBytes int) ([]byte, int64, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	truncated := false
	if offset < b.baseOffset {
		offset = b.baseOffset
		truncated = true
	}
	if offset > b.totalBytes {
		offset = b.totalBytes
	}
	start := int(offset - b.baseOffset)
	end := start + maxBytes
	if end > len(b.data) {
		end = len(b.data)
	}
	result := append([]byte(nil), b.data[start:end]...)
	return result, offset + int64(len(result)), truncated || end < len(b.data)
}

func commandEnvironment(overrides map[string]string, allowNetwork bool) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		if index := strings.IndexByte(item, '='); index > 0 {
			key := strings.ToUpper(item[:index])
			if sensitiveEnvironmentName(key) {
				continue
			}
			values[key] = item[index+1:]
		}
	}
	if !allowNetwork {
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY"} {
			delete(values, key)
		}
		values["NO_PROXY"] = "*"
	}
	for key, value := range overrides {
		values[strings.ToUpper(key)] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func sensitiveEnvironmentName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	for _, fragment := range []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY", "PRIVATE_KEY", "CREDENTIAL", "AUTHORIZATION"} {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

func validEnvironmentName(value string) bool {
	if value == "" || strings.Contains(value, "=") {
		return false
	}
	for _, char := range value {
		if !(char >= 'A' && char <= 'Z') && !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '_' {
			return false
		}
	}
	return true
}

func knownNetworkCommand(command string) bool {
	name := strings.ToLower(filepath.Base(command))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	switch name {
	case "curl", "wget", "ssh", "scp", "sftp", "ftp", "telnet", "ping", "nslookup", "dig", "tracert", "traceroute":
		return true
	default:
		return false
	}
}

func commandRuntime() string { return runtime.GOOS }
