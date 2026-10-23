package sampler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resonata/pkg/wav"
)

// TestCaseInsensitiveResolve verifies resolving sample paths case-insensitively on case-sensitive file systems.
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

// TestCaseInsensitiveWalkForced verifies recursive directory tree scanning for case-insensitive path resolution.
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

// TestSanitizePath verifies path normalization stripping backslashes, double slashes, and quotes.
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

// TestIncludeCycleDetection verifies recursive #include expansion detects circular references and returns error.
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

// TestIncludeNested verifies multi-level nested #include file resolution and path rebasing.
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

// TestLoadSFZWindowsAuthored verifies loading Windows-authored SFZ libraries with backslashes on POSIX systems.
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

// BenchmarkResolveCache measures case-insensitive path resolution throughput and cache hit rate.
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

// TestPathResolverTraversal verifies path traversal attacks escaping library root are blocked.
func TestPathResolverTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	sampleDir := filepath.Join(tmpDir, "Samples")
	if err := os.MkdirAll(sampleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretFile := filepath.Join(tmpDir, "secret.wav")
	if err := os.WriteFile(secretFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	validSample := filepath.Join(sampleDir, "valid.wav")
	if err := os.WriteFile(validSample, []byte("valid"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := NewPathResolver(sampleDir)

	if got, err := resolver.Resolve("valid.wav"); err != nil || got != validSample {
		t.Fatalf("Resolve(valid.wav) = %q, %v; want %q", got, err, validSample)
	}

	traversalVectors := []string{
		"../secret.wav",
		"../../secret.wav",
		"../../../etc/passwd",
		"../../../../etc/passwd",
		"/etc/passwd",
		secretFile,
		"sub/../../secret.wav",
	}

	for _, vector := range traversalVectors {
		if got, err := resolver.Resolve(vector); err == nil {
			t.Errorf("Resolve(%q) = %q; expected traversal to be rejected", vector, got)
		}
	}

	caseInsensitiveVectors := []string{
		"../SECRET.WAV",
		"../../SECRET.WAV",
		"sub/../../SECRET.WAV",
	}
	for _, vector := range caseInsensitiveVectors {
		if got, err := resolver.resolveCaseInsensitive(vector); err == nil {
			t.Errorf("resolveCaseInsensitive(%q) = %q; expected traversal to be rejected", vector, got)
		}
	}
}

// TestSymlinkTraversal verifies symlinks resolving outside the root boundary are rejected.
func TestSymlinkTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "base")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outsideDir, "secret.wav")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	sym := filepath.Join(baseDir, "link.wav")
	if err := os.Symlink(secret, sym); err != nil {
		t.Skip("symlinks not supported")
	}
	resolver := NewPathResolver(baseDir)
	if _, err := resolver.Resolve("link.wav"); err == nil {
		t.Error("expected symlink escape to be rejected")
	}
}

// TestWindowsDriveSanitize verifies Windows drive letters are stripped during path sanitization.
func TestWindowsDriveSanitize(t *testing.T) {
	tmpDir := t.TempDir()
	sample := filepath.Join(tmpDir, "kick.wav")
	if err := os.WriteFile(sample, []byte("kick"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewPathResolver(tmpDir)
	got, err := resolver.Resolve(`C:\kick.wav`)
	if err != nil {
		t.Fatalf("Resolve with drive letter failed: %v", err)
	}
	if got != sample {
		t.Fatalf("Resolve = %q, want %q", got, sample)
	}
}

// TestForDirRootBase verifies child resolver preserves original root containment boundary.
func TestForDirRootBase(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(tmpDir, "shared.sfz")
	if err := os.WriteFile(shared, []byte("<region> sample=a.wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewPathResolver(tmpDir)
	subResolver := resolver.ForDir(subDir)
	got, err := subResolver.Resolve("../shared.sfz")
	if err != nil {
		t.Fatalf("Resolve from subdir to root parent failed: %v", err)
	}
	if got != shared {
		t.Fatalf("Resolve = %q, want %q", got, shared)
	}
	if _, err := subResolver.Resolve("../../outside.sfz"); err == nil {
		t.Error("expected escape above root to fail")
	}
}

// TestPathResolverSharedCacheCollision verifies shared path resolution cache isolation across distinct directory roots.
func TestPathResolverSharedCacheCollision(t *testing.T) {
	tmpDir := t.TempDir()
	subA := filepath.Join(tmpDir, "SubA")
	subB := filepath.Join(tmpDir, "SubB")
	if err := os.MkdirAll(subA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subB, 0o755); err != nil {
		t.Fatal(err)
	}
	fileA := filepath.Join(subA, "Sample.wav")
	fileB := filepath.Join(subB, "Sample.wav")
	if err := os.WriteFile(fileA, []byte("AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := NewPathResolver(tmpDir)
	resA := resolver.ForDir(subA)
	resB := resolver.ForDir(subB)
	gotA, err := resA.resolveCaseInsensitive("sample.wav")
	if err != nil {
		t.Fatalf("resA error: %v", err)
	}
	if gotA != fileA {
		t.Fatalf("resA = %q, want %q", gotA, fileA)
	}
	gotB, err := resB.resolveCaseInsensitive("sample.wav")
	if err != nil {
		t.Fatalf("resB error: %v", err)
	}
	if gotB != fileB {
		t.Fatalf("resB = %q, want %q (got cached file from subA instead of subB)", gotB, fileB)
	}
}

// TestIsWithinBaseRelativeTarget verifies relative target paths stay contained within base directory.
func TestIsWithinBaseRelativeTarget(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !isWithinBase(tmpDir, "sub/nonexistent.wav") {
		t.Fatal("expected relative target within base to return true")
	}
	if isWithinBase(tmpDir, "../../outside.wav") {
		t.Fatal("expected relative target escaping base to return false")
	}
}
