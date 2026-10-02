package main

import (
	"flag"
	"os"
	"testing"

	"resonata/pkg/engine"
)

// withCLIArgs swaps os.Args and the global flag set so parseFlags can
// run in-process without touching the real command line.
func withCLIArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	oldSet := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldSet
	})
}

func TestParseFlagsDefaults(t *testing.T) {
	withCLIArgs(t, "resonata")
	cfg := parseFlags()
	if cfg.ScorePath != "" || cfg.OutputPath != "" {
		t.Fatalf("paths = %q/%q, want empty", cfg.ScorePath, cfg.OutputPath)
	}
	if cfg.RenderMode != "offline" || cfg.SampleRate != engine.DefaultSampleRate {
		t.Fatalf("mode/rate = %q/%d", cfg.RenderMode, cfg.SampleRate)
	}
	if cfg.BitDepth != "24" || cfg.Channels != 2 {
		t.Fatalf("depth/ch = %q/%d", cfg.BitDepth, cfg.Channels)
	}
	if cfg.Humanize != 0 || cfg.ReverbPreset != "hall" || cfg.MasterGain != 1.0 {
		t.Fatalf("audio = %v/%q/%v", cfg.Humanize, cfg.ReverbPreset, cfg.MasterGain)
	}
	if cfg.Verbose || cfg.Benchmark || cfg.ShowVersion {
		t.Fatal("bools default true")
	}
}

func TestParseFlagsFull(t *testing.T) {
	withCLIArgs(t, "resonata",
		"--score=s.json", "--output=o.wav", "--mode=stream",
		"--sample-rate=44100", "--bit-depth=16", "--channels=1",
		"--humanize=0.5", "--reverb=cathedral", "--master-gain=0.9",
		"--import-midi=i.mid", "--export-midi=e.mid", "--export-json=j.json",
		"--profile-cpu=c.prof", "--profile-mem=m.prof",
		"--verbose", "--benchmark", "--batch-dir=.", "--load-sfz=l.sfz", "--version")
	cfg := parseFlags()
	if cfg.ScorePath != "s.json" || cfg.OutputPath != "o.wav" || cfg.RenderMode != "stream" {
		t.Fatalf("core = %+v", cfg)
	}
	if cfg.SampleRate != 44100 || cfg.BitDepth != "16" || cfg.Channels != 1 {
		t.Fatalf("format = %+v", cfg)
	}
	if cfg.Humanize != 0.5 || cfg.ReverbPreset != "cathedral" || cfg.MasterGain != 0.9 {
		t.Fatalf("audio = %+v", cfg)
	}
	if cfg.ImportMIDI != "i.mid" || cfg.ExportMIDI != "e.mid" || cfg.ExportJSON != "j.json" {
		t.Fatalf("midi = %+v", cfg)
	}
	if cfg.ProfileCPU != "c.prof" || cfg.ProfileMem != "m.prof" {
		t.Fatalf("profile = %+v", cfg)
	}
	if !cfg.Verbose || !cfg.Benchmark || cfg.BatchDir != "." || cfg.LoadSFZ != "l.sfz" || !cfg.ShowVersion {
		t.Fatalf("rest = %+v", cfg)
	}
}

func TestParseFlagsShorthands(t *testing.T) {
	withCLIArgs(t, "resonata", "-s=s.json", "-o=o.wav", "-v")
	cfg := parseFlags()
	if cfg.ScorePath != "s.json" || cfg.OutputPath != "o.wav" || !cfg.Verbose {
		t.Fatalf("shorthand = %+v", cfg)
	}
}

func TestParseFlagsLegacyAliases(t *testing.T) {
	withCLIArgs(t, "resonata", "--load=old.json", "--out=old.wav")
	cfg := parseFlags()
	if cfg.ScorePath != "old.json" || cfg.OutputPath != "old.wav" {
		t.Fatalf("legacy = %+v", cfg)
	}
	// Explicit flags win over legacy aliases.
	withCLIArgs(t, "resonata", "--score=new.json", "--load=old.json")
	cfg = parseFlags()
	if cfg.ScorePath != "new.json" {
		t.Fatalf("precedence = %+v", cfg)
	}
}
