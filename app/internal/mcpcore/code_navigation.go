package mcpcore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxCodeNavigationResults = 500

type codeSymbolArgs struct {
	Path  string `json:"path,omitempty"`
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type codeLookupArgs struct {
	Symbol string `json:"symbol"`
	Path   string `json:"path,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type codeNavigationMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
	Preview string `json:"preview"`
}

func codeNavigationTools() []Tool {
	return []Tool{
		{
			Name:        "document_symbols",
			Title:       "Document Symbols",
			Description: "Return symbols declared in one source document. This is an alias-shaped code-navigation surface intended for clients that use LSP terminology.",
			InputSchema: codeSymbolSchema(true),
		},
		{
			Name:        "workspace_symbols",
			Title:       "Workspace Symbols",
			Description: "Search bounded source declarations across the active workspace or Agent Task worktree.",
			InputSchema: codeSymbolSchema(false),
		},
		{
			Name:        "find_definition",
			Title:       "Find Definition",
			Description: "Find likely source definitions for a symbol across supported source languages in the active workspace or Agent Task worktree.",
			InputSchema: codeLookupSchema(),
		},
		{
			Name:        "find_references",
			Title:       "Find References",
			Description: "Find bounded whole-word source references to a symbol in the active workspace or Agent Task worktree.",
			InputSchema: codeLookupSchema(),
		},
	}
}

func codeSymbolSchema(requirePath bool) map[string]any {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "default": "."},
			"query": map[string]any{"type": "string", "maxLength": 300},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxCodeNavigationResults, "default": 100},
		},
		"additionalProperties": false,
	}
	if requirePath {
		schema["required"] = []string{"path"}
	}
	return schema
}

func codeLookupSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"symbol": map[string]any{"type": "string", "minLength": 1, "maxLength": 300},
			"path":   map[string]any{"type": "string", "default": "."},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxCodeNavigationResults, "default": 100},
		},
		"required":             []string{"symbol"},
		"additionalProperties": false,
	}
}

func (s *Server) executeCodeNavigationTool(name string, arguments map[string]any) (map[string]any, error) {
	capabilities := detectedLanguageServers()
	base := map[string]any{
		"engine":                   "lexical-fallback",
		"availableLanguageServers": capabilities,
		"lspPersistentSession":     false,
	}
	switch name {
	case "list_symbols", "document_symbols":
		var args codeSymbolArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		matches, err := s.scanSourceSymbols(args.Path, args.Query, args.Limit, true)
		if err != nil {
			return nil, err
		}
		base["matches"] = matches
		base["count"] = len(matches)
		return base, nil
	case "workspace_symbols":
		var args codeSymbolArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		matches, err := s.scanSourceSymbols(args.Path, args.Query, args.Limit, false)
		if err != nil {
			return nil, err
		}
		base["matches"] = matches
		base["count"] = len(matches)
		return base, nil
	case "find_definition":
		var args codeLookupArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		matches, err := s.scanDefinitions(args)
		if err != nil {
			return nil, err
		}
		base["matches"] = matches
		base["count"] = len(matches)
		return base, nil
	case "find_references":
		var args codeLookupArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		matches, err := s.scanReferences(args)
		if err != nil {
			return nil, err
		}
		base["matches"] = matches
		base["count"] = len(matches)
		return base, nil
	default:
		return nil, fmt.Errorf("unknown code navigation tool: %s", name)
	}
}

var sourceSymbolPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{kind: "type", re: regexp.MustCompile(`^\s*(?:type|class|interface|struct|enum|trait)\s+([A-Za-z_][A-Za-z0-9_]*)`)},
	{kind: "function", re: regexp.MustCompile(`^\s*(?:func\s+(?:\([^)]*\)\s*)?|fn\s+|def\s+|function\s+)([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
	{kind: "constant", re: regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)},
	{kind: "method", re: regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|async|final|virtual|override|sealed|abstract)\s+)+(?:[A-Za-z_][\w<>,\[\]?\.]*\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
}

func supportedSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".rs", ".py", ".js", ".jsx", ".ts", ".tsx", ".cs", ".java", ".kt", ".kts", ".c", ".cc", ".cpp", ".h", ".hpp", ".swift", ".rb", ".php":
		return true
	default:
		return false
	}
}

func skipCodeNavigationDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".devdesk", ".mcp-devdesk", ".venv", "venv", "node_modules", "vendor", "dist", "build", "target", "__pycache__":
		return true
	default:
		return false
	}
}

func boundedCodeNavigationLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > maxCodeNavigationResults {
		return maxCodeNavigationResults
	}
	return limit
}

func symbolsInSourceFile(path, relative, query string, limit int) ([]codeNavigationMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]codeNavigationMatch, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		for _, pattern := range sourceSymbolPatterns {
			found := pattern.re.FindStringSubmatch(text)
			if len(found) < 2 {
				continue
			}
			name := found[1]
			if query != "" && !strings.Contains(strings.ToLower(name), query) {
				continue
			}
			matches = append(matches, codeNavigationMatch{Path: filepath.ToSlash(relative), Line: line, Kind: pattern.kind, Name: name, Preview: strings.TrimSpace(text)})
			break
		}
		if len(matches) >= limit {
			break
		}
	}
	return matches, scanner.Err()
}

func (s *Server) scanSourceSymbols(path, query string, limit int, requireFile bool) ([]codeNavigationMatch, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	_, target, relative, err := s.resolveWorkspacePath(path)
	if err != nil {
		return nil, err
	}
	limit = boundedCodeNavigationLimit(limit)
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		if !supportedSourcePath(target) {
			return nil, errors.New("path is not a supported source file")
		}
		return symbolsInSourceFile(target, relative, query, limit)
	}
	if requireFile {
		return nil, errors.New("path must reference a source file")
	}
	effectiveRoot, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	matches := make([]codeNavigationMatch, 0)
	err = filepath.WalkDir(target, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if current != target && skipCodeNavigationDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedSourcePath(current) {
			return nil
		}
		rel, relErr := filepath.Rel(effectiveRoot, current)
		if relErr != nil {
			return nil
		}
		remaining := limit - len(matches)
		if remaining <= 0 {
			return filepath.SkipAll
		}
		found, _ := symbolsInSourceFile(current, rel, query, remaining)
		matches = append(matches, found...)
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return matches, err
}

func (s *Server) scanDefinitions(args codeLookupArgs) ([]codeNavigationMatch, error) {
	if strings.TrimSpace(args.Symbol) == "" {
		return nil, errors.New("symbol is required")
	}
	all, err := s.scanSourceSymbols(args.Path, "", maxCodeNavigationResults, false)
	if err != nil {
		return nil, err
	}
	limit := boundedCodeNavigationLimit(args.Limit)
	matches := make([]codeNavigationMatch, 0)
	for _, item := range all {
		if item.Name == args.Symbol {
			matches = append(matches, item)
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches, nil
}

func (s *Server) scanReferences(args codeLookupArgs) ([]codeNavigationMatch, error) {
	if strings.TrimSpace(args.Symbol) == "" {
		return nil, errors.New("symbol is required")
	}
	path := args.Path
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	_, target, _, err := s.resolveWorkspacePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	limit := boundedCodeNavigationLimit(args.Limit)
	word := regexp.MustCompile(`\b` + regexp.QuoteMeta(args.Symbol) + `\b`)
	effectiveRoot, err := s.workspaceRoot()
	if err != nil {
		return nil, err
	}
	matches := make([]codeNavigationMatch, 0)
	visit := func(current string) error {
		file, openErr := os.Open(current)
		if openErr != nil {
			return nil
		}
		defer file.Close()
		rel, _ := filepath.Rel(effectiveRoot, current)
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if word.MatchString(scanner.Text()) {
				matches = append(matches, codeNavigationMatch{Path: filepath.ToSlash(rel), Line: line, Name: args.Symbol, Preview: strings.TrimSpace(scanner.Text())})
				if len(matches) >= limit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	}
	if info.Mode().IsRegular() {
		if !supportedSourcePath(target) {
			return nil, errors.New("path is not a supported source file")
		}
		_ = visit(target)
		return matches, nil
	}
	err = filepath.WalkDir(target, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if current != target && skipCodeNavigationDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !supportedSourcePath(current) {
			return nil
		}
		return visit(current)
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	return matches, err
}

func detectedLanguageServers() []string {
	candidates := []string{"gopls", "rust-analyzer", "typescript-language-server", "pyright-langserver", "pylsp", "csharp-ls", "OmniSharp"}
	available := make([]string, 0)
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			available = append(available, filepath.Base(path))
		}
	}
	sort.Strings(available)
	return available
}
