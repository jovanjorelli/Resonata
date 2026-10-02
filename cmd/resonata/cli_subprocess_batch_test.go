package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBatchMixedSubprocess(t *testing.T) {
	bin := buildResonataBinary(t)
	in := t.TempDir()
	out := t.TempDir()
	writeMinimalScoreFile(t, in, "good1.json")
	writeMinimalScoreFile(t, in, "good2.json")
	if err := os.WriteFile(filepath.Join(in, "broken.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--batch-dir="+in, "--output="+out, "--reverb=none",
		"--bit-depth=16", "--channels=1", "--sample-rate=44100")
	b, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("mixed batch exited 0:\n%s", b)
	}
	combined := string(b)
	if !strings.Contains(combined, "2 ok") || !strings.Contains(combined, "1 failed") {
		t.Fatalf("counts missing:\n%s", combined)
	}
	if !strings.Contains(combined, "broken.json") {
		t.Fatalf("invalid file not named:\n%s", combined)
	}
	for _, n := range []string{"good1.wav", "good2.wav"} {
		info, statErr := os.Stat(filepath.Join(out, n))
		if statErr != nil || info.Size() <= 44 {
			t.Fatalf("%s missing/small: %+v %v", n, info, statErr)
		}
	}
}
