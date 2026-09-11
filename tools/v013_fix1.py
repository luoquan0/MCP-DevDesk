from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def read(path):
    return (ROOT / path).read_text(encoding="utf-8")

def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8", newline="\n")

def replace_once(path, old, new):
    text = read(path)
    if old not in text:
        raise RuntimeError(f"missing marker in {path}: {old!r}")
    write(path, text.replace(old, new, 1))

# Go 1.26 / Windows runners may canonicalize the workspace temp path from an 8.3
# representation while the initial defaultCWD still contains the original spelling.
# Resolve the initial relative base against the canonical workspace root so equivalent
# Windows paths never look like an escape merely because of path spelling.
replace_once(
    "app/internal/mcpcore/file_tools.go",
    "\t} else {\n\t\ttarget = filepath.Join(s.currentDefaultCWD(), value)\n\t}\n",
    "\t} else {\n\t\tbase := s.currentDefaultCWD()\n\t\tif sameFilesystemPath(filepath.Clean(base), filepath.Clean(s.workspace)) {\n\t\t\tbase = workspace\n\t\t}\n\t\ttarget = filepath.Join(base, value)\n\t}\n",
)

# The preview intentionally adds 7 model-facing tools:
# agent_task, validate_project, agent_workflow, four code-navigation tools,
# plus Screen Vision compatibility probe while the screen tools remain gated.
server_test = read("app/internal/mcpcore/server_test.go")
server_test = server_test.replace("len(listResult.Result.Tools) != 33", "len(listResult.Result.Tools) != 40")
server_test = server_test.replace("len(tools) != 33", "len(tools) != 40")
write("app/internal/mcpcore/server_test.go", server_test)

print("v0.13 fix1 applied")
