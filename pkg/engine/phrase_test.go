package engine

import (
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
)

// breathTrack builds an ocarina track of six one-second notes at times
// 0..5 with distinct pitches and the given phrase tags and breath.
func breathTrack(phrases []int, breath float64) score.Track {
	tr := score.Track{
		ID: "t", Name: "Solo",
		Instrument: score.InstrumentDef{Type: "ocarina"},
		Volume:     0.8,
		BreathMs:   breath,
	}
	for j, p := range phrases {
		tr.Notes = append(tr.Notes, score.NoteEvent{
			Time: float64(j), Duration: 0.5,
			Pitch: 60 + j, Velocity: 0.8, PhraseID: p,
		})
	}
	return tr
}

// breathScore wraps tracks with a global breath value.
func breathScore(globalBreath float64, tracks ...score.Track) *score.Score {
	return &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4", BreathMs: globalBreath},
		Tracks:   tracks,
	}
}

// onFrames maps each scheduled note-on pitch to its frame.
func onFrames(t *testing.T, e *Engine) map[int]int {
	t.Helper()
	out := map[int]int{}
	for i := range e.events {
		ev := &e.events[i]
		if ev.on {
			out[ev.pitch] = ev.frame
		}
	}
	return out
}

const testRate48 = 48000

// TestPhraseBreath delays each new phrase cumulatively: note 3 by
// 100 ms after one transition, note 5 by 200 ms after two.
func TestPhraseBreath(t *testing.T) {
	s := breathScore(0, breathTrack([]int{1, 1, 2, 2, 3, 3}, 100))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	frames := onFrames(t, e)
	at := func(sec float64) int { return int(sec * testRate48) }
	for j, want := range map[int]int{
		60: at(0), 61: at(1), 62: at(2.1), 63: at(3.1), 64: at(4.2), 65: at(5.2),
	} {
		_ = j
		if got := frames[j]; got != want {
			t.Errorf("pitch %d frame = %d, want %d", j, got, want)
		}
	}
}

// TestBreathZeroPhrase inserts no pause across phrase_id zero.
func TestBreathZeroPhrase(t *testing.T) {
	s := breathScore(0, breathTrack([]int{0, 1, 0, 2}, 100))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	frames := onFrames(t, e)
	for j := 0; j < 4; j++ {
		if got, want := frames[60+j], j*testRate48; got != want {
			t.Errorf("pitch %d frame = %d, want %d", 60+j, got, want)
		}
	}
}

// TestBreathDefault leaves times untouched without a breath value.
func TestBreathDefault(t *testing.T) {
	s := breathScore(0, breathTrack([]int{1, 2, 3}, 0))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	frames := onFrames(t, e)
	for j := 0; j < 3; j++ {
		if got, want := frames[60+j], j*testRate48; got != want {
			t.Errorf("pitch %d frame = %d, want %d", 60+j, got, want)
		}
	}
}

// TestBreathOverride prefers the track pause over the global pause:
// a 120 ms track breath delays the second phrase by 5760 frames.
func TestBreathOverride(t *testing.T) {
	s := breathScore(50, breathTrack([]int{1, 2}, 120))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	frames := onFrames(t, e)
	if got, want := frames[60], 0; got != want {
		t.Errorf("first phrase frame = %d, want %d", got, want)
	}
	if got, want := frames[61], int(1.12*testRate48); got != want {
		t.Errorf("second phrase frame = %d, want %d", got, want)
	}
}

// roomTrack builds an ocarina track carrying a raw room member.
func roomTrack(id, room string) score.Track {
	tr := score.Track{
		ID: id, Name: id,
		Instrument: score.InstrumentDef{Type: "ocarina"},
		Volume:     0.8,
	}
	if room != "" {
		r := &score.Room{}
		if err := r.UnmarshalJSON([]byte(room)); err != nil {
			panic(err)
		}
		tr.Room = r
	}
	tr.Notes = append(tr.Notes, score.NoteEvent{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8})
	return tr
}

// TestRoomPreset routes a cathedral track to cathedral parameters.
func TestRoomPreset(t *testing.T) {
	s := breathScore(0, roomTrack("t", `"cathedral"`))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want, _ := dsp.PresetParams("cathedral")
	got, ok := e.Mixer().TrackRoom(0)
	if !ok {
		t.Fatal("track has no room override")
	}
	if got != want {
		t.Fatalf("room params = %+v, want %+v", got, want)
	}
}

// TestRoomObject routes explicit size/damping/width values.
func TestRoomObject(t *testing.T) {
	s := breathScore(0, roomTrack("t", `{"size": 0.7, "damping": 0.4, "width": 0.8}`))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, ok := e.Mixer().TrackRoom(0)
	if !ok {
		t.Fatal("track has no room override")
	}
	if got.RoomSize != 0.7 || got.Damping != 0.4 || got.Width != 0.8 {
		t.Fatalf("room params = %+v, want 0.7/0.4/0.8", got)
	}
}

// TestRoomMaster keeps a roomless track on the master reverb.
func TestRoomMaster(t *testing.T) {
	s := breathScore(0, roomTrack("t", ""))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := e.Mixer().TrackRoom(0); ok {
		t.Fatal("roomless track reports an override")
	}
}

// TestRoomMultiple routes three rooms to their own spaces.
func TestRoomMultiple(t *testing.T) {
	s := breathScore(0,
		roomTrack("a", `"cathedral"`),
		roomTrack("b", `"hall"`),
		roomTrack("c", `"room"`))
	e, err := New(s, testRate48, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i, name := range []string{"cathedral", "hall", "room"} {
		want, _ := dsp.PresetParams(name)
		got, ok := e.Mixer().TrackRoom(i)
		if !ok {
			t.Fatalf("track %d has no room override", i)
		}
		if got != want {
			t.Fatalf("track %d room = %+v, want %s %+v", i, got, name, want)
		}
	}
	// Each track renders through its own space without errors.
	e.ProcessFrames(e.TotalFrames(), nil)
}
