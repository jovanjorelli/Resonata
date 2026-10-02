package score

import (
	"strings"
	"testing"
)

// transposeScore builds a parseable score with one track per pitch list.
// transpose[i] is the per-track transpose of tracks[i].
func transposeScore(global int, names, ids []string, transpose []int, pitches ...[]int) string {
	var b strings.Builder
	b.WriteString(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4", "transpose": `)
	b.WriteString(itoa(global))
	b.WriteString(`}, "tracks": [`)
	for i := range pitches {
		if i > 0 {
			b.WriteString(`, `)
		}
		b.WriteString(`{"id": "`)
		b.WriteString(ids[i])
		b.WriteString(`", "name": "`)
		b.WriteString(names[i])
		b.WriteString(`", "instrument": {"type": "ocarina"}, "pan": 0, "volume": 0.8, "transpose": `)
		b.WriteString(itoa(transpose[i]))
		b.WriteString(`, "notes": [`)
		for j, p := range pitches[i] {
			if j > 0 {
				b.WriteString(`, `)
			}
			b.WriteString(`{"time": 0, "duration": 0.5, "pitch": `)
			b.WriteString(itoa(p))
			b.WriteString(`, "velocity": 0.8}`)
		}
		b.WriteString(`]}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [8]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func mustParseJSON(t *testing.T, data string) *Score {
	t.Helper()
	s, err := ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return s
}

func pitchesOf(s *Score, track int) []int {
	var out []int
	for _, n := range s.Tracks[track].Notes {
		out = append(out, n.Pitch)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTransposeGlobalUp shifts every note up one octave via metadata.
func TestTransposeGlobalUp(t *testing.T) {
	s := mustParseJSON(t, transposeScore(12,
		[]string{"Violins"}, []string{"vln"}, []int{0}, []int{60, 64, 67}))
	if got, want := pitchesOf(s, 0), []int{72, 76, 79}; !equalInts(got, want) {
		t.Fatalf("pitches = %v, want %v", got, want)
	}
	if s.Tracks[0].Transpose != 0 {
		t.Fatalf("track transpose = %d, want 0", s.Tracks[0].Transpose)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid transposed score rejected: %v", err)
	}
}

// TestTransposeTrackDown shifts one track down one octave via the track.
func TestTransposeTrackDown(t *testing.T) {
	s := mustParseJSON(t, transposeScore(0,
		[]string{"Cellos"}, []string{"vlc"}, []int{-12}, []int{60, 64, 67}))
	if got, want := pitchesOf(s, 0), []int{48, 52, 55}; !equalInts(got, want) {
		t.Fatalf("pitches = %v, want %v", got, want)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid transposed score rejected: %v", err)
	}
}

// TestTransposeCombined adds global and track shifts: 60 + 2 - 5 = 57.
func TestTransposeCombined(t *testing.T) {
	s := mustParseJSON(t, transposeScore(2,
		[]string{"Violas"}, []string{"vla"}, []int{-5}, []int{60}))
	if got := pitchesOf(s, 0); !equalInts(got, []int{57}) {
		t.Fatalf("pitches = %v, want [57]", got)
	}
}

// TestTransposeMultipleTracks applies one global shift with per-track
// offsets: 60+1, 60+1+3, 60+1-2.
func TestTransposeMultipleTracks(t *testing.T) {
	s := mustParseJSON(t, transposeScore(1,
		[]string{"A", "B", "C"}, []string{"a", "b", "c"}, []int{0, 3, -2},
		[]int{60}, []int{60}, []int{60}))
	for i, want := range []int{61, 64, 59} {
		if got := s.Tracks[i].Notes[0].Pitch; got != want {
			t.Fatalf("track %d pitch = %d, want %d", i, got, want)
		}
	}
}

// TestTransposeAboveRange rejects a note transposed past 127 with a
// detailed error.
func TestTransposeAboveRange(t *testing.T) {
	s := mustParseJSON(t, transposeScore(12,
		[]string{"Violins"}, []string{"vln"}, []int{0}, []int{120}))
	err := Validate(s)
	if err == nil {
		t.Fatal("want error for pitch 132, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"Violins", "note index 0", "120", "132", "exceeds"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if !strings.Contains(msg, "maximum") {
		t.Errorf("error %q does not contain %q", msg, "maximum")
	}
}

// TestTransposeBelowRange rejects a note transposed below 0 with a
// detailed error.
func TestTransposeBelowRange(t *testing.T) {
	s := mustParseJSON(t, transposeScore(-12,
		[]string{"Basses"}, []string{"bass"}, []int{0}, []int{5}))
	err := Validate(s)
	if err == nil {
		t.Fatal("want error for pitch -7, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"Basses", "note index 0", "5", "-7", "below"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if !strings.Contains(msg, "minimum") {
		t.Errorf("error %q does not contain %q", msg, "minimum")
	}
}

// TestTransposeZeroDefault leaves boundary pitches untouched and valid.
func TestTransposeZeroDefault(t *testing.T) {
	s := mustParseJSON(t, `{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"},
		"tracks": [{"id": "t", "name": "Solo", "instrument": {"type": "ocarina"},
		"pan": 0, "volume": 0.8,
		"notes": [{"time": 0, "duration": 0.5, "pitch": 0, "velocity": 0.8},
		{"time": 0.5, "duration": 0.5, "pitch": 60, "velocity": 0.8},
		{"time": 1.0, "duration": 0.5, "pitch": 127, "velocity": 0.8}]}]}`)
	if got, want := pitchesOf(s, 0), []int{0, 60, 127}; !equalInts(got, want) {
		t.Fatalf("pitches = %v, want %v", got, want)
	}
	if s.Metadata.Transpose != 0 || s.Tracks[0].Transpose != 0 {
		t.Fatalf("transpose defaults = %d/%d, want 0/0",
			s.Metadata.Transpose, s.Tracks[0].Transpose)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("untransposed score rejected: %v", err)
	}
}

// TestTransposeMultipleErrors collects one error per offending note.
func TestTransposeMultipleErrors(t *testing.T) {
	s := mustParseJSON(t, transposeScore(24,
		[]string{"Brass"}, []string{"brass"}, []int{0},
		[]int{104, 108, 112, 116, 120}))
	err := Validate(s)
	if err == nil {
		t.Fatal("want 5 errors, got nil")
	}
	msg := err.Error()
	if n := strings.Count(msg, "note index"); n != 5 {
		t.Fatalf("found %d note errors, want 5: %q", n, msg)
	}
	for _, want := range []string{"128", "132", "136", "140", "144"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not contain resulting pitch %s: %q", want, msg)
		}
	}
}
