package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileWriterKeepsNewestOneHundredEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	writer, err := NewFileWriter(path, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 140; index++ {
		if _, err := fmt.Fprintf(writer, "line-%03d\n", index); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != MaxEntries {
		t.Fatalf("retained %d entries, want %d", len(lines), MaxEntries)
	}
	if lines[0] != "line-041" || lines[len(lines)-1] != "line-140" {
		t.Fatalf("unexpected retained range: %q ... %q", lines[0], lines[len(lines)-1])
	}
}

func TestFileWriterDiscardsWhenDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disabled.log")
	enabled := false
	writer, err := NewFileWriter(path, func() bool { return enabled })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("hidden\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled writer created a log file: %v", err)
	}
	enabled = true
	if _, err := writer.Write([]byte("visible\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "visible\n" {
		t.Fatalf("unexpected content %q", raw)
	}
}

func TestAppendLineTrimsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	var initial strings.Builder
	for index := 0; index < 110; index++ {
		fmt.Fprintf(&initial, "record-%03d\n", index)
	}
	if err := os.WriteFile(path, []byte(initial.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(path, []byte("record-110")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != MaxEntries || lines[0] != "record-011" || lines[99] != "record-110" {
		t.Fatalf("unexpected retained audit entries: count=%d first=%q last=%q", len(lines), lines[0], lines[len(lines)-1])
	}
}
