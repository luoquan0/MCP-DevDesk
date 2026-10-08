//go:build linux

package process

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLinuxTCPListenerOwnership(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	owner, err := FindTCPListener(port)
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Occupied || owner.PID != os.Getpid() || owner.ProcessPath == "" {
		t.Fatalf("wrong port owner: %+v", owner)
	}
	listener.Close()
	owner, err = FindTCPListener(port)
	if err != nil || owner.Occupied {
		t.Fatalf("closed listener remains occupied: %+v %v", owner, err)
	}
}

func TestLinuxManagedProcessGroupStop(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 60 & wait")
	configureChildProcess(cmd, false)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = stopManagedCommand(cmd) })
	if err := stopManagedCommand(cmd); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("managed process did not exit")
	}
	if err := stopManagedCommand(cmd); err != nil {
		t.Fatalf("repeated stop must be idempotent: %v", err)
	}
}
