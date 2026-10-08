package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Items 1-4 of the main() matrix (help table, version string, unknown
// flag, missing input file) are already covered by TestCLIHelpFlagTable,
// TestCLIVersionString, TestCLIUnknownFlag, and TestCLIMissingInputFile
// in cli_subprocess_help_test.go. The tests below close the remaining
// gap: end-to-end renders through every post-1.0 flag and rejection of
// invalid flag values, all through the real binary.

// r2Bin builds the CLI once per test run into a shared temp dir.
var (
	r2BinOnce sync.Once
	r2BinPath string
	r2BinRoot string
	r2BinErr  error
)

func r2Binary(t *testing.T) string {
	t.Helper()
	r2BinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "resonata-r2bin")
		if err != nil {
			r2BinErr = err
			return
		}
		root, err := os.Getwd()
		if err != nil {
			r2BinErr = err
			return
		}
		for {
			if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				r2BinErr = err
				return
			}
			root = parent
		}
		r2BinPath = filepath.Join(dir, "resonata")
		r2BinRoot = root
		cmd := exec.Command("go", "build", "-o", r2BinPath, "./cmd/resonata")
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			r2BinErr = err
			_ = b
			return
		}
	})
	if r2BinErr != nil {
		t.Fatalf("build shared binary: %v", r2BinErr)
	}
	return r2BinPath
}

// r2Render runs the shared binary on the bundled simple score with extra
// flags and returns the output WAV path.
func r2Render(t *testing.T, extra ...string) string {
	t.Helper()
	bin := r2Binary(t)
	out := filepath.Join(t.TempDir(), "out.wav")
	args := append([]string{"--score=examples/simple_score.json", "--output=" + out}, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = r2BinRoot
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render %v: %v\n%s", extra, err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil || len(raw) <= 44 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("render %v produced invalid wav (%d bytes, err %v)", extra, len(raw), err)
	}
	return out
}

// TestR2SubSaturationClean proves --saturation=clean renders valid audio.
func TestR2SubSaturationClean(t *testing.T) { r2Render(t, "--saturation=clean") }

// TestR2SubReverbDamping proves --reverb-damping=0.0 renders valid audio.
func TestR2SubReverbDamping(t *testing.T) { r2Render(t, "--reverb-damping=0.0") }

// TestR2SubNoiseGate proves --noise-gate=-60 renders valid audio.
func TestR2SubNoiseGate(t *testing.T) { r2Render(t, "--noise-gate=-60") }

// TestR2SubLimiter proves --limiter renders valid audio.
func TestR2SubLimiter(t *testing.T) { r2Render(t, "--limiter") }

// TestR2SubMonoBass proves --mono-bass=100 renders valid audio.
func TestR2SubMonoBass(t *testing.T) { r2Render(t, "--mono-bass=100") }

// TestR2SubParallel proves --parallel renders valid audio.
func TestR2SubParallel(t *testing.T) { r2Render(t, "--parallel") }

// TestR2SubBadSaturation proves an unknown saturation name is rejected.
func TestR2SubBadSaturation(t *testing.T) { r2Reject(t, "--saturation=bogus", "saturation") }

// TestR2SubBadMonoBass proves a non-numeric crossover is rejected.
func TestR2SubBadMonoBass(t *testing.T) { r2Reject(t, "--mono-bass=bogus", "mono-bass") }

// TestR2SubBadNoiseGate proves a positive gate threshold is rejected.
func TestR2SubBadNoiseGate(t *testing.T) { r2Reject(t, "--noise-gate=5", "noise-gate") }

// r2Reject runs the binary expecting a non-zero exit and a readable
// stderr naming the offending flag.
func r2Reject(t *testing.T, flag, want string) {
	t.Helper()
	bin := r2Binary(t)
	out := filepath.Join(t.TempDir(), "out.wav")
	cmd := exec.Command(bin, "--score=examples/simple_score.json", "--output="+out, flag)
	cmd.Dir = r2BinRoot
	b, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s accepted with exit 0", flag)
	}
	if !strings.Contains(string(b), want) {
		t.Fatalf("%s stderr missing %q:\n%s", flag, want, b)
	}
}
