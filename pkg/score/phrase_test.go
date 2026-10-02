package score

import (
	"strings"
	"testing"
)

// phraseScore builds a one-track score with phrase tags on six notes
// and breath fields on the metadata and track.
func phraseScore(metaBreath, trackBreath string, phrases ...int) string {
	var b strings.Builder
	b.WriteString(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"`)
	if metaBreath != "" {
		b.WriteString(`,"breath": `)
		b.WriteString(metaBreath)
	}
	b.WriteString(`}, "tracks": [{"id": "t", "name": "Solo", `)
	b.WriteString(`"instrument": {"type": "ocarina"}, "pan": 0, "volume": 0.8`)
	if trackBreath != "" {
		b.WriteString(`,"breath": `)
		b.WriteString(trackBreath)
	}
	b.WriteString(`, "notes": [`)
	for j, p := range phrases {
		if j > 0 {
			b.WriteString(`, `)
		}
		b.WriteString(`{"time": `)
		b.WriteString(itoa(j))
		b.WriteString(`.0, "duration": 0.5, "pitch": 60, "velocity": 0.8`)
		if p != 0 {
			b.WriteString(`,"phrase_id": `)
			b.WriteString(itoa(p))
		}
		b.WriteString(`}`)
	}
	b.WriteString(`]}]}`)
	return b.String()
}

// TestPhraseFields keeps phrase tags through parsing.
func TestPhraseFields(t *testing.T) {
	s, err := ParseJSON([]byte(phraseScore("", "", 1, 1, 2, 2, 3, 3)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for j, want := range []int{1, 1, 2, 2, 3, 3} {
		if got := s.Tracks[0].Notes[j].PhraseID; got != want {
			t.Fatalf("note %d phrase_id = %d, want %d", j, got, want)
		}
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid phrase score rejected: %v", err)
	}
}

// TestBreathFields parses global and per-track breath pauses.
func TestBreathFields(t *testing.T) {
	s, err := ParseJSON([]byte(phraseScore("50", "120", 1, 2)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Metadata.BreathMs != 50 || s.Tracks[0].BreathMs != 120 {
		t.Fatalf("breath = %v/%v, want 50/120", s.Metadata.BreathMs, s.Tracks[0].BreathMs)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid breath score rejected: %v", err)
	}
}

// TestBreathNegative rejects a negative pause.
func TestBreathNegative(t *testing.T) {
	s, err := ParseJSON([]byte(phraseScore("-10", "", 1, 2)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "breath") {
		t.Fatalf("want breath error, got %v", err)
	}
}

// roomScore builds a one-track score with a raw room member.
func roomScore(room string) string {
	return `{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, ` +
		`"tracks": [{"id": "t", "name": "Solo", "instrument": {"type": "ocarina"}, ` +
		`"pan": 0, "volume": 0.8, "room": ` + room + `, ` +
		`"notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`
}

// TestRoomPresetString decodes a preset-name room.
func TestRoomPresetString(t *testing.T) {
	s, err := ParseJSON([]byte(roomScore(`"cathedral"`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := s.Tracks[0].Room
	if r == nil || !r.IsPreset || r.Preset != "cathedral" {
		t.Fatalf("room = %+v, want cathedral preset", r)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid room score rejected: %v", err)
	}
}

// TestRoomObject decodes an explicit space.
func TestRoomObject(t *testing.T) {
	s, err := ParseJSON([]byte(roomScore(`{"size": 0.7, "damping": 0.4, "width": 0.8}`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := s.Tracks[0].Room
	if r == nil || r.IsPreset {
		t.Fatalf("room = %+v, want object", r)
	}
	if r.Config != (RoomConfig{Size: 0.7, Damping: 0.4, Width: 0.8}) {
		t.Fatalf("room config = %+v", r.Config)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("valid room score rejected: %v", err)
	}
}

// TestRoomAbsent leaves the track on the master reverb.
func TestRoomAbsent(t *testing.T) {
	s, err := ParseJSON([]byte(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, ` +
		`"tracks": [{"id": "t", "name": "Solo", "instrument": {"type": "ocarina"}, ` +
		`"pan": 0, "volume": 0.8, ` +
		`"notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Tracks[0].Room != nil {
		t.Fatalf("room = %+v, want nil", s.Tracks[0].Room)
	}
}

// TestRoomInvalidPreset rejects an unknown space name.
func TestRoomInvalidPreset(t *testing.T) {
	s, err := ParseJSON([]byte(roomScore(`"cavern"`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "room") {
		t.Fatalf("want room error, got %v", err)
	}
}

// TestRoomObjectRange rejects out-of-range space fields.
func TestRoomObjectRange(t *testing.T) {
	s, err := ParseJSON([]byte(roomScore(`{"size": 2.0, "damping": 0.4, "width": 0.8}`)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "room.size") {
		t.Fatalf("want room.size error, got %v", err)
	}
}
