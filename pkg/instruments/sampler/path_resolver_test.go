package sampler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resonata/pkg/wav"
)

func TestCaseInsensitiveResolve(t *testing.T) {
	// Fixture with mixed case.
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "Grand Piano"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmpDir, "Grand Piano", "C4 Loud.wav")
	if err := os.WriteFile(want, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := NewPathResolver(tmpDir)

	tests := []struct {
		input string
	}{
		// Exact match.
		{"Grand Piano/C4 Loud.wav"},
		// All lowercase (case-insensitive fallback on Linux).
		{"grand piano/c4 loud.wav"},
		// Windows backslashes.
		{`Grand Piano\C4 Loud.wav`},
		// Quoted path with spaces.
		{`"Grand Piano/C4 Loud.wav"`},
		// Mixed separators and case.
		{`grand piano\C4 LOUD.wav`},
	}

	for _, tt := range tests {
		got, err := resolver.Resolve(tt.input)
		if err != nil {
			t.Errorf("Resolve(%q) error: %v", tt.input, err)
			continue
		}
		// Compare file identity: on Windows the filesystem is
		// case-insensitive, so the fast path returns the request
		// spelling while still opening the same file.
		gotInfo, gerr := os.Stat(got)
		wantInfo, werr := os.Stat(want)
		if gerr != nil || werr != nil || !os.SameFile(gotInfo, wantInfo) {
			t.Errorf("Resolve(%q) = %q, want the same file as %q", tt.input, got, want)
		}
	}

	// Missing files report a PathError naming the request.
	if _, err := resolver.Resolve("grand piano/missing.wav"); err == nil {
		t.Error("missing file resolved without error")
	} else if !strings.Contains(err.Error(), "missing.wav") {
		t.Errorf("missing file error = %v, want the requested name", err)
	}
}

// TestCaseInsensitiveWalkForced exercises the Linux fallback walk on any
// platform by calling it directly (the fast Stat path always succeeds on
// Windows, even for case variants). The walk rebuilds the true on-disk
// casing, so exact string equality holds here.
func TestCaseInsensitiveWalkForced(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "Grand Piano"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmpDir, "Grand Piano", "C4 Loud.wav")
	if err := os.WriteFile(want, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewPathResolver(tmpDir)
	got, err := resolver.resolveCaseInsensitive("grand piano/c4 loud.wav")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("walk Resolve = %q, want %q", got, want)
	}
	// Second resolution hits the cache.
	if _, err := resolver.resolveCaseInsensitive("GRAND PIANO/C4 LOUD.WAV"); err != nil {
		t.Fatal(err)
	}
	if resolver.Hits() < 1 {
		t.Errorf("cache hits = %d, want >= 1", resolver.Hits())
	}
}

func TestSanitizePath(t *testing.T) {
	cases := map[string]string{
		`"Grand Piano\c4 loud.wav"`: "Grand Piano/c4 loud.wav",
		`'a\b.wav'`:                 "a/b.wav",
		`a\\b\\\\c.wav`:             "a/b/c.wav",
		`  spaced.wav  `:            "spaced.wav",
		`plain.wav`:                 "plain.wav",
	}
	for in, want := range cases {
		if got := SanitizePath(in); got != want {
			t.Errorf("SanitizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIncludeCycleDetection(t *testing.T) {
	tmpDir := t.TempDir()

	// Cycle: a.sfz includes b.sfz includes a.sfz.
	if err := os.WriteFile(filepath.Join(tmpDir, "a.sfz"),
		[]byte(" sample=a.wav\n"+`#include "b.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.sfz"),
		[]byte(" sample=b.wav\n"+`#include "a.sfz"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := NewPathResolver(tmpDir)
	expander := NewIncludeExpander(resolver)

	_, err := expander.Expand(filepath.Join(tmpDir, "a.sfz"))
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle detection error, got: %v", err)
	}
}

func TestIncludeNested(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "Strings")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Windows-authored main file: backslashes, mixed case include.
	main := "<group>\nvolume=-3\n" + "#include \"strings\\EXTRA.sfz\"\n" +
		"<region>\nsample=a.wav lokey=60\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "main.sfz"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "extra.sfz"),
		[]byte("<region>\nsample=b.wav lokey=61\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := NewPathResolver(tmpDir)
	out, err := NewIncludeExpander(resolver).Expand(filepath.Join(tmpDir, "main.sfz"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "sample=b.wav") || !strings.Contains(text, "sample=a.wav") {
		t.Errorf("nested include not expanded: %q", text)
	}

	// Diamond includes expand without false cycle errors.
	diamond := "#include \"main.sfz\"\n#include \"main.sfz\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "d.sfz"), []byte(diamond), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewIncludeExpander(NewPathResolver(tmpDir)).Expand(filepath.Join(tmpDir, "d.sfz")); err != nil {
		t.Errorf("diamond include failed: %v", err)
	}
}

func TestLoadSFZWindowsAuthored(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Samples", "Grand Piano")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// On-disk exact case; the SFZ references it lowercase with
	// backslashes, exercising sanitize plus case fallback.
	writeTestWav(t, filepath.Join(sub, "C4 Loud.wav"), wav.PCM24, 48000, 500, 1, 261.63)
	doc := "<control>\ndefault_path=Samples\\\n<group>\nvolume=-3\n" +
		"<region>\nsample=\"grand piano\\c4 loud.wav\" lokey=60 hikey=64 pitch_keycenter=60\n"
	sfzPath := filepath.Join(dir, "win.sfz")
	if err := os.WriteFile(sfzPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := LoadSFZ(sfzPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
	if f.Regions[0].Sample == nil || f.Regions[0].Sample.Frames() != 500 {
		t.Fatalf("sample not decoded: %+v", f.Regions[0].Sample)
	}
}

func BenchmarkResolveCache(b *testing.B) {
	dir := b.TempDir()
	sub := filepath.Join(dir, "Grand Piano")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "C4 Loud.wav"), []byte("data"), 0o644); err != nil {
		b.Fatal(err)
	}
	resolver := NewPathResolver(dir)
	// Exercise the cached walk directly: on Windows the fast Stat path
	// succeeds for any casing, bypassing the cache entirely.
	if _, err := resolver.resolveCaseInsensitive("grand piano/c4 loud.wav"); err != nil {
		b.Fatal(err)
	}
	hitsBefore := resolver.Hits()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := resolver.resolveCaseInsensitive("grand piano/c4 loud.wav"); err != nil {
			b.Fatal(err)
		}
	}
	hits := resolver.Hits() - hitsBefore
	rate := float64(hits) / float64(b.N)
	b.ReportMetric(rate*100, "cache-hit-%")
	if rate < 0.9 {
		b.Fatalf("cache hit rate = %.1f%%, want >= 90%%", rate*100)
	}
}
