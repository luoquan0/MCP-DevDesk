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

# First-stage relative path repair; fix3 below then anchors this to the raw active
# workspace before Windows canonicalization changes its spelling.
replace_once(
    "app/internal/mcpcore/file_tools.go",
    '''\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tif absolute, absErr := filepath.Abs(base); absErr == nil {\n\t\t\tbase = filepath.Clean(absolute)\n\t\t}\n\t\tif evaluated, evalErr := filepath.EvalSymlinks(base); evalErr == nil {\n\t\t\tbase = filepath.Clean(evaluated)\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
    '''\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tconfiguredBase := filepath.Clean(strings.TrimSpace(s.workspace))\n\t\tif configuredBase != "" {\n\t\t\tif absolute, absErr := filepath.Abs(configuredBase); absErr == nil {\n\t\t\t\tconfiguredBase = filepath.Clean(absolute)\n\t\t\t}\n\t\t\tif relativeBase, relErr := filepath.Rel(configuredBase, base); relErr == nil && pathWithin(configuredBase, base) {\n\t\t\t\tbase = filepath.Join(workspace, relativeBase)\n\t\t\t}\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
)

# The product intentionally moved from machine-global to per-instance Screen Vision.
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

# Normalize CRLF for a semantic task-accept assertion.
tasks_test = read("app/internal/agentstate/tasks_test.go")
tasks_test = tasks_test.replace(
    '''\tif string(raw) != "updated\\n" {\n\t\tt.Fatalf("accepted content = %q", raw)\n\t}\n''',
    '''\tif strings.ReplaceAll(string(raw), "\\r\\n", "\\n") != "updated\\n" {\n\t\tt.Fatalf("accepted content = %q", raw)\n\t}\n''',
)
write("app/internal/agentstate/tasks_test.go", tasks_test)

# Hosted Actions has no interactive desktop; keep UIA integration coverage for real
# interactive Windows while skipping only that environmental probe in CI.
uia_test = read("app/internal/mcpcore/ui_automation_windows_test.go")
uia_test = uia_test.replace('import (\n\t"encoding/base64"', 'import (\n\t"encoding/base64"\n\t"os"')
uia_test = uia_test.replace(
    "func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {\n",
    '''func TestUIAutomationReadsOwnedNativeWindow(t *testing.T) {\n\tif strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {\n\t\tt.Skip("hosted GitHub Actions has no interactive Windows desktop for UI Automation")\n\t}\n''',
)
write("app/internal/mcpcore/ui_automation_windows_test.go", uia_test)

# Apply the stricter active-workspace canonicalization repair after the first-stage
# edit above. Kept in a separate file so the repair is easy to review independently.
fix3 = ROOT / "tools" / "v013_fix3.py"
exec(compile(fix3.read_text(encoding="utf-8"), str(fix3), "exec"), globals(), globals())

print("v0.13 fix2 applied")
