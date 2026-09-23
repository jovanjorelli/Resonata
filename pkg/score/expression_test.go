package score

import (
	"strings"
	"testing"
)

// exprScore builds a one-track score string with full control over the
// expressive fields. extraMeta and extraTrack inject raw JSON members
// (e.g. `"swing": 1.0`) into the metadata and track objects.
func exprScore(extraMeta, extraTrack string, notes ...string) string {
	var b strings.Builder
	b.WriteString(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"`)
	if extraMeta != "" {
		b.WriteString(", ")
		b.WriteString(extraMeta)
	}
	b.WriteString(`}, "tracks": [{"id": "t", "name": "Solo", `)
	b.WriteString(`"instrument": {"type": "ocarina"}, "pan": 0, "volume": 0.8`)
	if extraTrack != "" {
		b.WriteString(", ")
		b.WriteString(extraTrack)
	}
	b.WriteString(`, "notes": [`)
	b.WriteString(strings.Join(notes, ", "))
	b.WriteString(`]}]}`)
	return b.String()
}

func mustParseExpr(t *testing.T, data string) *Score {
	t.Helper()
	s, err := ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return s
}

func closeF64(t *testing.T, got, want float64, what string) {
	t.Helper()
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// TestTimingOffsetPositive delays a note by 5 ms.
func TestTimingOffsetPositive(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 1.0, "duration": 0.5, "pitch": 60, "velocity": 0.8, "timing_offset_ms": 5.0}`))
	closeF64(t, s.Tracks[0].Notes[0].Time, 1.005, "time")
}

// TestTimingOffsetNegative advances a note by 3 ms.
func TestTimingOffsetNegative(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 1.0, "duration": 0.5, "pitch": 60, "velocity": 0.8, "timing_offset_ms": -3.0}`))
	closeF64(t, s.Tracks[0].Notes[0].Time, 0.997, "time")
}

// TestTimingOffsetClampZero keeps a dragged note inside the score.
func TestTimingOffsetClampZero(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 0.001, "duration": 0.5, "pitch": 60, "velocity": 0.8, "timing_offset_ms": -5.0}`))
	closeF64(t, s.Tracks[0].Notes[0].Time, 0.0, "time")
}

// swingEighths returns four eighth notes at 120 BPM: on-beats at 0.0
// and 0.5, off-beats at 0.25 and 0.75.
func swingEighths() []string {
	return []string{
		`{"time": 0.0, "duration": 0.2, "pitch": 60, "velocity": 0.8}`,
		`{"time": 0.25, "duration": 0.2, "pitch": 62, "velocity": 0.8}`,
		`{"time": 0.5, "duration": 0.2, "pitch": 64, "velocity": 0.8}`,
		`{"time": 0.75, "duration": 0.2, "pitch": 65, "velocity": 0.8}`,
	}
}

// TestSwingFull delays off-beats by one-third of the 0.5 s beat.
func TestSwingFull(t *testing.T) {
	s := mustParseExpr(t, exprScore("", `"swing": 1.0`, swingEighths()...))
	got := s.Tracks[0].Notes
	closeF64(t, got[0].Time, 0.0, "on-beat 0")
	closeF64(t, got[2].Time, 0.5, "on-beat 2")
	third := 0.5 / 3
	closeF64(t, got[1].Time, 0.25+third, "off-beat 1")
	closeF64(t, got[3].Time, 0.75+third, "off-beat 3")
	if err := Validate(s); err != nil {
		t.Fatalf("swung score rejected: %v", err)
	}
}

// TestSwingZero leaves every note untouched.
func TestSwingZero(t *testing.T) {
	s := mustParseExpr(t, exprScore(`"swing": 0.0`, `"swing": 0.0`, swingEighths()...))
	for i, want := range []float64{0.0, 0.25, 0.5, 0.75} {
		closeF64(t, s.Tracks[0].Notes[i].Time, want, "note time")
	}
}

// TestSwingOverride prefers the track value over the metadata value:
// 0.8 swing delays the 0.25 off-beat by 0.8 * 0.5 / 3, not 0.5 * 0.5 / 3.
func TestSwingOverride(t *testing.T) {
	s := mustParseExpr(t, exprScore(`"swing": 0.5`, `"swing": 0.8`, swingEighths()...))
	closeF64(t, s.Tracks[0].Notes[1].Time, 0.25+0.8*0.5/3, "off-beat time")
}

// TestAccentMultiplier scales velocity: 0.7 * 1.3 = 0.91.
func TestAccentMultiplier(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 0.0, "duration": 0.5, "pitch": 60, "velocity": 0.7, "accent": 1.3}`))
	got := float64(s.Tracks[0].Notes[0].Velocity)
	if diff := got - 0.91; diff < -1e-6 || diff > 1e-6 {
		t.Fatalf("velocity = %v, want 0.91", got)
	}
}

// TestAccentClamping caps the boosted velocity at 1.0.
func TestAccentClamping(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 0.0, "duration": 0.5, "pitch": 60, "velocity": 0.9, "accent": 1.5}`))
	if got := s.Tracks[0].Notes[0].Velocity; got != 1.0 {
		t.Fatalf("velocity = %v, want 1.0", got)
	}
}

// TestAccentDefault keeps an unaccented note at its base velocity.
func TestAccentDefault(t *testing.T) {
	s := mustParseExpr(t, exprScore("", "",
		`{"time": 0.0, "duration": 0.5, "pitch": 60, "velocity": 0.7}`))
	if got := s.Tracks[0].Notes[0].Velocity; got != 0.7 {
		t.Fatalf("velocity = %v, want 0.7", got)
	}
}
