from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8", newline="\n")


def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise RuntimeError(f"missing marker in {path}: {old[:180]!r}")
    write(path, text.replace(old, new, 1))


# Windows can canonicalize an existing temp/worktree directory through an 8.3 alias
# when EvalSymlinks runs. Resolve user-relative/default-CWD paths against the *raw*
# active workspace first, then project the relative suffix onto the canonical root.
# This preserves the containment boundary instead of relaxing it.
replace_once(
    "app/internal/mcpcore/file_tools.go",
    '''\tif filepath.IsAbs(value) {\n\t\ttarget = filepath.Clean(value)\n\t} else {\n\t\tbase := filepath.Clean(s.currentDefaultCWD())\n\t\tconfiguredBase := filepath.Clean(strings.TrimSpace(s.workspace))\n\t\tif configuredBase != "" {\n\t\t\tif absolute, absErr := filepath.Abs(configuredBase); absErr == nil {\n\t\t\t\tconfiguredBase = filepath.Clean(absolute)\n\t\t\t}\n\t\t\tif relativeBase, relErr := filepath.Rel(configuredBase, base); relErr == nil && pathWithin(configuredBase, base) {\n\t\t\t\tbase = filepath.Join(workspace, relativeBase)\n\t\t\t}\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
    '''\trawWorkspace := strings.TrimSpace(s.workspace)\n\tif s.tasks != nil {\n\t\trawWorkspace = strings.TrimSpace(s.tasks.ActiveWorkspace(rawWorkspace))\n\t}\n\tif rawWorkspace != "" {\n\t\tif absolute, absErr := filepath.Abs(rawWorkspace); absErr == nil {\n\t\t\trawWorkspace = filepath.Clean(absolute)\n\t\t}\n\t}\n\tprojectToCanonical := func(candidate string) string {\n\t\tcandidate = filepath.Clean(candidate)\n\t\tif rawWorkspace == "" {\n\t\t\treturn candidate\n\t\t}\n\t\trel, relErr := filepath.Rel(rawWorkspace, candidate)\n\t\tif relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {\n\t\t\treturn filepath.Join(workspace, rel)\n\t\t}\n\t\treturn candidate\n\t}\n\tif filepath.IsAbs(value) {\n\t\ttarget = projectToCanonical(value)\n\t} else {\n\t\tbase := projectToCanonical(s.currentDefaultCWD())\n\t\ttarget = filepath.Join(base, value)\n\t}\n''',
)

print("v0.13 fix3 applied")
