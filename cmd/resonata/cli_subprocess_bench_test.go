package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBenchmarkFlag(t *testing.T) {
	bin := buildResonataBinary(t)
	dir := t.TempDir()
	score := writeMinimalScoreFile(t, dir, "s.json")
	out := filepath.Join(dir, "o.wav")
	cmd := exec.Command(bin, "--score="+score, "--output="+out, "--reverb=none", "--benchmark")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--benchmark: %v\n%s", err, b)
	}
	if !strings.Contains(string(b), "benchmark:") || !strings.Contains(string(b), "avg") {
		t.Fatalf("stdout missing timing line:\n%s", b)
	}
	if info, err := os.Stat(out); err != nil || info.Size() <= 44 {
		t.Fatalf("benchmark WAV missing/small: %+v %v", info, err)
	}
}

func TestCLIProfileFlags(t *testing.T) {
	bin := buildResonataBinary(t)
	dir := t.TempDir()
	score := writeMinimalScoreFile(t, dir, "s.json")
	cpu := filepath.Join(dir, "cpu.prof")
	mem := filepath.Join(dir, "mem.prof")
	cmd := exec.Command(bin, "--score="+score,
		"--output="+filepath.Join(dir, "o.wav"), "--reverb=none",
		"--profile-cpu="+cpu, "--profile-mem="+mem)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("profiles: %v\n%s", err, b)
	}
	for _, p := range []string{cpu, mem} {
		info, err := os.Stat(p)
		if err != nil || info.Size() == 0 {
			t.Fatalf("profile %s missing/empty: %+v %v", p, info, err)
		}
	}
}
