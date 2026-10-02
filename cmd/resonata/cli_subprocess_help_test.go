package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildResonataBinary compiles the real CLI once per test into a temp
// dir and returns its path. Tests exec it with os/exec so flag parsing,
// exit codes, and stdio routing are exercised end to end.
func buildResonataBinary(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := dir
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("go.mod not found")
		}
		root = parent
	}
	out := filepath.Join(t.TempDir(), "resonata")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/resonata")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func TestCLIHelpFlagTable(t *testing.T) {
	bin := buildResonataBinary(t)
	cmd := exec.Command(bin, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help exit: %v\n%s", err, out)
	}
	// Go's flag package prints single-dash names in usage.
	for _, want := range []string{"-score", "-output", "-sample-rate", "-version", "-batch-dir"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestCLIVersionString(t *testing.T) {
	bin := buildResonataBinary(t)
	cmd := exec.Command(bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("--version exit: %v", err)
	}
	if strings.TrimSpace(string(out)) != "Resonata v"+Version {
		t.Fatalf("version = %q, want %q", out, "Resonata v"+Version)
	}
}

func TestCLIUnknownFlag(t *testing.T) {
	bin := buildResonataBinary(t)
	cmd := exec.Command(bin, "--bogus-flag")
	out := cmd.Run()
	if out == nil {
		t.Fatal("unknown flag accepted with exit 0")
	}
	if exit, ok := out.(*exec.ExitError); !ok || exit.ExitCode() == 0 {
		t.Fatalf("unknown flag exit = %v, want non-zero", out)
	}
	// Re-run capturing stderr for the readable message.
	cmd2 := exec.Command(bin, "--bogus-flag")
	b, _ := cmd2.CombinedOutput()
	if !strings.Contains(string(b), "not defined") {
		t.Fatalf("stderr not readable:\n%s", b)
	}
}

func TestCLIMissingInputFile(t *testing.T) {
	bin := buildResonataBinary(t)
	missing := filepath.Join(t.TempDir(), "nope.json")
	cmd := exec.Command(bin, "--score="+missing, "--output="+filepath.Join(t.TempDir(), "o.wav"))
	b, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("missing input accepted with exit 0")
	}
	if !strings.Contains(string(b), "nope.json") {
		t.Fatalf("stderr missing path:\n%s", b)
	}
}
