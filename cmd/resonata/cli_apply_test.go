package main

import (
	"strings"
	"testing"

	"resonata/pkg/engine"
)

// applyEngine builds a minimal engine for apply* tests.
func applyEngine(t *testing.T) *engine.Engine {
	t.Helper()
	eng, err := engine.New(minimalScore(), 48000, engine.DefaultBlockSize)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return eng
}

// TestApplySaturationModes proves every saturation value resolves or
// fails with the documented error.
func TestApplySaturationModes(t *testing.T) {
	for _, tc := range []struct {
		in      string
		bypass  bool
		wantErr string
	}{
		{"", false, ""},
		{"tape", false, ""},
		{"TAPE", false, ""},
		{" clean ", true, ""},
		{"vintage", false, `unknown --saturation "vintage" (want tape|clean)`},
	} {
		eng := applyEngine(t)
		err := applySaturation(eng, tc.in)
		if tc.wantErr == "" {
			if err != nil {
				t.Fatalf("%q: %v", tc.in, err)
			}
			if got := eng.Mixer().SaturationBypass(); got != tc.bypass {
				t.Fatalf("%q bypass = %v, want %v", tc.in, got, tc.bypass)
			}
		} else if err == nil || err.Error() != tc.wantErr {
			t.Fatalf("%q err = %v, want %q", tc.in, err, tc.wantErr)
		}
	}
}

// TestApplyReverbDampingModes proves preset-keep, override, and
// rejection paths with exact messages.
func TestApplyReverbDampingModes(t *testing.T) {
	eng := applyEngine(t)
	before := eng.Mixer().Reverb().Params().Damping
	if err := applyReverbDamping(eng, -1); err != nil {
		t.Fatalf("keep: %v", err)
	}
	if got := eng.Mixer().Reverb().Params().Damping; got != before {
		t.Fatalf("keep changed %v -> %v", before, got)
	}
	if err := applyReverbDamping(eng, 0); err != nil {
		t.Fatalf("zero: %v", err)
	}
	if got := eng.Mixer().Reverb().Params().Damping; got != 0 {
		t.Fatalf("damping = %v, want 0", got)
	}
	if err := applyReverbDamping(eng, 1); err != nil {
		t.Fatalf("one: %v", err)
	}
	if err := applyReverbDamping(eng, 2); err == nil ||
		!strings.Contains(err.Error(), "--reverb-damping") {
		t.Fatalf("range err = %v", err)
	}
}

// TestApplyNoiseGateModes proves disable, enable, and rejection paths.
func TestApplyNoiseGateModes(t *testing.T) {
	if err := applyNoiseGate(applyEngine(t), 0); err != nil {
		t.Fatalf("off: %v", err)
	}
	if err := applyNoiseGate(applyEngine(t), -60); err != nil {
		t.Fatalf("on: %v", err)
	}
	for _, bad := range []float64{5, 0.5} {
		if err := applyNoiseGate(applyEngine(t), bad); err == nil ||
			!strings.Contains(err.Error(), "--noise-gate") {
			t.Fatalf("%v err = %v", bad, err)
		}
	}
}

// TestApplyMonoBassModes proves off/Hz/error paths with exact messages.
func TestApplyMonoBassModes(t *testing.T) {
	eng := applyEngine(t)
	for _, off := range []string{"", "off", " OFF "} {
		if err := applyMonoBass(eng, off); err != nil {
			t.Fatalf("%q: %v", off, err)
		}
		if got := eng.Mixer().MonoBassCutoff(); got != 0 {
			t.Fatalf("%q cutoff = %v", off, got)
		}
	}
	if err := applyMonoBass(eng, "100"); err != nil {
		t.Fatalf("100: %v", err)
	}
	if got := eng.Mixer().MonoBassCutoff(); got != 100 {
		t.Fatalf("cutoff = %v, want 100", got)
	}
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"vintage", `unknown --mono-bass "vintage" (want off or cutoff Hz)`},
		{"1.5", `unknown --mono-bass "1.5" (want off or cutoff Hz)`},
		{"0", `unknown --mono-bass "0" (want off or cutoff Hz)`},
		{"-40", `unknown --mono-bass "-40" (want off or cutoff Hz)`},
	} {
		if err := applyMonoBass(eng, tc.in); err == nil || err.Error() != tc.want {
			t.Fatalf("%q err = %v, want %q", tc.in, err, tc.want)
		}
	}
}
