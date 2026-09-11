from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8", newline="\n")


def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise RuntimeError(f"missing marker in {path}: {old[:140]!r}")
    write(path, text.replace(old, new, 1))


# model.Config already calls this field ScreenCaptureMode. Keep the new instance
# management API aligned with the established JSON vocabulary instead of inventing
# a parallel ScreenVisionMode name.
for path in ["app/internal/model/types.go", "app/internal/application/instances.go"]:
    text = read(path)
    text = text.replace("ScreenVisionMode", "ScreenCaptureMode")
    text = text.replace("screenVisionMode", "screenCaptureMode")
    write(path, text)

# Relative paths need to follow the active Agent Task worktree. Map the current
# default CWD from the configured base workspace to the canonical active workspace.
# This also normalizes GitHub Windows runner long-path / 8.3-path aliases because
# target construction is anchored on workspaceRoot(), not the original spelling.
replace_once(
    "app/internal/mcpcore/file_tools.go",
    '''\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tif absolute, absErr := filepath.Abs(base); absErr == nil {\n\t\t\tbase = filepath.Clean(absolute)\n\t\t}\n\t\tif evaluated, evalErr := filepath.EvalSymlinks(base); evalErr == nil {\n\t\t\tbase = filepath.Clean(evaluated)\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
    '''\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tconfiguredBase := filepath.Clean(strings.TrimSpace(s.workspace))\n\t\tif configuredBase != "" {\n\t\t\tif absolute, absErr := filepath.Abs(configuredBase); absErr == nil {\n\t\t\t\tconfiguredBase = filepath.Clean(absolute)\n\t\t\t}\n\t\t\tif relativeBase, relErr := filepath.Rel(configuredBase, base); relErr == nil && pathWithin(configuredBase, base) {\n\t\t\t\tbase = filepath.Join(workspace, relativeBase)\n\t\t\t}\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
)

# The product intentionally moved from machine-global to per-instance Screen Vision.
# Update the old regression test to assert the new privacy boundary.
process_test = read("app/internal/process/screen_vision_args_test.go")
process_test = process_test.replace(
    "func TestMCPArgumentsManagedInstanceUsesPrimaryScreenVisionConfig(t *testing.T)",
    "func TestMCPArgumentsManagedInstanceUsesOwnScreenVisionConfig(t *testing.T)",
)
process_test = process_test.replace(
    '''\tif got := argumentValue(args, "--screen-vision-config"); got != filepath.Join(primaryDataDir, "config.json") {\n\t\tt.Fatalf("managed screen vision config = %q, want primary config", got)\n\t}\n''',
    '''\tif got := argumentValue(args, "--screen-vision-config"); got != filepath.Join(instanceDataDir, "config.json") {\n\t\tt.Fatalf("managed screen vision config = %q, want instance config", got)\n\t}\n''',
)
write("app/internal/process/screen_vision_args_test.go", process_test)

# Git on Windows may check out/apply the accepted text with CRLF. This test is about
# task isolation/accept semantics, not line-ending policy, so normalize for comparison.
tasks_test = read("app/internal/agentstate/tasks_test.go")
tasks_test = tasks_test.replace(
    '''\tif string(raw) != "updated\\n" {\n\t\tt.Fatalf("accepted content = %q", raw)\n\t}\n''',
    '''\tif strings.ReplaceAll(string(raw), "\\r\\n", "\\n") != "updated\\n" {\n\t\tt.Fatalf("accepted content = %q", raw)\n\t}\n''',
)
write("app/internal/agentstate/tasks_test.go", tasks_test)

# Windows UI Automation requires an interactive desktop. Hosted GitHub Actions uses
# a service session where UIA can legitimately time out even though compilation and
# pure logic are healthy. Keep the real UIA integration test for local/interactive
# Windows runs and skip only this environmental integration probe in hosted CI.
uia_test = read("app/internal/mcpcore/ui_automation_windows_test.go")
uia_test = uia_test.replace('import (\n\t"encoding/base64"', 'import (\n\t"encoding/base64"\n\t"os"')
uia_test = uia_test.replace(
    "func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {\n",
    '''func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {\n\tif strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {\n\t\tt.Skip("hosted GitHub Actions has no interactive Windows desktop for UI Automation")\n\t}\n''',
)
write("app/internal/mcpcore/ui_automation_windows_test.go", uia_test)

print("v0.13 fix2 applied")
