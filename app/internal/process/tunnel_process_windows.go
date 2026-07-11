//go:build windows

package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"mcp-devdesk/internal/model"
)

func ListCloudflaredProcesses() ([]model.TunnelProcess, error) {
	processes, wmicErr := listCloudflaredWithWMIC()
	if wmicErr == nil {
		sort.Slice(processes, func(left, right int) bool { return processes[left].PID < processes[right].PID })
		return processes, nil
	}
	processes, powershellErr := listCloudflaredWithPowerShell()
	if powershellErr != nil {
		return nil, fmt.Errorf("list cloudflared processes: WMIC: %v; PowerShell: %w", wmicErr, powershellErr)
	}
	sort.Slice(processes, func(left, right int) bool { return processes[left].PID < processes[right].PID })
	return processes, nil
}

func listCloudflaredWithWMIC() ([]model.TunnelProcess, error) {
	command := exec.Command(
		"wmic.exe",
		"process",
		"where",
		"Name='cloudflared.exe'",
		"get",
		"CommandLine,ExecutablePath,ParentProcessId,ProcessId",
		"/format:list",
	)
	configureChildProcess(command, true)
	output, err := command.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" || strings.Contains(strings.ToLower(text), "no instance") {
			return []model.TunnelProcess{}, nil
		}
		return nil, fmt.Errorf("%w: %s", err, text)
	}
	return parseWMICCloudflaredList(string(output)), nil
}

func listCloudflaredWithPowerShell() ([]model.TunnelProcess, error) {
	const script = `Get-CimInstance Win32_Process -Filter "Name='cloudflared.exe'" | Select-Object ProcessId,ParentProcessId,ExecutablePath,CommandLine | ConvertTo-Json -Compress`
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	configureChildProcess(command, true)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" || raw == "null" {
		return []model.TunnelProcess{}, nil
	}
	type cimProcess struct {
		ProcessID       int    `json:"ProcessId"`
		ParentProcessID int    `json:"ParentProcessId"`
		ExecutablePath  string `json:"ExecutablePath"`
		CommandLine     string `json:"CommandLine"`
	}
	var values []cimProcess
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("decode PowerShell process list: %w", err)
		}
	} else {
		var value cimProcess
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("decode PowerShell process: %w", err)
		}
		values = append(values, value)
	}
	processes := make([]model.TunnelProcess, 0, len(values))
	for _, value := range values {
		if value.ProcessID <= 0 || !isCloudflaredTunnelCommand(value.CommandLine) {
			continue
		}
		process := parseCloudflaredCommandLine(value.CommandLine)
		process.PID = value.ProcessID
		process.ParentPID = value.ParentProcessID
		if value.ExecutablePath != "" {
			process.ProcessPath = value.ExecutablePath
		}
		processes = append(processes, process)
	}
	return processes, nil
}

func StopCloudflaredProcess(pid int) error {
	if pid <= 0 {
		return errors.New("invalid cloudflared PID")
	}
	processes, err := ListCloudflaredProcesses()
	if err != nil {
		return err
	}
	found := false
	for _, process := range processes {
		if process.PID == pid {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("cloudflared PID %d is no longer running", pid)
	}

	command := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	configureChildProcess(command, true)
	if output, err := command.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(output))
		lower := strings.ToLower(text)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "no running instance") {
			return nil
		}
		return fmt.Errorf("stop cloudflared PID %d: %w: %s", pid, err, text)
	}
	return nil
}

func parseWMICCloudflaredList(output string) []model.TunnelProcess {
	var processes []model.TunnelProcess
	current := model.TunnelProcess{}
	flush := func() {
		if current.PID <= 0 {
			current = model.TunnelProcess{}
			return
		}
		if !isCloudflaredTunnelCommand(current.CommandLine) {
			current = model.TunnelProcess{}
			return
		}
		parsed := parseCloudflaredCommandLine(current.CommandLine)
		parsed.PID = current.PID
		parsed.ParentPID = current.ParentPID
		if current.ProcessPath != "" {
			parsed.ProcessPath = current.ProcessPath
		}
		processes = append(processes, parsed)
		current = model.TunnelProcess{}
	}

	for _, rawLine := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			flush()
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "CommandLine":
			current.CommandLine = value
		case "ExecutablePath":
			current.ProcessPath = value
		case "ParentProcessId":
			current.ParentPID, _ = strconv.Atoi(value)
		case "ProcessId":
			current.PID, _ = strconv.Atoi(value)
		}
	}
	flush()
	return processes
}
