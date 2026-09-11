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

func TestDetectValidationPlan(t *testing.T) {
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.test/validate\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(goDir, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := detectValidationPlan(goDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].Label != "Go tests" || plan[1].Label != "Go vet" || plan[2].Label != "Go build" {
		t.Fatalf("Go validation plan = %#v", plan)
	}

	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(`{"scripts":{"test":"vitest","lint":"eslint .","build":"vite build"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = detectValidationPlan(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].Args[1] != "test" || plan[1].Args[1] != "lint" || plan[2].Args[1] != "build" {
		t.Fatalf("Node validation plan = %#v", plan)
	}
}
