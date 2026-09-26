//go:build linux

package process

import (
 "bufio"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
 "syscall"
 "time"

 "mcp-devdesk/internal/model"
)

func configureChildProcess(cmd *exec.Cmd, _ bool) {
 cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopManagedCommand(cmd *exec.Cmd) error {
 if cmd == nil || cmd.Process == nil { return nil }
 pid := cmd.Process.Pid
 if pid <= 1 { return errors.New("refusing invalid process group") }
 err := syscall.Kill(-pid, syscall.SIGTERM)
 if errors.Is(err, syscall.ESRCH) { return nil }; if err != nil { return err }
 deadline := time.Now().Add(3*time.Second)
 for time.Now().Before(deadline) {
  if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) { return nil }
  time.Sleep(50*time.Millisecond)
 }
 err = syscall.Kill(-pid, syscall.SIGKILL)
 if errors.Is(err, syscall.ESRCH) { return nil }; return err
}

func procIdentity(pid int) (PortOwner, error) {
 dir := filepath.Join("/proc", strconv.Itoa(pid))
 exe, err := os.Readlink(filepath.Join(dir, "exe")); if err != nil { return PortOwner{}, err }
 stat, err := os.ReadFile(filepath.Join(dir, "stat")); if err != nil { return PortOwner{}, err }
 end := strings.LastIndex(string(stat), ")")
 if end < 0 { return PortOwner{}, errors.New("invalid proc stat") }
 fields := strings.Fields(string(stat[end+1:]))
 if len(fields) < 2 { return PortOwner{}, errors.New("truncated proc stat") }
 parent, err := strconv.Atoi(fields[1]); if err != nil { return PortOwner{}, err }
 return PortOwner{Occupied:true, PID:pid, ParentPID:parent, ProcessPath:strings.TrimSuffix(exe," (deleted)"), ProcessName:filepath.Base(strings.TrimSuffix(exe," (deleted)"))}, nil
}

func FindTCPListener(port int) (PortOwner, error) {
 owners, err := FindTCPListeners([]int{port}); return owners[port], err
}

func FindTCPListeners(ports []int) (map[int]PortOwner, error) {
 result := make(map[int]PortOwner, len(ports)); wanted := make(map[int]bool, len(ports))
 for _, port := range ports { wanted[port] = true; result[port] = PortOwner{} }
 inodes := make(map[string]int)
 for _, name := range []string{"tcp", "tcp6"} {
  file, err := os.Open("/proc/net/"+name)
  if err != nil { if name == "tcp6" && os.IsNotExist(err) { continue }; return nil, err }
  scanner := bufio.NewScanner(file)
  for scanner.Scan() {
   fields := strings.Fields(scanner.Text())
   if len(fields) < 10 || fields[3] != "0A" { continue }
   local := strings.Split(fields[1], ":"); if len(local) != 2 { continue }
   port, err := strconv.ParseUint(local[1],16,16)
   if err != nil || !wanted[int(port)] { continue }
   result[int(port)] = PortOwner{Occupied:true}; inodes["socket:["+fields[9]+"]"] = int(port)
  }
  scanErr := scanner.Err(); _ = file.Close(); if scanErr != nil { return nil, scanErr }
 }
 if len(inodes) == 0 { return result,nil }
 entries, err := os.ReadDir("/proc"); if err != nil { return nil,err }
 for _, entry := range entries {
  pid, err := strconv.Atoi(entry.Name()); if err != nil || pid <= 0 { continue }
  fds, err := os.ReadDir(filepath.Join("/proc",entry.Name(),"fd")); if err != nil { continue }
  for _, fd := range fds {
   target, err := os.Readlink(filepath.Join("/proc",entry.Name(),"fd",fd.Name())); if err != nil { continue }
   port, found := inodes[target]; if !found { continue }
   identity, err := procIdentity(pid); if err == nil { result[port] = identity }
  }
 }
 // Occupied remains true even if procfs permissions hide another user's PID.
 return result,nil
}

func KillPortOwner(owner PortOwner) error {
 if !owner.Occupied || owner.PID <= 1 || owner.ProcessPath == "" { return errors.New("cannot safely identify port owner") }
 process, err := os.FindProcess(owner.PID); if err != nil { return err }; defer process.Release()
 current, err := procIdentity(owner.PID); if os.IsNotExist(err) { return nil }; if err != nil { return err }
 if current.ProcessPath != owner.ProcessPath { return errors.New("port owner identity changed; refusing to stop it") }
 err = process.Signal(syscall.SIGTERM)
 if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) { return nil }; return err
}

func ListCloudflaredProcesses() ([]model.TunnelProcess, error) {
 entries, err := os.ReadDir("/proc"); if err != nil { return nil,err }
 result := make([]model.TunnelProcess,0)
 for _, entry := range entries {
  pid, err := strconv.Atoi(entry.Name()); if err != nil || pid <= 1 { continue }
  identity, err := procIdentity(pid); if err != nil || identity.ProcessName != "cloudflared" { continue }
  raw, err := os.ReadFile(filepath.Join("/proc",entry.Name(),"cmdline")); if err != nil || len(raw) == 0 { continue }
  args := strings.Split(strings.TrimSuffix(string(raw),"\x00"),"\x00")
  tunnelRun := false
  for i := 1; i < len(args); i++ { if args[i-1] == "tunnel" && args[i] == "run" { tunnelRun = true } }
  if !tunnelRun { continue }
  item := parseCloudflaredArguments(args)
  item.PID = pid; item.ParentPID = identity.ParentPID; item.ProcessPath = identity.ProcessPath
  result = append(result,item)
 }
 sort.Slice(result,func(i,j int)bool{return result[i].PID<result[j].PID})
 return result,nil
}

func StopCloudflaredProcess(pid int) error {
 if pid <= 1 { return errors.New("invalid cloudflared PID") }
 process, err := os.FindProcess(pid); if err != nil { return err }; defer process.Release()
 items, err := ListCloudflaredProcesses(); if err != nil { return err }
 for _, item := range items {
  if item.PID != pid { continue }
  err := process.Signal(syscall.SIGTERM)
  if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) { return nil }; return err
 }
 return fmt.Errorf("PID %d is not an identifiable cloudflared tunnel",pid)
}
