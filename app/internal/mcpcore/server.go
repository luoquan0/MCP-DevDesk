package mcpcore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	ProtocolVersion       = "2025-06-18"
	SessionHeader         = "Mcp-Session-Id"
	ProtocolVersionHeader = "MCP-Protocol-Version"
	defaultMaxBodyBytes   = int64(1 << 20)
)

type Options struct {
	Name         string
	Version      string
	Workspace    string
	MaxBodyBytes int64
}

type Server struct {
	name         string
	version      string
	workspace    string
	maxBodyBytes int64
	startedAt    time.Time

	mu       sync.RWMutex
	sessions map[string]session
	tools    []Tool
}

type session struct {
	CreatedAt  time.Time
	ClientName string
	ClientVer  string
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
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content           []contentItem `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

func New(options Options) *Server {
	if strings.TrimSpace(options.Name) == "" {
		options.Name = "mcp-devdesk-go-core"
	}
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "dev"
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = defaultMaxBodyBytes
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
			Description: "Return protocol, version, transport, workspace, and uptime details for the Go MCP preview core.",
			InputSchema: emptyObjectSchema,
		},
		{
			Name:        "get_workspace",
			Title:       "Get Workspace",
			Description: "Return the workspace currently assigned to the Go MCP preview core.",
			InputSchema: emptyObjectSchema,
		},
	}
	tools = append(tools, previewFileTools()...)
	return &Server{
		name:         options.Name,
		version:      options.Version,
		workspace:    options.Workspace,
		maxBodyBytes: options.MaxBodyBytes,
		startedAt:    time.Now(),
		sessions:     make(map[string]session),
		tools:        tools,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", s)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	case http.MethodGet:
		w.Header().Set("Allow", "POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": "SSE resumability is not enabled in the first Go core preview",
		})
	default:
		w.Header().Set("Allow", "POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
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
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		writeRPCError(w, http.StatusBadRequest, responseID(request.ID), -32600, "invalid request", nil)
		return
	}

	if request.Method == "initialize" {
		s.handleInitialize(w, request)
		return
	}

	sessionID, ok := s.validateSession(r)
	if !ok {
		writeRPCError(w, http.StatusBadRequest, responseID(request.ID), -32001, "missing or invalid MCP session", nil)
		return
	}
	if requestedVersion := strings.TrimSpace(r.Header.Get(ProtocolVersionHeader)); requestedVersion != "" && requestedVersion != ProtocolVersion {
		writeRPCError(w, http.StatusBadRequest, responseID(request.ID), -32602, "unsupported protocol version", map[string]any{"supported": ProtocolVersion})
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
		s.handleToolCall(w, request)
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
	if params.ProtocolVersion != ProtocolVersion {
		writeRPCError(w, http.StatusBadRequest, request.ID, -32602, "unsupported protocol version", map[string]any{"supported": ProtocolVersion})
		return
	}

	sessionID, err := newSessionID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, request.ID, -32603, "failed to create session", nil)
		return
	}
	s.mu.Lock()
	s.sessions[sessionID] = session{
		CreatedAt:  time.Now(),
		ClientName: params.ClientInfo.Name,
		ClientVer:  params.ClientInfo.Version,
	}
	s.mu.Unlock()

	w.Header().Set(SessionHeader, sessionID)
	writeRPCResult(w, request.ID, map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
		"instructions": "Go MCP preview core. The legacy core remains the default until compatibility testing is complete.",
	})
}

func (s *Server) handleToolCall(w http.ResponseWriter, request rpcRequest) {
	var params toolCallParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writeRPCError(w, http.StatusBadRequest, request.ID, -32602, "invalid tools/call params", err.Error())
		return
	}

	structured, err := s.executeTool(params.Name, params.Arguments)
	if err != nil {
		writeRPCResult(w, request.ID, toolCallResult{
			Content: []contentItem{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
		return
	}

	raw, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, request.ID, -32603, "failed to encode tool result", nil)
		return
	}
	writeRPCResult(w, request.ID, toolCallResult{
		Content:           []contentItem{{Type: "text", Text: string(raw)}},
		StructuredContent: structured,
	})
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
	s.mu.RLock()
	sessionCount := len(s.sessions)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"name":            s.name,
		"version":         s.version,
		"protocolVersion": ProtocolVersion,
		"workspace":       s.workspace,
		"sessions":        sessionCount,
	})
}

func (s *Server) validateSession(r *http.Request) (string, bool) {
	sessionID := strings.TrimSpace(r.Header.Get(SessionHeader))
	if sessionID == "" {
		return "", false
	}
	s.mu.RLock()
	_, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	return sessionID, ok
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
