//go:build windows

package process

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func FindTCPListener(port int) (PortOwner, error) {
	command := exec.Command("netstat.exe", "-ano", "-p", "TCP")
	configureChildProcess(command, true)
	output, err := command.CombinedOutput()
	if err != nil {
		return PortOwner{}, fmt.Errorf("query TCP listeners: %w", err)
	}
	portSuffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") || !strings.HasSuffix(fields[1], portSuffix) {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[4])
		if parseErr != nil || pid <= 0 {
			continue
		}
		owner := PortOwner{Occupied: true, PID: pid}
		populateProcessDetails(&owner)
		return owner, nil
	}
	return PortOwner{}, nil
}

func KillPortOwner(owner PortOwner) error {
	if !owner.Occupied || owner.PID <= 0 {
		return nil
	}
	pid := owner.PID
	if owner.ParentPID > 0 && strings.EqualFold(owner.ProcessName, "coding-tools-mcp.exe") {
		parent := processDetails(owner.ParentPID)
		if strings.EqualFold(parent.ProcessName, owner.ProcessName) {
			pid = owner.ParentPID
		}
	}
	cmd := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	configureChildProcess(cmd, true)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("terminate conflicting process PID %d: %w: %s", pid, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func populateProcessDetails(owner *PortOwner) {
	details := processDetails(owner.PID)
	owner.ParentPID = details.ParentPID
	owner.ProcessName = details.ProcessName
	owner.ProcessPath = details.ProcessPath
}

func processDetails(pid int) PortOwner {
	owner := PortOwner{PID: pid}
	query := fmt.Sprintf("ProcessId=%d", pid)
	command := exec.Command("wmic.exe", "process", "where", query, "get", "Name,ExecutablePath,ParentProcessId", "/format:list")
	configureChildProcess(command, true)
	output, err := command.CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			switch strings.TrimSpace(key) {
			case "Name":
				owner.ProcessName = strings.TrimSpace(value)
			case "ExecutablePath":
				owner.ProcessPath = strings.TrimSpace(value)
			case "ParentProcessId":
				owner.ParentPID, _ = strconv.Atoi(strings.TrimSpace(value))
			}
		}
	}
	if owner.ProcessName == "" {
		command = exec.Command("tasklist.exe", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH")
		configureChildProcess(command, true)
		output, err = command.CombinedOutput()
		if err == nil {
			reader := csv.NewReader(bytes.NewReader(output))
			if row, readErr := reader.Read(); readErr == nil && len(row) > 0 {
				owner.ProcessName = strings.TrimSpace(row[0])
			}
		}
	}
	return owner
}
