from pathlib import Path

p = Path("app/internal/mcpcore/command_platform_windows.go")
text = p.read_text(encoding="utf-8")
old = '''func terminateCommand(cmd *exec.Cmd) error {
\tif cmd == nil || cmd.Process == nil {
\t\treturn nil
\t}
\ttaskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
\ttaskkill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
\tif err := taskkill.Run(); err != nil {
\t\tif killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
\t\t\treturn fmt.Errorf("terminate process tree: taskkill failed: %v; fallback kill: %w", err, killErr)
\t\t}
\t}
\treturn nil
}
'''
new = '''func terminateCommand(cmd *exec.Cmd) error {
\tif cmd == nil || cmd.Process == nil {
\t\treturn nil
\t}
\tif cmd.ProcessState != nil && cmd.ProcessState.Exited() {
\t\treturn nil
\t}
\ttaskkill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
\ttaskkill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
\tif err := taskkill.Run(); err != nil {
\t\tif cmd.ProcessState != nil && cmd.ProcessState.Exited() {
\t\t\treturn nil
\t\t}
\t\tif killErr := cmd.Process.Kill(); killErr != nil {
\t\t\tif errors.Is(killErr, os.ErrProcessDone) || errors.Is(killErr, syscall.EINVAL) {
\t\t\t\treturn nil
\t\t\t}
\t\t\treturn fmt.Errorf("terminate process tree: taskkill failed: %v; fallback kill: %w", err, killErr)
\t\t}
\t}
\treturn nil
}
'''
if old not in text:
    raise SystemExit("expected beta6 terminateCommand body not found")
p.write_text(text.replace(old, new, 1), encoding="utf-8")
