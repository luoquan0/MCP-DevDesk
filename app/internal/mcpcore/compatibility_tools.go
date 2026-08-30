package mcpcore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	maxImageBytes           = 20 * 1024 * 1024
	maxDownloadedImageBytes = 50 * 1024 * 1024
)

type setDefaultCWDArgs struct {
	Path string `json:"path"`
}

type listFilesArgs struct {
	Path                 string   `json:"path,omitempty"`
	Glob                 string   `json:"glob,omitempty"`
	Patterns             []string `json:"patterns,omitempty"`
	ExcludePatterns      []string `json:"excludePatterns,omitempty"`
	ExcludePatternsSnake []string `json:"exclude_patterns,omitempty"`
	IncludeHidden        bool     `json:"includeHidden,omitempty"`
	IncludeHiddenSnake   bool     `json:"include_hidden,omitempty"`
	IncludeIgnored       bool     `json:"includeIgnored,omitempty"`
	IncludeIgnoredSnake  bool     `json:"include_ignored,omitempty"`
	MaxResults           int      `json:"maxResults,omitempty"`
	MaxResultsSnake      int      `json:"max_results,omitempty"`
	Offset               int      `json:"offset,omitempty"`
	Sort                 string   `json:"sort,omitempty"`
}

type listedFile struct {
	Path       string `json:"path"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

type gitBlameArgs struct {
	Path      string `json:"path"`
	Revision  string `json:"revision,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

type writeImageArgs struct {
	Path          string `json:"path"`
	Data          string `json:"data,omitempty"`
	DataURL       string `json:"dataUrl,omitempty"`
	MIMEType      string `json:"mimeType,omitempty"`
	Overwrite     bool   `json:"overwrite,omitempty"`
	CreateParents bool   `json:"createParents,omitempty"`
}

type saveChatGPTImageArgs struct {
	Path          string           `json:"path"`
	SourceImage   *openAIFileInput `json:"source_image"`
	Overwrite     bool             `json:"overwrite,omitempty"`
	CreateParents bool             `json:"create_parents,omitempty"`
	MaxBytes      int64            `json:"max_bytes,omitempty"`
}

type openAIFileInput struct {
	DownloadURL string `json:"download_url"`
	FileID      string `json:"file_id"`
	MIMEType    string `json:"mime_type,omitempty"`
	FileName    string `json:"file_name,omitempty"`
}

type viewImageArgs struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"maxBytes,omitempty"`
}

func compatibilityTools() []Tool {
	empty := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	return []Tool{
		{
			Name:        "check_exec_environment",
			Title:       "Check Execution Environment",
			Description: "Return runtime, workspace, file-scope, permission, networking, and command-session limits.",
			InputSchema: empty,
		},
		{
			Name:        "get_default_cwd",
			Title:       "Get Default Directory",
			Description: "Return the directory used to resolve relative tool paths.",
			InputSchema: empty,
		},
		{
			Name:        "set_default_cwd",
			Title:       "Set Default Directory",
			Description: "Set the directory used to resolve relative paths, subject to the configured file scope.",
			InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "minLength": 1}},
				"required": []string{"path"}, "additionalProperties": false,
			},
		},
		{
			Name:        "list_files",
			Title:       "List Files Recursively",
			Description: "Recursively list files with optional glob and exclusion filters without following symbolic-link directories.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":             map[string]any{"type": "string", "default": "."},
					"glob":             map[string]any{"type": "string"},
					"patterns":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 50},
					"excludePatterns":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 50},
					"exclude_patterns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 50, "description": "Legacy alias for excludePatterns."},
					"includeHidden":    map[string]any{"type": "boolean", "default": false},
					"include_hidden":   map[string]any{"type": "boolean", "description": "Legacy alias for includeHidden."},
					"includeIgnored":   map[string]any{"type": "boolean", "default": false},
					"include_ignored":  map[string]any{"type": "boolean", "description": "Legacy alias for includeIgnored."},
					"maxResults":       map[string]any{"type": "integer", "minimum": 1, "maximum": 50000, "default": 5000},
					"max_results":      map[string]any{"type": "integer", "minimum": 1, "maximum": 50000, "description": "Legacy alias for maxResults."},
					"offset":           map[string]any{"type": "integer", "minimum": 0, "maximum": 50000, "default": 0},
					"sort":             map[string]any{"type": "string", "enum": []string{"path", "modified"}, "default": "path"},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "git_blame",
			Title:       "Git Blame",
			Description: "Return bounded line-level Git blame porcelain output for a workspace file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "minLength": 1},
					"revision":  map[string]any{"type": "string", "default": "HEAD"},
					"startLine": map[string]any{"type": "integer", "minimum": 1, "default": 1},
					"endLine":   map[string]any{"type": "integer", "minimum": 1},
					"maxBytes":  map[string]any{"type": "integer", "minimum": 1, "maximum": maxGitOutputBytes, "default": 262144},
				},
				"required": []string{"path"}, "additionalProperties": false,
			},
		},
		imageWriteTool("write_image", "Write Image", "Decode and atomically write a PNG, JPEG, GIF, or WebP image inside the configured file scope."),
		chatGPTImageSaveTool(),
		{
			Name:        "view_image",
			Title:       "View Image",
			Description: "Return an image as MCP image content with basic metadata.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":     map[string]any{"type": "string", "minLength": 1},
					"maxBytes": map[string]any{"type": "integer", "minimum": 1024, "maximum": maxImageBytes, "default": maxImageBytes},
				},
				"required": []string{"path"}, "additionalProperties": false,
			},
		},
	}
}

func imageWriteTool(name, title, description string) Tool {
	return Tool{
		Name: name, Title: title, Description: description,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":          map[string]any{"type": "string", "minLength": 1},
				"data":          map[string]any{"type": "string", "description": "Raw base64 image data."},
				"dataUrl":       map[string]any{"type": "string", "description": "A data:image/...;base64 URL."},
				"mimeType":      map[string]any{"type": "string"},
				"overwrite":     map[string]any{"type": "boolean", "default": false},
				"createParents": map[string]any{"type": "boolean", "default": false},
			},
			"required": []string{"path"}, "additionalProperties": false,
		},
	}
}

func chatGPTImageSaveTool() Tool {
	return Tool{
		Name:        "save_chatgpt_image",
		Title:       "Save ChatGPT Image",
		Description: "Download an image supplied through the source_image ChatGPT file parameter and atomically save the original bytes inside the configured file scope. Do not convert ChatGPT files to base64; use write_image only when complete base64 data already exists.",
		InputSchema: map[string]any{
			"type": "object",
			"$defs": map[string]any{
				"OpenAIFile": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"download_url": map[string]any{"type": "string"},
						"file_id":      map[string]any{"type": "string"},
						"mime_type":    map[string]any{"type": "string"},
						"file_name":    map[string]any{"type": "string"},
					},
					"required":             []string{"download_url", "file_id"},
					"additionalProperties": false,
				},
			},
			"properties": map[string]any{
				"path":           map[string]any{"type": "string", "minLength": 1},
				"source_image":   map[string]any{"$ref": "#/$defs/OpenAIFile", "description": "The generated or attached ChatGPT image file."},
				"overwrite":      map[string]any{"type": "boolean", "default": false},
				"create_parents": map[string]any{"type": "boolean", "default": false},
				"max_bytes":      map[string]any{"type": "integer", "minimum": 1024, "maximum": maxDownloadedImageBytes, "default": maxDownloadedImageBytes},
			},
			"required":             []string{"path", "source_image"},
			"additionalProperties": false,
		},
		Meta: map[string]any{
			"openai/fileParams":              []string{"source_image"},
			"openai/toolInvocation/invoking": "Saving image to the local workspace",
			"openai/toolInvocation/invoked":  "Image saved to the local workspace",
		},
	}
}

func (s *Server) executeCompatibilityTool(name string, arguments map[string]any) (map[string]any, error) {
	switch name {
	case "check_exec_environment":
		return map[string]any{
			"runtime":             runtime.GOOS + "/" + runtime.GOARCH,
			"workspace":           s.workspace,
			"defaultCwd":          s.currentDefaultCWD(),
			"permissionMode":      s.permissionMode,
			"toolProfile":         s.toolProfile,
			"fileScope":           s.fileScope,
			"allowedRoots":        append([]string(nil), s.allowedRoots...),
			"allowNetwork":        s.allowNetwork,
			"implicitShell":       false,
			"maxCommandOutput":    maxCommandOutputBytes,
			"maxReadableFile":     maxReadableFileBytes,
			"maxWritableFile":     maxWritableFileBytes,
			"maxBatchReadFiles":   maxBatchReadFiles,
			"maxBatchReadBytes":   maxBatchReadTotalBytes,
			"projectInstructions": projectRuleMetadata(s.currentProjectRules()),
			"oauthEnabled":        s.oauth != nil,
			"streamableHTTP":      true,
			"sseReplaySupported":  true,
		}, nil
	case "get_default_cwd":
		cwd := s.currentDefaultCWD()
		return map[string]any{"defaultCwd": cwd, "default_cwd": cwd}, nil
	case "set_default_cwd":
		var args setDefaultCWDArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.setDefaultCWD(args.Path)
	case "list_files":
		var args listFilesArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.listFiles(args)
	case "git_blame":
		var args gitBlameArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.gitBlame(args)
	case "write_image":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args writeImageArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.writeInlineImage(args)
	case "save_chatgpt_image":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args saveChatGPTImageArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.saveChatGPTImage(args)
	case "view_image":
		var args viewImageArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.viewImage(args)
	default:
		return nil, fmt.Errorf("unknown compatibility tool: %s", name)
	}
}

func (s *Server) currentDefaultCWD() string {
	s.cwdMu.RLock()
	value := s.defaultCWD
	s.cwdMu.RUnlock()
	if strings.TrimSpace(value) == "" {
		return s.workspace
	}
	return value
}

func (s *Server) setDefaultCWD(value string) (map[string]any, error) {
	_, target, display, err := s.resolveWorkspacePath(value)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return nil, errors.New("default cwd must be an existing directory")
	}
	s.cwdMu.Lock()
	s.defaultCWD = target
	s.cwdMu.Unlock()
	return map[string]any{"defaultCwd": target, "default_cwd": target, "displayPath": display, "display_path": display}, nil
}

func (s *Server) listFiles(args listFilesArgs) (map[string]any, error) {
	args.ExcludePatterns = append(args.ExcludePatterns, args.ExcludePatternsSnake...)
	args.IncludeHidden = args.IncludeHidden || args.IncludeHiddenSnake
	args.IncludeIgnored = args.IncludeIgnored || args.IncludeIgnoredSnake
	if args.MaxResults <= 0 {
		args.MaxResults = args.MaxResultsSnake
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	root, target, displayRoot, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 5000
	}
	if maxResults > 50000 {
		maxResults = 50000
	}
	if args.Offset < 0 || args.Offset > 50000 {
		return nil, errors.New("offset must be between 0 and 50000")
	}
	patterns := append([]string(nil), args.Patterns...)
	if strings.TrimSpace(args.Glob) != "" {
		patterns = append(patterns, args.Glob)
	}
	collectionLimit := 50000
	if args.Sort != "modified" {
		collectionLimit = min(50000, args.Offset+maxResults+1)
	}
	files := make([]listedFile, 0, collectionLimit)
	visited := 0
	collectionTruncated := false
	stop := errors.New("file collection limit reached")
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path != target && entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 || (!args.IncludeHidden && isHiddenName(entry.Name())) || (!args.IncludeIgnored && shouldSkipSearchDirectory(entry.Name(), args.IncludeHidden)) {
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
		visited++
		rel, relErr := filepath.Rel(target, path)
		if relErr != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if !matchesAnyPattern(slashRel, entry.Name(), patterns, true) || matchesAnyPattern(slashRel, entry.Name(), args.ExcludePatterns, false) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		display := slashRel
		if displayRoot != "." {
			if filepath.IsAbs(displayRoot) {
				display = filepath.ToSlash(filepath.Join(displayRoot, rel))
			} else {
				display = filepath.ToSlash(filepath.Join(displayRoot, rel))
			}
		} else if !sameFilesystemPath(root, target) {
			display = filepath.ToSlash(path)
		}
		files = append(files, listedFile{Path: display, SizeBytes: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339)})
		if len(files) >= collectionLimit {
			collectionTruncated = true
			return stop
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, fmt.Errorf("list files: %w", err)
	}
	if args.Sort == "modified" {
		sort.Slice(files, func(i, j int) bool { return files[i].ModifiedAt > files[j].ModifiedAt })
	} else {
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}
	totalCollected := len(files)
	start := min(args.Offset, totalCollected)
	end := min(start+maxResults, totalCollected)
	page := append([]listedFile(nil), files[start:end]...)
	hasMore := end < totalCollected || collectionTruncated
	result := map[string]any{
		"path":            displayRoot,
		"files":           page,
		"count":           len(page),
		"returnedCount":   len(page),
		"offset":          args.Offset,
		"resultLimit":     maxResults,
		"visitedFiles":    visited,
		"totalCollected":  totalCollected,
		"collectionLimit": collectionLimit,
		"truncated":       hasMore,
	}
	if hasMore && len(page) > 0 {
		result["nextOffset"] = end
	}
	return result, nil
}

func matchesAnyPattern(path, name string, patterns []string, emptyMatches bool) bool {
	if len(patterns) == 0 {
		return emptyMatches
	}
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		matchedPath, _ := pathpkg.Match(pattern, path)
		matchedName, _ := pathpkg.Match(pattern, name)
		if matchedPath || matchedName || (strings.HasPrefix(pattern, "**/") && strings.HasSuffix(path, strings.TrimPrefix(pattern, "**/"))) {
			return true
		}
	}
	return false
}

func (s *Server) gitBlame(args gitBlameArgs) (map[string]any, error) {
	_, target, display, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("git blame path must be a regular file")
	}
	revision := strings.TrimSpace(args.Revision)
	if revision == "" {
		revision = "HEAD"
	}
	if strings.HasPrefix(revision, "-") {
		return nil, errors.New("revision must not begin with a dash")
	}
	start := args.StartLine
	if start <= 0 {
		start = 1
	}
	end := args.EndLine
	if end <= 0 {
		end = start + 199
	}
	if end < start || end-start > 999 {
		return nil, errors.New("blame line range must contain at most 1000 lines")
	}
	limit := boundedGitLimit(args.MaxBytes)
	output, truncated, err := runGit(filepath.Dir(target), limit,
		"blame", "--line-porcelain", "-L", fmt.Sprintf("%d,%d", start, end), revision, "--", filepath.Base(target))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path": display, "revision": revision, "startLine": start, "endLine": end, "porcelain": output,
		"returnedBytes": len(output), "outputLimitBytes": limit, "truncated": truncated,
	}, nil
}

func (s *Server) writeInlineImage(args writeImageArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, errors.New("path is required")
	}
	data, declaredMIME, err := s.decodeImageInput(args)
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", maxImageBytes)
	}
	detected := http.DetectContentType(data)
	if !supportedImageMIME(detected) {
		return nil, fmt.Errorf("unsupported or invalid image type: %s", detected)
	}
	if declaredMIME != "" && normalizeImageMIME(declaredMIME) != normalizeImageMIME(detected) {
		return nil, fmt.Errorf("declared MIME type %s does not match image bytes %s", declaredMIME, detected)
	}
	_, target, display, err := s.prepareImageTarget(args.Path, args.Overwrite, args.CreateParents)
	if err != nil {
		return nil, err
	}
	if err := validateImageExtension(target, detected); err != nil {
		return nil, err
	}
	if err := atomicWriteFile(target, data, 0o600); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	result := map[string]any{
		"path":      display,
		"mimeType":  normalizeImageMIME(detected),
		"sizeBytes": len(data),
		"sha256":    hex.EncodeToString(digest[:]),
		"saved":     true,
	}
	return result, nil
}

func (s *Server) viewImage(args viewImageArgs) (map[string]any, error) {
	_, target, display, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("image path must be a regular file")
	}
	limit := args.MaxBytes
	if limit <= 0 || limit > maxImageBytes {
		limit = maxImageBytes
	}
	if info.Size() > int64(limit) {
		return nil, fmt.Errorf("image size %d exceeds maxBytes %d", info.Size(), limit)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	mimeType := normalizeImageMIME(http.DetectContentType(data))
	if !supportedImageMIME(mimeType) {
		return nil, fmt.Errorf("unsupported image type: %s", mimeType)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return map[string]any{
		"path": display, "mimeType": mimeType, "sizeBytes": len(data),
		"_mcpImageData": encoded, "_mcpImageMimeType": mimeType,
	}, nil
}

func (s *Server) decodeImageInput(args writeImageArgs) ([]byte, string, error) {
	sourceCount := 0
	if strings.TrimSpace(args.DataURL) != "" {
		sourceCount++
	}
	if strings.TrimSpace(args.Data) != "" {
		sourceCount++
	}
	if sourceCount == 0 {
		return nil, "", errors.New("data or dataUrl is required")
	}
	if sourceCount > 1 {
		return nil, "", errors.New("provide exactly one image source: data or dataUrl")
	}
	if strings.TrimSpace(args.DataURL) != "" {
		value := strings.TrimSpace(args.DataURL)
		comma := strings.IndexByte(value, ',')
		if comma < 0 || !strings.HasPrefix(strings.ToLower(value[:comma]), "data:image/") || !strings.Contains(strings.ToLower(value[:comma]), ";base64") {
			return nil, "", errors.New("dataUrl must be a base64 image data URL")
		}
		mimeType := strings.TrimSpace(strings.Split(strings.TrimPrefix(value[:comma], "data:"), ";")[0])
		data, err := base64.StdEncoding.DecodeString(value[comma+1:])
		if err != nil {
			return nil, "", errors.New("dataUrl contains invalid base64")
		}
		return data, mimeType, nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(args.Data))
	if err != nil {
		return nil, "", errors.New("data contains invalid base64")
	}
	return data, strings.TrimSpace(args.MIMEType), nil
}

func (s *Server) prepareImageTarget(path string, overwrite, createParents bool) (root, target, display string, err error) {
	root, target, display, err = s.resolveWorkspacePathForCreate(path)
	if err != nil {
		return "", "", "", err
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			return "", "", "", errors.New("image target must be a regular non-symlink file")
		}
		if !overwrite {
			return "", "", "", errors.New("image already exists; set overwrite=true to replace it")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", "", statErr
	}
	parent := filepath.Dir(target)
	if createParents {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return "", "", "", fmt.Errorf("create image parent directory: %w", err)
		}
	}
	if info, statErr := os.Stat(parent); statErr != nil || !info.IsDir() {
		return "", "", "", errors.New("image parent directory does not exist; enable create_parents/createParents")
	}
	return root, target, display, nil
}

func validateImageExtension(path, mimeType string) error {
	extension := strings.ToLower(filepath.Ext(path))
	allowed := map[string][]string{
		"image/png":  {".png"},
		"image/jpeg": {".jpg", ".jpeg"},
		"image/gif":  {".gif"},
		"image/webp": {".webp"},
	}
	for _, candidate := range allowed[normalizeImageMIME(mimeType)] {
		if extension == candidate {
			return nil
		}
	}
	if extension == "" {
		return errors.New("image destination must include a .png, .jpg, .jpeg, .gif, or .webp extension")
	}
	return fmt.Errorf("image destination extension %s does not match %s", extension, normalizeImageMIME(mimeType))
}

func supportedImageMIME(value string) bool {
	switch normalizeImageMIME(value) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func normalizeImageMIME(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}
