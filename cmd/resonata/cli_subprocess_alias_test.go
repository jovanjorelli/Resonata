package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func renderVia(t *testing.T, bin string, scoreFlag, score, outFlag, out string) {
	t.Helper()
	cmd := exec.Command(bin, scoreFlag+"="+score, outFlag+"="+out, "--reverb=none")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s/%s: %v\n%s", scoreFlag, outFlag, err, b)
	}
}

func sha256File(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("output missing/empty: %v", err)
	}
	return sha256.Sum256(data)
}

func TestCLILegacyLoadAlias(t *testing.T) {
	bin := buildResonataBinary(t)
	dir := t.TempDir()
	score := writeMinimalScoreFile(t, dir, "s.json")
	modern := filepath.Join(dir, "modern.wav")
	legacy := filepath.Join(dir, "legacy.wav")
	renderVia(t, bin, "--score", score, "--output", modern)
	renderVia(t, bin, "--load", score, "--output", legacy)
	if sha256File(t, modern) != sha256File(t, legacy) {
		t.Fatal("--load output differs from --score output")
	}
}

func TestCLILegacyOutAlias(t *testing.T) {
	bin := buildResonataBinary(t)
	dir := t.TempDir()
	score := writeMinimalScoreFile(t, dir, "s.json")
	modern := filepath.Join(dir, "modern.wav")
	legacy := filepath.Join(dir, "legacy.wav")
	renderVia(t, bin, "--score", score, "--output", modern)
	renderVia(t, bin, "--score", score, "--out", legacy)
	if sha256File(t, modern) != sha256File(t, legacy) {
		t.Fatal("--out output differs from --output output")
	}
}
