package mcpcore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectStructuredChecks(t *testing.T) {
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.test/demo\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(goDir, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	goCheck, err := detectStructuredCheck(goDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if goCheck.Runtime != "go" || goCheck.Command != "go" || !reflect.DeepEqual(goCheck.Args, []string{"test", "-mod=vendor", "./..."}) {
		t.Fatalf("Go check = %#v", goCheck)
	}

	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(`{"scripts":{"typecheck":"vue-tsc -b","build":"vite build"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	nodeCheck, err := detectStructuredCheck(nodeDir, "check")
	if err != nil {
		t.Fatal(err)
	}
	if nodeCheck.Runtime != "node" || nodeCheck.Command != "npm" || !reflect.DeepEqual(nodeCheck.Args, []string{"run", "typecheck"}) {
		t.Fatalf("Node check = %#v", nodeCheck)
	}
}
