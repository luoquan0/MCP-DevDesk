package mcpcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxWritableFileBytes = 16 * 1024 * 1024

type writeFileArgs struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	Overwrite     bool   `json:"overwrite,omitempty"`
	CreateParents bool   `json:"createParents,omitempty"`
}

type replaceTextArgs struct {
	Path                 string `json:"path"`
	OldText              string `json:"oldText"`
	NewText              string `json:"newText"`
	ExpectedReplacements int    `json:"expectedReplacements,omitempty"`
}

type patchOperation struct {
	OldText    string `json:"oldText"`
	NewText    string `json:"newText"`
	ReplaceAll bool   `json:"replaceAll,omitempty"`
}

type applyPatchArgs struct {
	Path        string           `json:"path"`
	Operations  []patchOperation `json:"operations"`
	Patch       string           `json:"patch,omitempty"`
	DryRun      bool             `json:"dryRun,omitempty"`
	DryRunSnake bool             `json:"dry_run,omitempty"`
	Confirm     bool             `json:"confirm,omitempty"`
}

type makeDirectoryArgs struct {
	Path string `json:"path"`
}

type movePathArgs struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Overwrite bool   `json:"overwrite,omitempty"`
	Confirm   bool   `json:"confirm,omitempty"`
}

type deletePathArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
	Confirm   bool   `json:"confirm,omitempty"`
}

func writeFileTools() []Tool {
	return []Tool{
		{
			Name:        "write_file",
			Title:       "Write Workspace File",
			Description: "Atomically create or overwrite a UTF-8 text file inside the configured workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":          map[string]any{"type": "string", "minLength": 1},
					"content":       map[string]any{"type": "string"},
					"overwrite":     map[string]any{"type": "boolean", "default": false},
					"createParents": map[string]any{"type": "boolean", "default": false},
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "replace_text",
			Title:       "Replace Text",
			Description: "Replace an exact text fragment in a UTF-8 file and fail if the expected match count differs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":                 map[string]any{"type": "string", "minLength": 1},
					"oldText":              map[string]any{"type": "string", "minLength": 1},
					"newText":              map[string]any{"type": "string"},
					"expectedReplacements": map[string]any{"type": "integer", "minimum": 1, "default": 1},
				},
				"required":             []string{"path", "oldText", "newText"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "apply_patch",
			Title:       "Apply Text Patch",
			Description: "Apply a legacy Begin Patch envelope across files, or multiple exact replacements to one UTF-8 file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch":   map[string]any{"type": "string", "description": "Legacy *** Begin Patch envelope."},
					"dryRun":  map[string]any{"type": "boolean", "default": false},
					"dry_run": map[string]any{"type": "boolean", "description": "Legacy alias for dryRun."},
					"confirm": map[string]any{"type": "boolean", "default": false, "description": "Required for patch deletions or moves in trusted mode."},
					"path":    map[string]any{"type": "string", "minLength": 1},
					"operations": map[string]any{
						"type": "array", "minItems": 1, "maxItems": 100,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"oldText":    map[string]any{"type": "string", "minLength": 1},
								"newText":    map[string]any{"type": "string"},
								"replaceAll": map[string]any{"type": "boolean", "default": false},
							},
							"required":             []string{"oldText", "newText"},
							"additionalProperties": false,
						},
					},
				},
				"anyOf": []any{
					map[string]any{"required": []string{"patch"}},
					map[string]any{"required": []string{"path", "operations"}},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "make_directory",
			Title:       "Create Directory",
			Description: "Create a directory and missing parent directories inside the workspace.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string", "minLength": 1}},
				"required":   []string{"path"}, "additionalProperties": false,
			},
		},
		{
			Name:        "move_path",
			Title:       "Move Workspace Path",
			Description: "Move or rename a file or directory inside the workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source":    map[string]any{"type": "string", "minLength": 1},
					"target":    map[string]any{"type": "string", "minLength": 1},
					"overwrite": map[string]any{"type": "boolean", "default": false},
					"confirm":   map[string]any{"type": "boolean", "default": false},
				},
				"required": []string{"source", "target"}, "additionalProperties": false,
			},
		},
		{
			Name:        "delete_path",
			Title:       "Delete Workspace Path",
			Description: "Delete a workspace file or directory. Trusted mode requires confirm=true; safe mode rejects deletion.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "minLength": 1},
					"recursive": map[string]any{"type": "boolean", "default": false},
					"confirm":   map[string]any{"type": "boolean", "default": false},
				},
				"required": []string{"path"}, "additionalProperties": false,
			},
		},
	}
}

func (s *Server) executeWriteTool(name string, arguments map[string]any) (map[string]any, error) {
	switch name {
	case "write_file":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args writeFileArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.writeFile(args)
	case "replace_text":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args replaceTextArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.replaceText(args)
	case "apply_patch":
		var args applyPatchArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		args.DryRun = args.DryRun || args.DryRunSnake
		if strings.TrimSpace(args.Patch) != "" {
			return s.applyPatchEnvelope(args)
		}
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		return s.applyTextPatch(args)
	case "make_directory":
		if err := s.requireWritePermission(false, true); err != nil {
			return nil, err
		}
		var args makeDirectoryArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		return s.makeDirectory(args)
	case "move_path":
		var args movePathArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		if err := s.requireWritePermission(args.Overwrite, args.Confirm); err != nil {
			return nil, err
		}
		return s.movePath(args)
	case "delete_path":
		var args deletePathArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		if err := s.requireWritePermission(true, args.Confirm); err != nil {
			return nil, err
		}
		return s.deletePath(args)
	default:
		return nil, fmt.Errorf("unknown write tool: %s", name)
	}
}

func (s *Server) requireWritePermission(destructive, confirmed bool) error {
	switch s.permissionMode {
	case "safe":
		return errors.New("write operation denied in safe permission mode")
	case "trusted":
		if destructive && !confirmed {
			return errors.New("destructive operation requires confirm=true in trusted mode")
		}
		return nil
	case "dangerous":
		return nil
	default:
		return errors.New("invalid permission mode")
	}
}

func (s *Server) writeFile(args writeFileArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, errors.New("path is required")
	}
	if !utf8.ValidString(args.Content) {
		return nil, errors.New("content must be valid UTF-8")
	}
	if len(args.Content) > maxWritableFileBytes {
		return nil, fmt.Errorf("content exceeds %d bytes", maxWritableFileBytes)
	}
	_, target, relative, err := s.resolveWorkspacePathForCreate(args.Path)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("refusing to overwrite a symbolic link")
		}
		if info.IsDir() {
			return nil, errors.New("path references a directory")
		}
		if !args.Overwrite {
			return nil, errors.New("file already exists; set overwrite=true to replace it")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect target: %w", statErr)
	}
	parent := filepath.Dir(target)
	if args.CreateParents {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, fmt.Errorf("create parent directories: %w", err)
		}
	} else if info, statErr := os.Stat(parent); statErr != nil || !info.IsDir() {
		return nil, errors.New("parent directory does not exist; set createParents=true to create it")
	}
	if err := atomicWriteFile(target, []byte(args.Content), 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": relative, "bytesWritten": len(args.Content), "created": true}, nil
}

func (s *Server) replaceText(args replaceTextArgs) (map[string]any, error) {
	if args.OldText == "" {
		return nil, errors.New("oldText is required")
	}
	if args.ExpectedReplacements <= 0 {
		args.ExpectedReplacements = 1
	}
	_, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	content, err := readWritableTextFile(target)
	if err != nil {
		return nil, err
	}
	count := strings.Count(content, args.OldText)
	if count != args.ExpectedReplacements {
		return nil, fmt.Errorf("expected %d replacement matches, found %d", args.ExpectedReplacements, count)
	}
	updated := strings.ReplaceAll(content, args.OldText, args.NewText)
	if len(updated) > maxWritableFileBytes {
		return nil, fmt.Errorf("updated content exceeds %d bytes", maxWritableFileBytes)
	}
	if err := atomicWriteFile(target, []byte(updated), 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": relative, "replacements": count, "bytesWritten": len(updated)}, nil
}

func (s *Server) applyTextPatch(args applyPatchArgs) (map[string]any, error) {
	if len(args.Operations) == 0 || len(args.Operations) > 100 {
		return nil, errors.New("operations must contain between 1 and 100 entries")
	}
	_, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	content, err := readWritableTextFile(target)
	if err != nil {
		return nil, err
	}
	total := 0
	for index, operation := range args.Operations {
		if operation.OldText == "" {
			return nil, fmt.Errorf("operation %d oldText is required", index+1)
		}
		count := strings.Count(content, operation.OldText)
		if count == 0 {
			return nil, fmt.Errorf("operation %d did not match the file", index+1)
		}
		if !operation.ReplaceAll && count != 1 {
			return nil, fmt.Errorf("operation %d matched %d times; set replaceAll=true or use a more specific fragment", index+1, count)
		}
		if operation.ReplaceAll {
			content = strings.ReplaceAll(content, operation.OldText, operation.NewText)
			total += count
		} else {
			content = strings.Replace(content, operation.OldText, operation.NewText, 1)
			total++
		}
		if len(content) > maxWritableFileBytes {
			return nil, fmt.Errorf("patched content exceeds %d bytes", maxWritableFileBytes)
		}
	}
	if err := atomicWriteFile(target, []byte(content), 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": relative, "operations": len(args.Operations), "replacements": total, "bytesWritten": len(content)}, nil
}

func (s *Server) makeDirectory(args makeDirectoryArgs) (map[string]any, error) {
	_, target, relative, err := s.resolveWorkspacePathForCreate(args.Path)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return nil, errors.New("workspace root already exists")
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	return map[string]any{"path": relative, "created": true}, nil
}

func (s *Server) movePath(args movePathArgs) (map[string]any, error) {
	_, source, sourceRelative, err := s.resolveWorkspacePath(args.Source)
	if err != nil {
		return nil, err
	}
	_, target, targetRelative, err := s.resolveWorkspacePathForCreate(args.Target)
	if err != nil {
		return nil, err
	}
	if sourceRelative == "." || targetRelative == "." {
		return nil, errors.New("workspace root cannot be moved or replaced")
	}
	if pathWithin(source, target) && source != target {
		return nil, errors.New("cannot move a directory into itself")
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		if !args.Overwrite {
			return nil, errors.New("target already exists; set overwrite=true to replace it")
		}
		if err := os.RemoveAll(target); err != nil {
			return nil, fmt.Errorf("remove existing target: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect target: %w", statErr)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, fmt.Errorf("create target parent: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		return nil, fmt.Errorf("move path: %w", err)
	}
	return map[string]any{"source": sourceRelative, "target": targetRelative, "moved": true}, nil
}

func (s *Server) deletePath(args deletePathArgs) (map[string]any, error) {
	_, target, relative, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return nil, errors.New("workspace root cannot be deleted")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return nil, fmt.Errorf("inspect path: %w", err)
	}
	if info.IsDir() && !args.Recursive {
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return nil, readErr
		}
		if len(entries) > 0 {
			return nil, errors.New("directory is not empty; set recursive=true to delete it")
		}
	}
	if info.IsDir() && args.Recursive {
		err = os.RemoveAll(target)
	} else {
		err = os.Remove(target)
	}
	if err != nil {
		return nil, fmt.Errorf("delete path: %w", err)
	}
	return map[string]any{"path": relative, "deleted": true}, nil
}

func readWritableTextFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path must reference a regular file")
	}
	if info.Size() > maxWritableFileBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxWritableFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if !utf8.Valid(raw) {
		return "", errors.New("file is not valid UTF-8 text")
	}
	return string(raw), nil
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	temp, err := os.CreateTemp(parent, ".mcp-devdesk-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		cleanup()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

func (s *Server) resolveWorkspacePathForCreate(value string) (root, target, relative string, err error) {
	workspace, err := s.workspaceRoot()
	if err != nil {
		return "", "", "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", errors.New("path is required")
	}
	if filepath.IsAbs(value) {
		target = filepath.Clean(value)
	} else {
		target = filepath.Join(s.currentDefaultCWD(), value)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve path: %w", err)
	}
	target = filepath.Clean(target)
	root, err = s.allowedRootFor(target)
	if err != nil {
		return "", "", "", err
	}
	existing := target
	suffix := make([]string, 0)
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", "", fmt.Errorf("inspect path ancestor: %w", statErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", "", "", errors.New("no existing path ancestor found")
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	evaluated, evalErr := filepath.EvalSymlinks(existing)
	if evalErr != nil {
		return "", "", "", fmt.Errorf("resolve path ancestor: %w", evalErr)
	}
	if _, rootErr := s.allowedRootFor(evaluated); rootErr != nil {
		return "", "", "", errors.New("path ancestor escapes the configured file scope")
	}
	target = evaluated
	for index := len(suffix) - 1; index >= 0; index-- {
		target = filepath.Join(target, suffix[index])
	}
	root, err = s.allowedRootFor(target)
	if err != nil {
		return "", "", "", err
	}
	relative, err = filepath.Rel(root, target)
	if err != nil {
		return "", "", "", err
	}
	if relative == "." {
		return root, target, ".", nil
	}
	if !sameFilesystemPath(root, workspace) && s.fileScope != "workspace" {
		return root, target, filepath.ToSlash(target), nil
	}
	return root, target, filepath.ToSlash(relative), nil
}
