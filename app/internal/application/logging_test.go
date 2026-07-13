package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsTrimHistoryToOneHundredEntries(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data", "devdesk")
	app, err := New(root, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	logPath := filepath.Join(dataDir, "logs", "manager.log")
	var content strings.Builder
	for index := 1; index <= 125; index++ {
		fmt.Fprintf(&content, "entry-%03d\n", index)
	}
	if err := os.WriteFile(logPath, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := app.Logs("manager", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 100 {
		t.Fatalf("returned %d entries, want 100", len(result.Lines))
	}
	if result.Lines[0] != "entry-026" || result.Lines[99] != "entry-125" {
		t.Fatalf("unexpected retained range: %q ... %q", result.Lines[0], result.Lines[99])
	}
}
