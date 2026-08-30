package mcpcore

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ProtocolVersion            = "2025-06-18"
	LegacyProtocolVersion      = "2025-03-26"
	SessionHeader              = "Mcp-Session-Id"
	ProtocolVersionHeader      = "MCP-Protocol-Version"
	defaultMaxBodyBytes        = int64(1 << 20)
	defaultToolConcurrency     = 8
	maxMCPSessions             = 64
	maxSessionEvents           = 64
	maxSessionEventBytes       = 2 * 1024 * 1024
	mcpSessionTTL              = 12 * time.Hour
	maxManagedInstructionBytes = 96 * 1024
)

type Options struct {
	Name                    string
	Version                 string
	Workspace               string
	ManagedInstructions     string
	ManagedInstructionsFile string
	MaxBodyBytes            int64
	OAuth                   OAuthOptions
	AllowedOrigins          []string
	PermissionMode          string
	AllowNetwork            bool
	AuditPath               string
	LoggingConfig           string
	FileScope               string
	AllowedRoots            []string
	ToolProfile             string
	MaxConcurrentTools      int
}

type Server struct {
	name                    string
	version                 string
	workspace               string
	maxBodyBytes            int64
	startedAt               time.Time
	oauth                   *oauthServer
	allowedOrigins          map[string]struct{}
	permissionMode          string
	allowNetwork            bool
	toolProfile             string
	audit                   *auditLogger
	commands                *commandManager
	imageHTTPClient         *http.Client
	imageURLValidator       func(*url.URL) error
	fileScope               string
	allowedRoots            []string
	cwdMu                   sync.RWMutex
	defaultCWD              string
	projectRules            []projectRule
	managedInstructions     string
	managedInstructionsFile string

	mu        sync.RWMutex
	sessions  map[string]*session
	tools     []Tool
	toolSlots chan struct{}
}

type session struct {
	CreatedAt     time.Time
	LastSeen      time.Time
	ClientName    string
	ClientVer     string
	Protocol      string
	EventSequence uint64
	Events        []sseEvent
	EventBytes    int
}

type sseEvent struct {
	ID   string
	Data []byte
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type contentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type toolCallResult struct {
	Content           []contentItem `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

func New(options Options) (*Server, error) {
	if strings.TrimSpace(options.Name) == "" {
		options.Name = "mcp-devdesk-go-core"
	}
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "dev"
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = defaultMaxBodyBytes
	}
	if options.PermissionMode == "" {
		options.PermissionMode = "safe"
	}
	if options.PermissionMode != "safe" && options.PermissionMode != "trusted" && options.PermissionMode != "dangerous" {
		return nil, errors.New("permission mode must be safe, trusted, or dangerous")
	}
	if options.FileScope == "" {
		options.FileScope = "workspace"
	}
	if options.FileScope != "workspace" && options.FileScope != "roots" && options.FileScope != "computer" {
		return nil, errors.New("file scope must be workspace, roots, or computer")
	}
	if options.ToolProfile == "" {
		options.ToolProfile = "full"
	}
	if options.ToolProfile != "full" && options.ToolProfile != "read-only" && options.ToolProfile != "compat-readonly-all" {
		return nil, errors.New("tool profile must be full, read-only, or compat-readonly-all")
	}
	if options.MaxConcurrentTools <= 0 {
		options.MaxConcurrentTools = defaultToolConcurrency
	}
	if options.MaxConcurrentTools > 64 {
		return nil, errors.New("max concurrent tools cannot exceed 64")
	}

	emptyObjectSchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	tools := []Tool{
		{
			Name:        "server_info",
			Title:       "Go Core Server Info",
			Description: "Return protocol, version, transport, workspace, and uptime details for the Go MCP core.",
			InputSchema: emptyObjectSchema,
		},
		{
			Name:        "get_workspace",
			Title:       "Get Workspace",
			Description: "Return the workspace currently assigned to the Go MCP core.",
			InputSchema: emptyObjectSchema,
		},
		{
			Name:        "get_instructions",
			Title:       "Get Effective Instructions",
			Description: "Return the effective MCP DevDesk instructions currently applied to this connection, including workspace guidance and managed global/project prompts. Use this to verify which instructions are actually active.",
			InputSchema: emptyObjectSchema,
		},
	}
	tools = append(tools, previewFileTools()...)
	tools = append(tools, gitTools()...)
	tools = append(tools, permissionTools()...)
	compatibility := compatibilityTools()
	if options.ToolProfile == "read-only" {
		compatibility = filterTools(compatibility, func(tool Tool) bool {
			return tool.Name != "write_image" && tool.Name != "save_chatgpt_image"
		})
	} else {
		tools = append(tools, writeFileTools()...)
		tools = append(tools, commandTools()...)
	}
	tools = append(tools, compatibility...)
	oauth, err := newOAuthServer(options.OAuth)
	if err != nil {
		return nil, err
	}
	allowedOrigins := make(map[string]struct{})
	for _, origin := range options.AllowedOrigins {
		if normalized := normalizeOrigin(origin); normalized != "" {
			allowedOrigins[normalized] = struct{}{}
		}
	}
	if options.OAuth.Enabled {
		if parsed, parseErr := url.Parse(options.OAuth.Issuer); parseErr == nil {
			allowedOrigins[normalizeOrigin(parsed.Scheme+"://"+parsed.Host)] = struct{}{}
		}
	}
	server := &Server{
		name:                    options.Name,
		version:                 options.Version,
		workspace:               options.Workspace,
		maxBodyBytes:            options.MaxBodyBytes,
		startedAt:               time.Now(),
		oauth:                   oauth,
		allowedOrigins:          allowedOrigins,
		permissionMode:          options.PermissionMode,
		allowNetwork:            options.AllowNetwork,
		toolProfile:             options.ToolProfile,
		audit:                   newAuditLogger(options.AuditPath, options.LoggingConfig),
		fileScope:               options.FileScope,
		allowedRoots:            append([]string(nil), options.AllowedRoots...),
		defaultCWD:              options.Workspace,
		projectRules:            loadProjectRules(options.Workspace, maxProjectRuleBytes),
		managedInstructions:     strings.TrimSpace(options.ManagedInstructions),
		managedInstructionsFile: strings.TrimSpace(options.ManagedInstructionsFile),
		sessions:                make(map[string]*session),
		tools:                   tools,
		toolSlots:               make(chan struct{}, options.MaxConcurrentTools),
	}
	server.imageURLValidator = validateImageDownloadURL
	server.imageHTTPClient = newImageDownloadClient(server.imageURLValidator)
	server.commands = newCommandManager(server)
	return server, nil
}

func filterTools(tools []Tool, keep func(Tool) bool) []Tool {
	result := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if keep(tool) {
			result = append(result, tool)
		}
	}
	return result
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	var mcpHandler http.Handler = s
	if s.oauth != nil {
		s.oauth.registerRoutes(mux)
		mcpHandler = s.oauth.protect(mcpHandler)
	}
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/healthz", s.handleHealth)
	return s.securityHeaders(s.validateOrigin(mux))
}

func (s *Server) Close() {
	if s.commands != nil {
		s.commands.close()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	case http.MethodGet:
		s.handleGetSSE(w, r)
	case http.MethodOptions:
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	if acceptsOnlySSE(r.Header.Get("Accept")) {
		capture := newCaptureWriter()
		s.handlePostJSON(capture, r)
		if capture.status == http.StatusAccepted || capture.status == http.StatusNoContent || capture.body.Len() == 0 {
			copyCapturedResponse(w, capture)
			return
		}
		sessionID := capture.header.Get(SessionHeader)
		if sessionID == "" {
			sessionID = strings.TrimSpace(r.Header.Get(SessionHeader))
		}
		event := s.storeEvent(sessionID, capture.body.Bytes())
		for key, values := range capture.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(capture.status)
		writeSSEEvent(w, event)
		return
	}
	s.handlePostJSON(w, r)
}

func (s *Server) handlePostJSON(w http.ResponseWriter, r *http.Request) {
	if err := s.refreshManagedInstructions(); err != nil {
		writeRPCError(w, http.StatusInternalServerError, json.RawMessage("null"), -32603, "failed to refresh managed instructions", err.Error())
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type must be application/json"})
		return
	}

	reader := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var request rpcRequest
	if err := decoder.Decode(&request); err != nil {
		writeRPCError(w, http.StatusBadRequest, json.RawMessage("null"), -32700, "parse error", err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeRPCError(w, http.StatusBadRequest, json.RawMessage("null"), -32700, "parse error", "request body must contain one JSON object")
		return
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		writeRPCError(w, http.StatusBadRequest, responseID(request.ID), -32600, "invalid request", nil)
		return
	}

	if request.Method == "initialize" {
		s.handleInitialize(w, request)
		return
	}

	sessionID, negotiatedVersion, sessionStatus := s.validateSession(r)
	if sessionStatus != 0 {
		message := "missing MCP session"
		if sessionStatus == http.StatusNotFound {
			message = "unknown MCP session"
		}
		writeRPCError(w, sessionStatus, responseID(request.ID), -32001, message, nil)
		return
	}
	requestedVersion := strings.TrimSpace(r.Header.Get(ProtocolVersionHeader))
	if requestedVersion == "" {
		requestedVersion = negotiatedVersion
	}
	if !isSupportedProtocolVersion(requestedVersion) || requestedVersion != negotiatedVersion {
		writeRPCError(w, http.StatusBadRequest, responseID(request.ID), -32602, "unsupported protocol version", map[string]any{
			"supported":  []string{ProtocolVersion, LegacyProtocolVersion},
			"negotiated": negotiatedVersion,
			"requested":  requestedVersion,
		})
		return
	}

	if len(request.ID) == 0 {
		// Notifications do not receive JSON-RPC responses.
		w.Header().Set(SessionHeader, sessionID)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set(SessionHeader, sessionID)
	switch request.Method {
	case "ping":
		writeRPCResult(w, request.ID, map[string]any{})
	case "tools/list":
		writeRPCResult(w, request.ID, map[string]any{"tools": s.tools})
	case "tools/call":
		s.handleToolCall(w, r, request)
	default:
		writeRPCError(w, http.StatusOK, request.ID, -32601, "method not found", request.Method)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, request rpcRequest) {
	if len(request.ID) == 0 {
		writeRPCError(w, http.StatusBadRequest, json.RawMessage("null"), -32600, "initialize must be a request", nil)
		return
	}
	var params initializeParams
	if len(request.Params) == 0 {
		writeRPCError(w, http.StatusBadRequest, request.ID, -32602, "initialize params are required", nil)
		return
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writeRPCError(w, http.StatusBadRequest, request.ID, -32602, "invalid initialize params", err.Error())
		return
	}
	negotiatedVersion := negotiateProtocolVersion(params.ProtocolVersion)

	sessionID, err := newSessionID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, request.ID, -32603, "failed to create session", nil)
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.cleanupSessionsLocked(now)
	for len(s.sessions) >= maxMCPSessions {
		s.evictOldestSessionLocked()
	}
	s.sessions[sessionID] = &session{
		CreatedAt:  now,
		LastSeen:   now,
		ClientName: params.ClientInfo.Name,
		ClientVer:  params.ClientInfo.Version,
		Protocol:   negotiatedVersion,
		Events:     make([]sseEvent, 0, 16),
	}
	s.mu.Unlock()

	w.Header().Set(SessionHeader, sessionID)
	writeRPCResult(w, request.ID, map[string]any{
		"protocolVersion": negotiatedVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
		"instructions": s.initializeInstructions(),
	})
}

func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request, request rpcRequest) {
	var params toolCallParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writeRPCError(w, http.StatusBadRequest, request.ID, -32602, "invalid tools/call params", err.Error())
		return
	}
	select {
	case s.toolSlots <- struct{}{}:
		defer func() { <-s.toolSlots }()
	case <-r.Context().Done():
		writeRPCError(w, http.StatusRequestTimeout, request.ID, -32000, "request canceled before tool execution", nil)
		return
	default:
		writeRPCError(w, http.StatusServiceUnavailable, request.ID, -32000, "server is busy; retry the tool call", nil)
		return
	}

	started := time.Now()
	structured, err := s.executeTool(params.Name, params.Arguments)
	s.audit.log(params.Name, params.Arguments, started, err)
	if err != nil {
		writeRPCResult(w, request.ID, toolCallResult{
			Content: []contentItem{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
		return
	}

	imageData, _ := structured["_mcpImageData"].(string)
	imageMIME, _ := structured["_mcpImageMimeType"].(string)
	delete(structured, "_mcpImageData")
	delete(structured, "_mcpImageMimeType")
	raw, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, request.ID, -32603, "failed to encode tool result", nil)
		return
	}
	content := []contentItem{{Type: "text", Text: compactToolResultText(params.Name, structured, raw)}}
	if imageData != "" && imageMIME != "" {
		content = append(content, contentItem{Type: "image", Data: imageData, MimeType: imageMIME})
	}
	writeRPCResult(w, request.ID, toolCallResult{
		Content:           content,
		StructuredContent: structured,
	})
}

func (s *Server) initializeInstructions() string {
	base := "MCP DevDesk Go core. Use the exposed tools only within the configured workspace and permission policy."
	managedInstructions := s.currentManagedInstructions()
	if managedInstructions == "" && len(s.projectRules) == 0 {
		return base
	}
	var builder strings.Builder
	builder.WriteString(base)
	if len(s.projectRules) > 0 {
		builder.WriteString("\n\nProject instructions loaded from the workspace root:\n")
		for _, rule := range s.projectRules {
			builder.WriteString("\n--- ")
			builder.WriteString(rule.Path)
			if rule.Truncated {
				builder.WriteString(" (truncated)")
			}
			builder.WriteString(" ---\n")
			builder.WriteString(rule.Content)
			if !strings.HasSuffix(rule.Content, "\n") {
				builder.WriteByte('\n')
			}
		}
	}
	if managedInstructions != "" {
		builder.WriteString("\n\nMCP DevDesk managed project instructions (highest project-level priority; apply these when repository guidance conflicts):\n\n")
		builder.WriteString(managedInstructions)
		if !strings.HasSuffix(managedInstructions, "\n") {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func (s *Server) currentManagedInstructions() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.managedInstructions
}

func (s *Server) refreshManagedInstructions() error {
	path := strings.TrimSpace(s.managedInstructionsFile)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		raw = nil
	}
	if len(raw) > maxManagedInstructionBytes {
		return fmt.Errorf("instructions file cannot exceed %d bytes", maxManagedInstructionBytes)
	}
	if !utf8.Valid(raw) {
		return errors.New("instructions file must be valid UTF-8")
	}
	next := strings.TrimSpace(string(raw))
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == s.managedInstructions {
		return nil
	}
	s.managedInstructions = next
	// MCP has no standard notification for changed initialize instructions.
	// Drop existing sessions so the next client request is forced through a
	// fresh initialize handshake and receives the updated prompt immediately.
	s.sessions = make(map[string]*session)
	return nil
}

func compactToolResultText(name string, structured map[string]any, full []byte) string {
	const maxInlineToolTextBytes = 32 * 1024
	if len(full) <= maxInlineToolTextBytes {
		return string(full)
	}
	keys := []string{
		"path", "workspace", "query", "count", "returnedCount", "totalCount",
		"succeeded", "failed", "truncated", "nextOffset", "nextSkip",
		"returnedBytes", "omittedBytes", "visitedFiles", "clean", "revision",
	}
	summary := map[string]any{
		"tool":    name,
		"message": "The complete result is available in structuredContent; text output was compacted to protect the context window.",
	}
	for _, key := range keys {
		if value, ok := structured[key]; ok {
			summary[key] = value
		}
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Sprintf("%s returned a large structured result (%d bytes).", name, len(full))
	}
	return string(encoded)
}

func (s *Server) uptimeSeconds() int64 {
	return int64(time.Since(s.startedAt).Seconds())
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.Header.Get(SessionHeader))
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing MCP session header"})
		return
	}
	s.mu.Lock()
	_, exists := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "MCP session not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.cleanupSessionsLocked(time.Now())
	sessionCount := len(s.sessions)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"name":            s.name,
		"version":         s.version,
		"protocolVersion": ProtocolVersion,
		"workspace":       s.workspace,
		"sessions":        sessionCount,
		"activeToolCalls": len(s.toolSlots),
		"maxToolCalls":    cap(s.toolSlots),
	})
}

func (s *Server) validateSession(r *http.Request) (string, string, int) {
	sessionID := strings.TrimSpace(r.Header.Get(SessionHeader))
	if sessionID == "" {
		return "", "", http.StatusBadRequest
	}
	now := time.Now()
	s.mu.Lock()
	s.cleanupSessionsLocked(now)
	current, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return sessionID, "", http.StatusNotFound
	}
	current.LastSeen = now
	protocol := current.Protocol
	s.mu.Unlock()
	return sessionID, protocol, 0
}

func (s *Server) handleGetSSE(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
		w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
		writeJSON(w, http.StatusNotAcceptable, map[string]any{"error": "Accept must include text/event-stream"})
		return
	}
	sessionID, negotiatedVersion, sessionStatus := s.validateSession(r)
	if sessionStatus != 0 {
		writeJSON(w, sessionStatus, map[string]any{"error": "MCP session not found"})
		return
	}
	requestedVersion := strings.TrimSpace(r.Header.Get(ProtocolVersionHeader))
	if requestedVersion == "" {
		requestedVersion = negotiatedVersion
	}
	if !isSupportedProtocolVersion(requestedVersion) || requestedVersion != negotiatedVersion {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported protocol version"})
		return
	}
	events := s.eventsAfter(sessionID, strings.TrimSpace(r.Header.Get("Last-Event-ID")))
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set(SessionHeader, sessionID)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	for _, event := range events {
		writeSSEEvent(w, event)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "wait=0") {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keepalive "+strconv.FormatInt(time.Now().Unix(), 10)+"\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func (s *Server) storeEvent(sessionID string, data []byte) sseEvent {
	if sessionID == "" {
		return sseEvent{Data: append([]byte(nil), data...)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[sessionID]
	if !ok {
		return sseEvent{Data: append([]byte(nil), data...)}
	}
	current.LastSeen = time.Now()
	if len(data) > maxSessionEventBytes {
		return sseEvent{Data: append([]byte(nil), data...)}
	}
	current.EventSequence++
	event := sseEvent{
		ID:   sessionID + ":" + strconv.FormatUint(current.EventSequence, 10),
		Data: append([]byte(nil), data...),
	}
	current.Events = append(current.Events, event)
	current.EventBytes += len(event.Data)
	for len(current.Events) > maxSessionEvents || current.EventBytes > maxSessionEventBytes {
		current.EventBytes -= len(current.Events[0].Data)
		current.Events[0] = sseEvent{}
		current.Events = current.Events[1:]
	}
	if cap(current.Events) > maxSessionEvents*2 {
		current.Events = append([]sseEvent(nil), current.Events...)
	}
	return event
}

func (s *Server) eventsAfter(sessionID, lastEventID string) []sseEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, ok := s.sessions[sessionID]
	if !ok {
		return nil
	}
	if lastEventID == "" {
		return append([]sseEvent(nil), current.Events...)
	}
	result := make([]sseEvent, 0)
	found := false
	for _, event := range current.Events {
		if found {
			result = append(result, event)
			continue
		}
		if event.ID == lastEventID {
			found = true
		}
	}
	if !found {
		return append([]sseEvent(nil), current.Events...)
	}
	return result
}

func (s *Server) cleanupSessionsLocked(now time.Time) {
	for id, current := range s.sessions {
		lastSeen := current.LastSeen
		if lastSeen.IsZero() {
			lastSeen = current.CreatedAt
		}
		if now.Sub(lastSeen) > mcpSessionTTL {
			delete(s.sessions, id)
		}
	}
}

func (s *Server) evictOldestSessionLocked() {
	oldestID := ""
	var oldest time.Time
	for id, current := range s.sessions {
		lastSeen := current.LastSeen
		if lastSeen.IsZero() {
			lastSeen = current.CreatedAt
		}
		if oldestID == "" || lastSeen.Before(oldest) {
			oldestID = id
			oldest = lastSeen
		}
	}
	if oldestID != "" {
		delete(s.sessions, oldestID)
	}
}

func (s *Server) validateOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
		if rawOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Some OAuth popup / embedded-browser environments submit the owner
		// password form from an opaque browser origin and therefore send
		// `Origin: null`. Keep the exception narrowly scoped to the built-in
		// authorization form POST. MCP, token, registration, and every other
		// endpoint continue to reject opaque origins.
		if strings.EqualFold(rawOrigin, "null") && isOpaqueOAuthAuthorizationForm(r) {
			next.ServeHTTP(w, r)
			return
		}
		origin := normalizeOrigin(rawOrigin)
		if origin == "" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin is invalid"})
			return
		}
		parsed, err := url.Parse(origin)
		if err == nil && isLoopbackHost(parsed.Hostname()) {
			next.ServeHTTP(w, r)
			return
		}
		if _, allowed := s.allowedOrigins[origin]; allowed {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin is not allowed"})
	})
}

func isOpaqueOAuthAuthorizationForm(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/oauth/authorize" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/x-www-form-urlencoded")
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

func normalizeOrigin(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func negotiateProtocolVersion(requested string) string {
	requested = strings.TrimSpace(requested)
	if isSupportedProtocolVersion(requested) {
		return requested
	}
	return ProtocolVersion
}

func isSupportedProtocolVersion(version string) bool {
	return version == ProtocolVersion || version == LegacyProtocolVersion
}

func acceptsOnlySSE(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "text/event-stream") && !strings.Contains(value, "application/json")
}

func writeSSEEvent(w io.Writer, event sseEvent) {
	if event.ID != "" {
		_, _ = io.WriteString(w, "id: "+event.ID+"\n")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(event.Data)), "\n") {
		_, _ = io.WriteString(w, "data: "+line+"\n")
	}
	_, _ = io.WriteString(w, "\n")
}

type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{header: make(http.Header), status: http.StatusOK}
}

func (w *captureWriter) Header() http.Header            { return w.header }
func (w *captureWriter) WriteHeader(status int)         { w.status = status }
func (w *captureWriter) Write(data []byte) (int, error) { return w.body.Write(data) }

func copyCapturedResponse(w http.ResponseWriter, capture *captureWriter) {
	for key, values := range capture.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(capture.status)
	_, _ = w.Write(capture.body.Bytes())
}

func newSessionID() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func responseID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: responseID(id), Result: result})
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string, data any) {
	writeJSON(w, status, rpcResponse{
		JSONRPC: "2.0",
		ID:      responseID(id),
		Error:   &rpcError{Code: code, Message: message, Data: data},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var errServerClosed = errors.New("server closed")

// NormalizeServeError converts http.ErrServerClosed into a stable nil result
// for command entry points and tests.
func NormalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, errServerClosed) {
		return nil
	}
	return err
}
