package sampler

import (
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/wav"
)

// TestGapLoadFileSuccess proves the one-call LoadFile path parses an SFZ
// file and returns a sampler with regions bound.
func TestGapLoadFileSuccess(t *testing.T) {
	dir := t.TempDir()
	writeTestWav(t, filepath.Join(dir, "c4.wav"), wav.PCM24, 48000, 2000, 1, 261.63)
	sfzPath := filepath.Join(dir, "gap.sfz")
	if err := os.WriteFile(sfzPath, []byte("<region> sample=c4.wav lokey=60 hikey=72 pitch_keycenter=60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadFile(sfzPath, 48000)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := len(s.Regions()); got != 1 {
		t.Fatalf("Regions() = %d, want 1", got)
	}
}

// TestGapLoadFileErrors proves missing and corrupt inputs fail gracefully.
func TestGapLoadFileErrors(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.sfz"), 48000); err == nil {
		t.Fatal("LoadFile(missing) = nil, want error")
	}
	bad := filepath.Join(t.TempDir(), "bad.sfz")
	if err := os.WriteFile(bad, []byte("sample=nope.wav lokey=60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(bad, 48000); err == nil {
		t.Fatal("LoadFile(bad sample) = nil, want error")
	}
}

// TestGapRegionsNilBeforeLoad proves Regions is nil on a fresh sampler.
func TestGapRegionsNilBeforeLoad(t *testing.T) {
	s := New(48000)
	if s.Regions() != nil {
		t.Fatalf("Regions() = %d regions, want nil before Load", len(s.Regions()))
	}
}

// TestGapSetRandSeed proves seed 0 restores the default stream and that
// reseeding is reproducible (same seed => same draws).
func TestGapSetRandSeed(t *testing.T) {
	s := New(48000)
	s.SetRandSeed(0)
	if s.rand != defaultRandSeed {
		t.Fatalf("rand after SetRandSeed(0) = %x, want default %x", s.rand, defaultRandSeed)
	}
	s.SetRandSeed(12345)
	a1, a2 := s.nextRand(), s.nextRand()
	s.SetRandSeed(12345)
	b1, b2 := s.nextRand(), s.nextRand()
	if a1 != b1 || a2 != b2 {
		t.Fatalf("reseed not reproducible: (%v,%v) vs (%v,%v)", a1, a2, b1, b2)
	}
	if a1 < 0 || a1 >= 1 || a2 < 0 || a2 >= 1 {
		t.Fatalf("draws out of [0,1): %v %v", a1, a2)
	}
}
