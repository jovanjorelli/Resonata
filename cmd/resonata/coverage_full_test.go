package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"syscall"
	"testing"
)

// TestMainExecutions verifies main entrypoint execution paths across diverse flag combinations.
func TestMainExecutions(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{"resonata", "--version"}
	main()

	batchDir := t.TempDir()
	writePushScore(t, batchDir, "a.json")
	outDir := t.TempDir()
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{"resonata", "--batch-dir=" + batchDir, "--output=" + outDir}
	main()

	dir := t.TempDir()
	scoreFile := writePushScore(t, dir, "score.json")
	outWav := filepath.Join(dir, "out.wav")
	cpuProf := filepath.Join(dir, "cpu.prof")
	memProf := filepath.Join(dir, "mem.prof")
	midExport := filepath.Join(dir, "out.mid")
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{
		"resonata",
		"-s", scoreFile,
		"-o", outWav,
		"--profile-cpu=" + cpuProf,
		"--profile-mem=" + memProf,
		"--humanize=0.1",
		"--benchmark",
		"--verbose",
		"--load-sfz=../../samples/piano.sfz",
		"--export-midi=" + midExport,
	}
	main()
}

// TestProfileCPUAlreadyActive verifies attempting to start active CPU profile returns error.
func TestProfileCPUAlreadyActive(t *testing.T) {
	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		t.Skip("cpu profile cannot start:", err)
	}
	defer pprof.StopCPUProfile()
	if err := startCPUProfile(filepath.Join(t.TempDir(), "second.prof")); err == nil {
		t.Fatal("expected error on duplicate CPU profile")
	}
}

// TestRenderToFileConfigErrors verifies configuration error validation before audio rendering starts.
func TestRenderToFileConfigErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.wav")
	baseCfg := func() *CLIConfig {
		return &CLIConfig{
			RenderMode:   "offline",
			BitDepth:     "16",
			Channels:     1,
			SampleRate:   44100,
			OutputPath:   out,
			ReverbPreset: "none",
			MasterGain:   1.0,
		}
	}

	cfgSat := baseCfg()
	cfgSat.Saturation = "invalid"
	if _, err := renderToFile(cfgSat, minimalScore(), nil); err == nil {
		t.Fatal("expected error for invalid saturation")
	}

	cfgDamp := baseCfg()
	cfgDamp.ReverbDamping = 2.0
	if _, err := renderToFile(cfgDamp, minimalScore(), nil); err == nil {
		t.Fatal("expected error for reverb damping > 1")
	}

	cfgGate := baseCfg()
	cfgGate.NoiseGate = 10.0
	if _, err := renderToFile(cfgGate, minimalScore(), nil); err == nil {
		t.Fatal("expected error for positive noise gate")
	}

	cfgMono := baseCfg()
	cfgMono.MonoBass = "invalid"
	if _, err := renderToFile(cfgMono, minimalScore(), nil); err == nil {
		t.Fatal("expected error for invalid mono bass")
	}

	cfgProg := baseCfg()
	cfgProg.OutputPath = filepath.Join(t.TempDir(), "p.wav")
	prog := make(chan float64)
	if _, err := renderToFile(cfgProg, minimalScore(), prog); err != nil {
		t.Fatal(err)
	}
}

func closeFDByPath(target string) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return
	}
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err == nil && link == target {
			if fd, err := strconv.Atoi(e.Name()); err == nil {
				syscall.Close(fd)
			}
		}
	}
}

// TestBatchRecovery verifies batch execution recovers gracefully from panics in individual score files.
func TestBatchRecovery(t *testing.T) {
	dir := t.TempDir()
	scoreFile := writePushScore(t, dir, "valid.json")
	if err := renderOneBatched(nil, scoreFile); err == nil {
		t.Fatal("expected panic recovery error")
	}
}

// TestRenderToFileWriteErrors verifies handling file creation and write failures during rendering.
func TestRenderToFileWriteErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "render_err.wav")
	cfg := &CLIConfig{
		RenderMode:   "offline",
		BitDepth:     "16",
		Channels:     1,
		SampleRate:   44100,
		OutputPath:   out,
		ReverbPreset: "none",
		MasterGain:   1.0,
	}

	prog := make(chan float64)
	go func() {
		<-prog
		closeFDByPath(out)
		for range prog {
		}
	}()

	s := minimalScore()
	s.Tracks[0].Notes[0].Duration = 1.0
	if _, err := renderToFile(cfg, s, prog); err == nil {
		t.Fatal("expected write error on closed fd")
	}
}
