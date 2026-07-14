//go:build windows

package process

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const processDetailsCacheTTL = 15 * time.Second

var processDetailsCache = struct {
	sync.Mutex
	entries map[int]cachedProcessDetails
}{entries: make(map[int]cachedProcessDetails)}

type cachedProcessDetails struct {
	owner     PortOwner
	expiresAt time.Time
}

func FindTCPListener(port int) (PortOwner, error) {
	owners, err := FindTCPListeners([]int{port})
	if err != nil {
		return PortOwner{}, err
	}
	return owners[port], nil
}

func FindTCPListeners(ports []int) (map[int]PortOwner, error) {
	requested := make(map[int]struct{}, len(ports))
	result := make(map[int]PortOwner, len(ports))
	for _, port := range ports {
		if port > 0 && port <= 65535 {
			requested[port] = struct{}{}
		}
		result[port] = PortOwner{}
	}
	if len(requested) == 0 {
		return result, nil
	}
	command := exec.Command("netstat.exe", "-ano", "-p", "TCP")
	configureChildProcess(command, true)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("query TCP listeners: %w", err)
	}
	pids := make(map[int]int, len(requested))
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		separator := strings.LastIndexByte(fields[1], ':')
		if separator < 0 || separator == len(fields[1])-1 {
			continue
		}
		port, portErr := strconv.Atoi(fields[1][separator+1:])
		if portErr != nil {
			continue
		}
		if _, wanted := requested[port]; !wanted {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[4])
		if parseErr != nil || pid <= 0 {
			continue
		}
		if _, exists := pids[port]; !exists {
			pids[port] = pid
		}
	}
	detailsByPID := make(map[int]PortOwner, len(pids))
	for port, pid := range pids {
		details, exists := detailsByPID[pid]
		if !exists {
			details = processDetails(pid)
			detailsByPID[pid] = details
		}
		details.Occupied = true
		details.PID = pid
		result[port] = details
	}
	return result, nil
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
	processDetailsCache.Lock()
	if cached, ok := processDetailsCache.entries[pid]; ok && time.Now().Before(cached.expiresAt) {
		processDetailsCache.Unlock()
		return cached.owner
	}
	processDetailsCache.Unlock()

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
	processDetailsCache.Lock()
	if len(processDetailsCache.entries) >= 256 {
		now := time.Now()
		for cachedPID, cached := range processDetailsCache.entries {
			if now.After(cached.expiresAt) {
				delete(processDetailsCache.entries, cachedPID)
			}
		}
		for len(processDetailsCache.entries) >= 256 {
			oldestPID := 0
			var oldest time.Time
			for cachedPID, cached := range processDetailsCache.entries {
				if oldestPID == 0 || cached.expiresAt.Before(oldest) {
					oldestPID = cachedPID
					oldest = cached.expiresAt
				}
			}
			if oldestPID == 0 {
				break
			}
			delete(processDetailsCache.entries, oldestPID)
		}
	}
	processDetailsCache.entries[pid] = cachedProcessDetails{owner: owner, expiresAt: time.Now().Add(processDetailsCacheTTL)}
	processDetailsCache.Unlock()
	return owner
}
