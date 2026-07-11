package mcpcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxPatchTransactionBytes = 64 * 1024 * 1024

type patchFileAction struct {
	Kind   string
	Path   string
	MoveTo string
	Body   []string
}

type patchHunk struct {
	OldLines []string
	NewLines []string
}

type plannedPatchChange struct {
	Path           string
	DisplayPath    string
	Delete         bool
	Content        []byte
	Original       []byte
	OriginalExists bool
	Mode           os.FileMode
}

func (s *Server) applyPatchEnvelope(args applyPatchArgs) (map[string]any, error) {
	actions, err := parsePatchEnvelope(args.Patch)
	if err != nil {
		return nil, err
	}
	changes, destructive, err := s.planPatchChanges(actions)
	if err != nil {
		return nil, err
	}
	if args.DryRun {
		return patchPlanResult(changes, true), nil
	}
	if err := s.requireWritePermission(destructive, args.Confirm); err != nil {
		return nil, err
	}
	if err := commitPatchChanges(changes); err != nil {
		return nil, err
	}
	return patchPlanResult(changes, false), nil
}

func parsePatchEnvelope(value string) ([]patchFileAction, error) {
	value = normalizeText(value)
	lines := strings.Split(value, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, errors.New("patch must start with *** Begin Patch and end with *** End Patch")
	}
	actions := make([]patchFileAction, 0)
	for index := 1; index < len(lines)-1; {
		line := lines[index]
		kind, path, ok := parsePatchFileHeader(line)
		if !ok {
			if strings.TrimSpace(line) == "" {
				index++
				continue
			}
			return nil, fmt.Errorf("unexpected patch line %d: %s", index+1, line)
		}
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("patch file path is empty at line %d", index+1)
		}
		index++
		bodyStart := index
		for index < len(lines)-1 {
			if _, _, next := parsePatchFileHeader(lines[index]); next {
				break
			}
			index++
		}
		body := append([]string(nil), lines[bodyStart:index]...)
		action := patchFileAction{Kind: kind, Path: strings.TrimSpace(path), Body: body}
		if kind == "update" && len(action.Body) > 0 && strings.HasPrefix(action.Body[0], "*** Move to: ") {
			action.MoveTo = strings.TrimSpace(strings.TrimPrefix(action.Body[0], "*** Move to: "))
			action.Body = action.Body[1:]
			if action.MoveTo == "" {
				return nil, fmt.Errorf("move target is empty for %s", action.Path)
			}
		}
		actions = append(actions, action)
		if len(actions) > 100 {
			return nil, errors.New("patch contains more than 100 file actions")
		}
	}
	if len(actions) == 0 {
		return nil, errors.New("patch contains no file actions")
	}
	return actions, nil
}

func parsePatchFileHeader(line string) (kind, path string, ok bool) {
	for _, candidate := range []struct {
		Prefix string
		Kind   string
	}{
		{"*** Add File: ", "add"},
		{"*** Update File: ", "update"},
		{"*** Delete File: ", "delete"},
	} {
		if strings.HasPrefix(line, candidate.Prefix) {
			return candidate.Kind, strings.TrimPrefix(line, candidate.Prefix), true
		}
	}
	return "", "", false
}

func (s *Server) planPatchChanges(actions []patchFileAction) ([]plannedPatchChange, bool, error) {
	changes := make([]plannedPatchChange, 0, len(actions)*2)
	seen := make(map[string]struct{})
	totalBytes := 0
	destructive := false
	for _, action := range actions {
		switch action.Kind {
		case "add":
			_, target, display, err := s.resolveWorkspacePathForCreate(action.Path)
			if err != nil {
				return nil, false, err
			}
			if _, err := os.Lstat(target); err == nil {
				return nil, false, fmt.Errorf("cannot add %s: path already exists", action.Path)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, false, err
			}
			content, err := parseAddedFile(action.Body)
			if err != nil {
				return nil, false, fmt.Errorf("add %s: %w", action.Path, err)
			}
			totalBytes += len(content)
			change := plannedPatchChange{Path: target, DisplayPath: display, Content: content, Mode: 0o600}
			if err := appendUniquePatchChange(&changes, seen, change); err != nil {
				return nil, false, err
			}
		case "update":
			_, source, sourceDisplay, err := s.resolveWorkspacePath(action.Path)
			if err != nil {
				return nil, false, err
			}
			original, mode, newline, err := readPatchSource(source)
			if err != nil {
				return nil, false, fmt.Errorf("update %s: %w", action.Path, err)
			}
			updated, err := applyPatchHunks(normalizeText(string(original)), action.Body)
			if err != nil {
				return nil, false, fmt.Errorf("update %s: %w", action.Path, err)
			}
			if newline == "\r\n" {
				updated = strings.ReplaceAll(updated, "\n", "\r\n")
			}
			if len(updated) > maxWritableFileBytes {
				return nil, false, fmt.Errorf("updated file %s exceeds %d bytes", action.Path, maxWritableFileBytes)
			}
			totalBytes += len(original) + len(updated)
			if action.MoveTo == "" {
				change := plannedPatchChange{
					Path: source, DisplayPath: sourceDisplay, Content: []byte(updated),
					Original: original, OriginalExists: true, Mode: mode,
				}
				if err := appendUniquePatchChange(&changes, seen, change); err != nil {
					return nil, false, err
				}
				break
			}
			destructive = true
			_, target, targetDisplay, err := s.resolveWorkspacePathForCreate(action.MoveTo)
			if err != nil {
				return nil, false, err
			}
			if _, err := os.Lstat(target); err == nil {
				return nil, false, fmt.Errorf("move target %s already exists", action.MoveTo)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, false, err
			}
			if err := appendUniquePatchChange(&changes, seen, plannedPatchChange{
				Path: target, DisplayPath: targetDisplay, Content: []byte(updated), Mode: mode,
			}); err != nil {
				return nil, false, err
			}
			if err := appendUniquePatchChange(&changes, seen, plannedPatchChange{
				Path: source, DisplayPath: sourceDisplay, Delete: true,
				Original: original, OriginalExists: true, Mode: mode,
			}); err != nil {
				return nil, false, err
			}
		case "delete":
			destructive = true
			_, target, display, err := s.resolveWorkspacePath(action.Path)
			if err != nil {
				return nil, false, err
			}
			original, mode, _, err := readPatchSource(target)
			if err != nil {
				return nil, false, fmt.Errorf("delete %s: %w", action.Path, err)
			}
			totalBytes += len(original)
			if err := appendUniquePatchChange(&changes, seen, plannedPatchChange{
				Path: target, DisplayPath: display, Delete: true,
				Original: original, OriginalExists: true, Mode: mode,
			}); err != nil {
				return nil, false, err
			}
		default:
			return nil, false, fmt.Errorf("unsupported patch action %s", action.Kind)
		}
		if totalBytes > maxPatchTransactionBytes {
			return nil, false, fmt.Errorf("patch transaction exceeds %d bytes", maxPatchTransactionBytes)
		}
	}
	return changes, destructive, nil
}

func appendUniquePatchChange(changes *[]plannedPatchChange, seen map[string]struct{}, change plannedPatchChange) error {
	key := filepath.Clean(change.Path)
	if _, exists := seen[key]; exists {
		return fmt.Errorf("patch modifies the same path more than once: %s", change.DisplayPath)
	}
	seen[key] = struct{}{}
	*changes = append(*changes, change)
	return nil
}

func parseAddedFile(lines []string) ([]byte, error) {
	content := make([]string, 0, len(lines))
	for index, line := range lines {
		if line == "\\ No newline at end of file" {
			continue
		}
		if !strings.HasPrefix(line, "+") {
			return nil, fmt.Errorf("line %d must begin with +", index+1)
		}
		content = append(content, strings.TrimPrefix(line, "+"))
	}
	result := strings.Join(content, "\n")
	if len(content) > 0 {
		result += "\n"
	}
	if !utf8.ValidString(result) || len(result) > maxWritableFileBytes {
		return nil, errors.New("added file is invalid UTF-8 or too large")
	}
	return []byte(result), nil
}

func readPatchSource(path string) ([]byte, os.FileMode, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, 0, "", errors.New("patch source must be a regular non-symlink file")
	}
	if info.Size() > maxWritableFileBytes {
		return nil, 0, "", errors.New("patch source is too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, "", err
	}
	if !utf8.Valid(raw) {
		return nil, 0, "", errors.New("patch source is not UTF-8 text")
	}
	newline := "\n"
	if strings.Contains(string(raw), "\r\n") {
		newline = "\r\n"
	}
	return raw, info.Mode().Perm(), newline, nil
}

func applyPatchHunks(content string, body []string) (string, error) {
	hunks, err := parsePatchHunks(body)
	if err != nil {
		return "", err
	}
	for index, hunk := range hunks {
		oldBlock := strings.Join(hunk.OldLines, "\n")
		newBlock := strings.Join(hunk.NewLines, "\n")
		if oldBlock == "" {
			return "", fmt.Errorf("hunk %d has no context or deleted lines", index+1)
		}
		count := strings.Count(content, oldBlock)
		needle := oldBlock
		replacement := newBlock
		if count == 0 && strings.Count(content, oldBlock+"\n") == 1 {
			needle = oldBlock + "\n"
			replacement = newBlock
			if replacement != "" {
				replacement += "\n"
			}
			count = 1
		}
		if count != 1 {
			return "", fmt.Errorf("hunk %d expected one match, found %d", index+1, count)
		}
		content = strings.Replace(content, needle, replacement, 1)
	}
	return content, nil
}

func parsePatchHunks(lines []string) ([]patchHunk, error) {
	hunks := make([]patchHunk, 0)
	current := patchHunk{}
	flush := func() {
		if len(current.OldLines) == 0 && len(current.NewLines) == 0 {
			return
		}
		hunks = append(hunks, current)
		current = patchHunk{}
	}
	for index, line := range lines {
		if strings.HasPrefix(line, "@@") {
			flush()
			continue
		}
		if line == "\\ No newline at end of file" {
			continue
		}
		if line == "" && index == len(lines)-1 {
			continue
		}
		if line == "" {
			return nil, fmt.Errorf("unprefixed empty line at patch body line %d", index+1)
		}
		switch line[0] {
		case ' ':
			current.OldLines = append(current.OldLines, line[1:])
			current.NewLines = append(current.NewLines, line[1:])
		case '-':
			current.OldLines = append(current.OldLines, line[1:])
		case '+':
			current.NewLines = append(current.NewLines, line[1:])
		default:
			return nil, fmt.Errorf("invalid patch prefix at body line %d", index+1)
		}
	}
	flush()
	if len(hunks) == 0 {
		return nil, errors.New("update action contains no hunks")
	}
	return hunks, nil
}

func commitPatchChanges(changes []plannedPatchChange) error {
	applied := make([]plannedPatchChange, 0, len(changes))
	rollback := func() {
		for index := len(applied) - 1; index >= 0; index-- {
			change := applied[index]
			if change.OriginalExists {
				_ = os.MkdirAll(filepath.Dir(change.Path), 0o700)
				_ = atomicWriteFile(change.Path, change.Original, change.Mode)
			} else {
				_ = os.Remove(change.Path)
			}
		}
	}
	for _, change := range changes {
		if change.Delete {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(change.Path), 0o700); err != nil {
			rollback()
			return err
		}
		if err := atomicWriteFile(change.Path, change.Content, change.Mode); err != nil {
			rollback()
			return err
		}
		applied = append(applied, change)
	}
	for _, change := range changes {
		if !change.Delete {
			continue
		}
		if err := os.Remove(change.Path); err != nil {
			rollback()
			return fmt.Errorf("delete patched file %s: %w", change.DisplayPath, err)
		}
		applied = append(applied, change)
	}
	return nil
}

func patchPlanResult(changes []plannedPatchChange, dryRun bool) map[string]any {
	items := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		action := "write"
		if change.Delete {
			action = "delete"
		} else if !change.OriginalExists {
			action = "add"
		}
		items = append(items, map[string]any{
			"path": change.DisplayPath, "action": action, "bytes": len(change.Content),
		})
	}
	return map[string]any{"applied": !dryRun, "dryRun": dryRun, "changes": items, "count": len(items)}
}
