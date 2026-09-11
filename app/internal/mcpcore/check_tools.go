package mcpcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type checksRunArgs struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	WaitMillis int    `json:"waitMillis,omitempty"`
}

type detectedCheck struct {
	Runtime string
	Command string
	Args    []string
	Label   string
}

func checkTools() []Tool {
	return []Tool{{
		Name: "checks_run", Title: "Run Structured Project Check",
		Description: "Auto-detect the project toolchain and run a structured test, check, or format verification without requiring the MCP client to assemble shell commands. The returned Job ID is persisted.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"type":       map[string]any{"type": "string", "enum": []string{"test", "check", "format"}},
			"path":       map[string]any{"type": "string", "default": "."},
			"waitMillis": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000, "default": 30000},
		}, "required": []string{"type"}, "additionalProperties": false},
	}}
}

func (s *Server) executeCheckTool(name string, arguments map[string]any) (map[string]any, error) {
	if name != "checks_run" {
		return nil, fmt.Errorf("unknown check tool: %s", name)
	}
	if s.permissionMode == "safe" {
		return nil, errors.New("structured checks require trusted or dangerous permission mode")
	}
	if err := s.requireTaskEditable(); err != nil {
		return nil, err
	}
	var args checksRunArgs
	if err := decodeToolArguments(arguments, &args); err != nil {
		return nil, err
	}
	args.Type = strings.ToLower(strings.TrimSpace(args.Type))
	if args.Type != "test" && args.Type != "check" && args.Type != "format" {
		return nil, errors.New("type must be test, check, or format")
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}
	_, cwd, display, err := s.resolveWorkspacePath(args.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, errors.New("check path must be an existing directory")
	}
	detected, err := detectStructuredCheck(cwd, args.Type)
	if err != nil {
		return nil, err
	}
	wait := args.WaitMillis
	if wait == 0 {
		wait = 30000
	}
	result, err := s.commands.start(execCommandArgs{Command: detected.Command, Args: detected.Args, CWD: display, TimeoutSeconds: 900, WaitMillis: wait, Kind: "check:" + args.Type})
	if err != nil {
		return nil, err
	}
	result["checkType"] = args.Type
	result["detectedRuntime"] = detected.Runtime
	result["checkLabel"] = detected.Label
	result["structured"] = true
	return result, nil
}

func detectStructuredCheck(cwd, checkType string) (detectedCheck, error) {
	exists := func(name string) bool {
		info, err := os.Stat(filepath.Join(cwd, name))
		return err == nil && !info.IsDir()
	}
	if exists("build.ps1") && runtime.GOOS == "windows" && (checkType == "test" || checkType == "check") {
		return detectedCheck{Runtime: "powershell", Command: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "build.ps1", "-Arch", "amd64", "-RunTests"}, Label: "MCP DevDesk full Windows build and tests"}, nil
	}
	if exists("go.mod") {
		vendor := false
		if info, err := os.Stat(filepath.Join(cwd, "vendor")); err == nil && info.IsDir() {
			vendor = true
		}
		switch checkType {
		case "test":
			args := []string{"test"}
			if vendor {
				args = append(args, "-mod=vendor")
			}
			args = append(args, "./...")
			return detectedCheck{Runtime: "go", Command: "go", Args: args, Label: "Go tests"}, nil
		case "check":
			args := []string{"vet"}
			if vendor {
				args = append(args, "-mod=vendor")
			}
			args = append(args, "./...")
			return detectedCheck{Runtime: "go", Command: "go", Args: args, Label: "Go vet"}, nil
		case "format":
			if runtime.GOOS == "windows" {
				script := `$files = git ls-files '*.go'; if (-not $files) { exit 0 }; $bad = @(); foreach ($f in $files) { $out = & gofmt -l -- $f; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; if ($out) { $bad += $out } }; if ($bad.Count -gt 0) { $bad | Write-Output; exit 1 }`
				return detectedCheck{Runtime: "go", Command: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}, Label: "gofmt verification"}, nil
			}
			return detectedCheck{Runtime: "go", Command: "sh", Args: []string{"-c", `files=$(git ls-files '*.go'); [ -z "$files" ] || { out=$(gofmt -l $files); [ -z "$out" ] || { printf '%s\n' "$out"; exit 1; }; }`}, Label: "gofmt verification"}, nil
		}
	}
	if exists("package.json") {
		raw, err := os.ReadFile(filepath.Join(cwd, "package.json"))
		if err != nil {
			return detectedCheck{}, err
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return detectedCheck{}, errors.New("package.json is invalid")
		}
		choices := map[string][]string{
			"test":   {"test"},
			"check":  {"check", "typecheck", "lint", "build"},
			"format": {"format:check", "check:format", "prettier:check"},
		}
		for _, script := range choices[checkType] {
			if strings.TrimSpace(pkg.Scripts[script]) != "" {
				return detectedCheck{Runtime: "node", Command: "npm", Args: []string{"run", script}, Label: "npm run " + script}, nil
			}
		}
		available := make([]string, 0, len(pkg.Scripts))
		for name := range pkg.Scripts {
			available = append(available, name)
		}
		sort.Strings(available)
		return detectedCheck{}, fmt.Errorf("package.json has no safe auto-detected %s script; available scripts: %s", checkType, strings.Join(available, ", "))
	}
	if exists("Cargo.toml") {
		switch checkType {
		case "test":
			return detectedCheck{Runtime: "rust", Command: "cargo", Args: []string{"test", "--all-targets"}, Label: "cargo test"}, nil
		case "check":
			return detectedCheck{Runtime: "rust", Command: "cargo", Args: []string{"check", "--all-targets"}, Label: "cargo check"}, nil
		case "format":
			return detectedCheck{Runtime: "rust", Command: "cargo", Args: []string{"fmt", "--all", "--", "--check"}, Label: "cargo fmt --check"}, nil
		}
	}
	if exists("pyproject.toml") || exists("pytest.ini") || exists("requirements.txt") {
		python := "python"
		if runtime.GOOS != "windows" {
			python = "python3"
		}
		switch checkType {
		case "test":
			return detectedCheck{Runtime: "python", Command: python, Args: []string{"-m", "pytest"}, Label: "pytest"}, nil
		case "check":
			return detectedCheck{Runtime: "python", Command: python, Args: []string{"-m", "compileall", "-q", "."}, Label: "Python compile check"}, nil
		case "format":
			return detectedCheck{}, errors.New("no safe Python formatter was auto-detected; add a project format-check script or tool configuration")
		}
	}
	return detectedCheck{}, fmt.Errorf("could not auto-detect a structured %s check in %s", checkType, cwd)
}
