package mcpcore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	defaultToolOutputBytes = 128 * 1024
	maxToolOutputBytes     = 1024 * 1024
	maxReadableFileBytes   = 8 * 1024 * 1024
	maxSearchFileBytes     = 1024 * 1024
	maxSearchVisitedFiles  = 10000
)

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

type listDirArgs struct {
	Path          string `json:"path,omitempty"`
	IncludeHidden bool   `json:"includeHidden,omitempty"`
	MaxEntries    int    `json:"maxEntries,omitempty"`
}

type searchTextArgs struct {
	Query         string `json:"query"`
	Path          string `json:"path,omitempty"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
	IncludeHidden bool   `json:"includeHidden,omitempty"`
	MaxResults    int    `json:"maxResults,omitempty"`
}

type directoryEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
}

type textMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Preview string `json:"preview"`
}

func previewFileTools() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Title:       "Read Workspace File",
			Description: "Read a UTF-8 text file inside the configured workspace with optional line and output limits.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative or in-workspace absolute path."},
					"startLine": map[string]any{"type": "integer", "minimum": 1, "default": 1},
					"endLine":   map[string]any{"type": "integer", "minimum": 1},
					"maxBytes":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxToolOutputBytes, "default": defaultToolOutputBytes},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "list_dir",
			Title:       "List Workspace Directory",
			Description: "List files and directories without following symlinked directories outside the workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":          map[string]any{"type": "string", "default": "."},
					"includeHidden": map[string]any{"type": "boolean", "default": false},
					"maxEntries":    map[string]any{"type": "integer", "minimum": 1, "maximum": 5000, "default": 500},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "search_text",
			Title:       "Search Workspace Text",
			Description: "Search UTF-8 text files inside the workspace using a literal string query.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":         map[string]any{"type": "string", "minLength": 1},
					"path":          map[string]any{"type": "string", "default": "."},
					"caseSensitive": map[string]any{"type": "boolean", "default": false},
					"includeHidden": map[string]any{"type": "boolean", "default": false},
					"maxResults":    map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": 100},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) executeTool(name string, arguments map[string]any) (map[string]any, error) {
	switch name {
	case "server_info":
		return map[string]any{
			"name":            s.name,
			"version":         s.version,
			"protocolVersion": ProtocolVersion,
			"transport":       "streamable-http",
			"coreMode":        "go-preview",
			"workspace":       s.workspace,
			"toolCount":       len(s.tools),
			"uptimeSeconds":   s.uptimeSeconds(),
		}, nil
	case "get_workspace":
		root, err := s.workspaceRoot()
		if err != nil {
			return nil, err
		}
		return map[string]any{"workspace": root}, nil
	case "read_file":
		var args readFileArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.readFile(args)
	case "list_dir":
		var args listDirArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.listDir(args)
	case "search_text":
		var args searchTextArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.searchText(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func decodeToolArguments(arguments map[string]any, target any) error {
	raw, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("encode tool arguments: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

func (s *Server) readFile(args readFileArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, errors.New("path is required")
	}
	_, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path must reference a regular file")
	}
	if info.Size() > maxReadableFileBytes {
		return nil, fmt.Errorf("file is too large for preview read: %d bytes", info.Size())
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if !utf8.Valid(raw) {
		return nil, errors.New("file is not valid UTF-8 text")
	}

	startLine := args.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultToolOutputBytes
	}
	if maxBytes > maxToolOutputBytes {
		maxBytes = maxToolOutputBytes
	}
	text := normalizeText(string(raw))
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)
	if totalLines > 0 && startLine > totalLines {
		return nil, fmt.Errorf("startLine %d exceeds file length %d", startLine, totalLines)
	}
	endLine := args.EndLine
	if endLine <= 0 || endLine > totalLines {
		endLine = totalLines
	}
	if endLine < startLine && totalLines > 0 {
		return nil, errors.New("endLine must be greater than or equal to startLine")
	}

	selected := ""
	if totalLines > 0 && startLine <= totalLines {
		selected = strings.Join(lines[startLine-1:endLine], "\n")
	}
	truncated := false
	if len(selected) > maxBytes {
		selected = selected[:maxBytes]
		for !utf8.ValidString(selected) && len(selected) > 0 {
			selected = selected[:len(selected)-1]
		}
		truncated = true
	}
	return map[string]any{
		"path":       relative,
		"content":    selected,
		"startLine":  startLine,
		"endLine":    endLine,
		"totalLines": totalLines,
		"sizeBytes":  info.Size(),
		"truncated":  truncated,
	}, nil
}

func (s *Server) listDir(args listDirArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	_, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 500
	}
	if maxEntries > 5000 {
		maxEntries = 5000
	}
	visibleCount := visibleEntryCount(entries, args.IncludeHidden)
	result := make([]directoryEntry, 0, min(visibleCount, maxEntries))
	for _, entry := range entries {
		if !args.IncludeHidden && isHiddenName(entry.Name()) {
			continue
		}
		if len(result) >= maxEntries {
			break
		}
		item := directoryEntry{
			Name: entry.Name(),
			Path: joinRelative(relative, entry.Name()),
			Type: entryType(entry),
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			item.SizeBytes = info.Size()
			item.ModifiedAt = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		result = append(result, item)
	}
	return map[string]any{
		"path":      relative,
		"entries":   result,
		"count":     len(result),
		"truncated": len(result) < visibleCount,
	}, nil
}

func (s *Server) searchText(args searchTextArgs) (map[string]any, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	root, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	if maxResults > 1000 {
		maxResults = 1000
	}
	needle := query
	if !args.CaseSensitive {
		needle = strings.ToLower(query)
	}
	matches := make([]textMatch, 0, min(maxResults, 100))
	visited := 0
	truncated := false
	stopWalk := errors.New("search result limit reached")

	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path != target && entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 || shouldSkipSearchDirectory(entry.Name(), args.IncludeHidden) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !args.IncludeHidden && isHiddenName(entry.Name()) {
			return nil
		}
		if visited >= maxSearchVisitedFiles {
			truncated = true
			return stopWalk
		}
		visited++
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Size() > maxSearchFileBytes {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil || !utf8.Valid(raw) {
			return nil
		}
		for index, line := range strings.Split(normalizeText(string(raw)), "\n") {
			haystack := line
			if !args.CaseSensitive {
				haystack = strings.ToLower(line)
			}
			if !strings.Contains(haystack, needle) {
				continue
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			matches = append(matches, textMatch{
				Path:    filepath.ToSlash(rel),
				Line:    index + 1,
				Preview: trimPreview(line, 300),
			})
			if len(matches) >= maxResults {
				truncated = true
				return stopWalk
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, stopWalk) {
		return nil, fmt.Errorf("search workspace: %w", err)
	}
	return map[string]any{
		"query":         query,
		"path":          relative,
		"caseSensitive": args.CaseSensitive,
		"visitedFiles":  visited,
		"matches":       matches,
		"count":         len(matches),
		"truncated":     truncated,
	}, nil
}

func (s *Server) workspaceRoot() (string, error) {
	workspace := strings.TrimSpace(s.workspace)
	if workspace == "" {
		return "", errors.New("workspace is not configured")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("workspace unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	if evaluated, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = filepath.Clean(evaluated)
	}
	return abs, nil
}

func (s *Server) resolveWorkspacePath(value string) (root, target, relative string, err error) {
	root, err = s.workspaceRoot()
	if err != nil {
		return "", "", "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = "."
	}
	if filepath.IsAbs(value) {
		target = filepath.Clean(value)
	} else {
		target = filepath.Join(root, value)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve path: %w", err)
	}
	target = filepath.Clean(target)
	if evaluated, evalErr := filepath.EvalSymlinks(target); evalErr == nil {
		target = filepath.Clean(evaluated)
	}
	if !pathWithin(root, target) {
		return "", "", "", errors.New("path escapes the configured workspace")
	}
	relative, err = filepath.Rel(root, target)
	if err != nil {
		return "", "", "", fmt.Errorf("make workspace-relative path: %w", err)
	}
	if relative == "." {
		return root, target, ".", nil
	}
	return root, target, filepath.ToSlash(relative), nil
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func entryType(entry fs.DirEntry) string {
	if entry.Type()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if entry.IsDir() {
		return "directory"
	}
	return "file"
}

func joinRelative(parent, name string) string {
	if parent == "." || parent == "" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(parent, name))
}

func visibleEntryCount(entries []fs.DirEntry, includeHidden bool) int {
	count := 0
	for _, entry := range entries {
		if includeHidden || !isHiddenName(entry.Name()) {
			count++
		}
	}
	return count
}

func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func shouldSkipSearchDirectory(name string, includeHidden bool) bool {
	if !includeHidden && isHiddenName(name) {
		return true
	}
	switch strings.ToLower(name) {
	case ".git", ".gocache", ".gotmp", "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func trimPreview(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value + "…"
}

func normalizeText(value string) string {
	value = strings.TrimPrefix(value, "\ufeff")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}
