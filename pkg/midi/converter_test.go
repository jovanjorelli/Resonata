package midi

import (
	"math"
	"os"
	"testing"

	"resonata/pkg/score"
)

func assetFile(t *testing.T) *File {
	t.Helper()
	data, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ReadFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestToScoreFromAsset(t *testing.T) {
	s, err := ToScore(assetFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := score.Validate(s); err != nil {
		t.Fatalf("imported score invalid: %v", err)
	}
	if s.Metadata.Title != "Mahler Symphony Sketch" {
		t.Errorf("title = %q", s.Metadata.Title)
	}
	if s.Metadata.BPM != 120 || s.Metadata.TimeSignature != "4/4" {
		t.Errorf("metadata = %+v", s.Metadata)
	}
	if len(s.Tracks) != 5 { // conductor track has no notes
		t.Fatalf("tracks = %d, want 5", len(s.Tracks))
	}
	total := s.TotalNotes()
	if total < 250 {
		t.Errorf("notes = %d, want 250+", total)
	}
	// Track identities from FF 03 names.
	names := map[string]bool{}
	for _, tr := range s.Tracks {
		names[tr.Name] = true
		for _, n := range tr.Notes {
			if n.Pitch < 0 || n.Pitch > 127 || n.Velocity <= 0 || n.Velocity > 1 ||
				n.Time < 0 || n.Duration <= 0 {
				t.Fatalf("bad note %+v in track %s", n, tr.Name)
			}
			if n.Time+n.Duration > 33 {
				t.Fatalf("note beyond the 32 s piece: %+v", n)
			}
		}
	}
	for _, want := range []string{"Strings", "Woodwinds", "Brass", "Percussion", "Harp"} {
		if !names[want] {
			t.Errorf("track %q missing (names: %v)", want, names)
		}
	}
	// The strings track picked up its CC7/CC10 mix: volume 100/127*0.9,
	// pan 54/63.5-1.
	for _, tr := range s.Tracks {
		if tr.Name == "Strings" {
			if math.Abs(float64(tr.Volume)-100.0/127*0.9) > 0.01 {
				t.Errorf("strings volume = %v", tr.Volume)
			}
			if math.Abs(float64(tr.Pan)-(54.0/63.5-1)) > 0.02 {
				t.Errorf("strings pan = %v", tr.Pan)
			}
			// Program 48 = strings family voicing.
			if tr.Instrument.Parameters["brightness"] != 0.45 {
				t.Errorf("strings voicing = %v", tr.Instrument.Parameters)
			}
		}
	}
	// Bar-aligned tick grid maps to exact half-second times at 120 BPM.
	for _, tr := range s.Tracks {
		if tr.Name == "Strings" {
			for _, n := range tr.Notes {
				if math.Mod(n.Time, 0.5) > 1e-9 {
					t.Fatalf("strings note off the 8th grid: %+v", n)
				}
			}
		}
	}
}

func TestFromScoreStructure(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "Struct Test", BPM: 90, TimeSignature: "3/4"},
		Tracks: []score.Track{
			{
				ID: "a", Name: "Solo", Volume: 0.9, Pan: -0.5, ReverbSend: 0.3,
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Notes: []score.NoteEvent{
					{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8},
					{Time: 0.5, Duration: 0.25, Pitch: 64, Velocity: 0.6},
				},
			},
			{
				ID: "b", Name: "Keys", Volume: 0.7,
				Instrument: score.InstrumentDef{Type: "sampler", File: "x.sfz"},
				Notes:      []score.NoteEvent{{Time: 0, Duration: 1, Pitch: 48, Velocity: 1}},
			},
		},
	}
	f := FromScore(s, 0) // default PPQ
	if f.Header.Format != 1 || f.Header.TicksPerQuarter != DefaultTicksPerQuarter {
		t.Fatalf("header = %+v", f.Header)
	}
	if len(f.Tracks) != 3 { // conductor + 2
		t.Fatalf("tracks = %d, want 3", len(f.Tracks))
	}
	cond := f.Tracks[0]
	if cond[0].Type != EventMeta || cond[0].Data1 != MetaTempo {
		t.Fatal("conductor does not start with tempo")
	}
	mpq := int(cond[0].Data[0])<<16 | int(cond[0].Data[1])<<8 | int(cond[0].Data[2])
	if math.Abs(BPMFromMicros(mpq)-90) > 0.01 {
		t.Errorf("tempo = %d µs, want ~90 BPM", mpq)
	}
	if cond[1].Data1 != MetaTimeSignature || cond[1].Data[0] != 3 || cond[1].Data[1] != 2 {
		t.Errorf("time signature meta = %+v, want 3/4", cond[1])
	}
	if string(cond[2].Data) != "Struct Test" {
		t.Errorf("title meta = %q", cond[2].Data)
	}
	// Program choices and channel assignment (channel 9 skipped).
	prog := map[int]int{}
	for _, tr := range f.Tracks[1:] {
		for _, e := range tr {
			if e.Type == EventProgramChange {
				prog[e.Channel] = e.Data1
			}
		}
	}
	if prog[0] != programOcarina {
		t.Errorf("ocarina program = %v, want %d", prog[0], programOcarina)
	}
	if prog[1] != programPiano {
		t.Errorf("sampler program = %v, want %d", prog[1], programPiano)
	}
	// End-of-track metas present on every track.
	for i, tr := range f.Tracks {
		last := tr[len(tr)-1]
		if last.Type != EventMeta || last.Data1 != MetaEndOfTrack {
			t.Errorf("track %d does not end with FF 2F", i)
		}
	}
}

// TestScoreRoundTrip is the JSON->MIDI->JSON exactness contract: pitches
// and velocities identical, timings within float tolerance.
func TestScoreRoundTrip(t *testing.T) {
	orig, err := ToScore(assetFile(t))
	if err != nil {
		t.Fatal(err)
	}
	data := WriteFile(FromScore(orig, DefaultTicksPerQuarter))
	back, err := ReadFile(data)
	if err != nil {
		t.Fatal(err)
	}
	round, err := ToScore(back)
	if err != nil {
		t.Fatal(err)
	}
	if err := score.Validate(round); err != nil {
		t.Fatalf("round-tripped score invalid: %v", err)
	}

	if round.Metadata.Title != orig.Metadata.Title {
		t.Errorf("title %q -> %q", orig.Metadata.Title, round.Metadata.Title)
	}
	if math.Abs(round.Metadata.BPM-orig.Metadata.BPM) > 0.01 {
		t.Errorf("BPM %v -> %v", orig.Metadata.BPM, round.Metadata.BPM)
	}
	if round.Metadata.TimeSignature != orig.Metadata.TimeSignature {
		t.Errorf("time signature %q -> %q", orig.Metadata.TimeSignature, round.Metadata.TimeSignature)
	}
	if len(round.Tracks) != len(orig.Tracks) {
		t.Fatalf("tracks %d -> %d", len(orig.Tracks), len(round.Tracks))
	}
	for i := range orig.Tracks {
		a, b := orig.Tracks[i], round.Tracks[i]
		if a.Name != b.Name {
			t.Errorf("track %d name %q -> %q", i, a.Name, b.Name)
		}
		if len(a.Notes) != len(b.Notes) {
			t.Fatalf("track %d: %d notes -> %d", i, len(a.Notes), len(b.Notes))
		}
		for j := range a.Notes {
			x, y := a.Notes[j], b.Notes[j]
			if x.Pitch != y.Pitch {
				t.Fatalf("track %d note %d: pitch %d -> %d", i, j, x.Pitch, y.Pitch)
			}
			if math.Abs(float64(x.Velocity-y.Velocity)) > 1.0/127+1e-6 {
				t.Fatalf("track %d note %d: velocity %v -> %v", i, j, x.Velocity, y.Velocity)
			}
			if math.Abs(x.Time-y.Time) > 1e-9 || math.Abs(x.Duration-y.Duration) > 1e-9 {
				t.Fatalf("track %d note %d: time %v/%v -> %v/%v", i, j,
					x.Time, x.Duration, y.Time, y.Duration)
			}
		}
	}
}

func TestTempoMapMultiSegment(t *testing.T) {
	// 480 PPQ: bar 1 at 60 BPM (1 s/beat), then 120 BPM from tick 1920.
	f := &File{
		Header: Header{Format: 1, Tracks: 1, TicksPerQuarter: 480},
		Tracks: [][]Event{{
			{Tick: 0, Type: EventMeta, Data1: MetaTempo, Data: be24(1000000)},
			{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 100},
			{Tick: 1920, Type: EventMeta, Data1: MetaTempo, Data: be24(500000)},
			{Tick: 1920, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 40},
			{Tick: 2400, Type: EventNoteOn, Channel: 0, Data1: 64, Data2: 100},
			{Tick: 2880, Type: EventNoteOff, Channel: 0, Data1: 64, Data2: 40},
			{Tick: 2880, Type: EventMeta, Data1: MetaEndOfTrack},
		}},
	}
	s, err := ToScore(f)
	if err != nil {
		t.Fatal(err)
	}
	if s.Metadata.BPM != 60 {
		t.Errorf("BPM = %v, want first tempo 60", s.Metadata.BPM)
	}
	n0, n1 := s.Tracks[0].Notes[0], s.Tracks[0].Notes[1]
	// First note: 1920 ticks = four beats at 60 BPM = 4.0 s.
	if math.Abs(n0.Time) > 1e-9 || math.Abs(n0.Duration-4.0) > 1e-9 {
		t.Errorf("note 0 = %+v, want start 0 dur 4.0", n0)
	}
	// Second: starts at tick 2400 = 4.0 s + 480 ticks at 120 BPM = 4.5 s,
	// spanning 480 ticks = 0.5 s.
	if math.Abs(n1.Time-4.5) > 1e-9 || math.Abs(n1.Duration-0.5) > 1e-9 {
		t.Errorf("note 1 = %+v, want start 4.5 dur 0.5", n1)
	}
}

func TestGMProgramNames(t *testing.T) {
	if GMProgramNames[0] != "Acoustic Grand Piano" {
		t.Errorf("program 0 = %q", GMProgramNames[0])
	}
	if GMProgramNames[79] != "Ocarina" {
		t.Errorf("program 79 = %q, want Ocarina", GMProgramNames[79])
	}
	if GMProgramNames[48] != "String Ensemble 1" {
		t.Errorf("program 48 = %q", GMProgramNames[48])
	}
	if GMProgramNames[127] != "Gunshot" {
		t.Errorf("program 127 = %q", GMProgramNames[127])
	}
}

func TestMicrosFromBPM(t *testing.T) {
	if mpq := MicrosFromBPM(120); mpq != 500000 {
		t.Errorf("120 BPM = %d µs", mpq)
	}
	if mpq := MicrosFromBPM(60); mpq != 1000000 {
		t.Errorf("60 BPM = %d µs", mpq)
	}
	if mpq := MicrosFromBPM(0); mpq != 500000 {
		t.Errorf("0 BPM = %d µs, want 120 BPM default", mpq)
	}
	if mpq := MicrosFromBPM(1e9); mpq < 60 {
		t.Errorf("clamp failed: %d", mpq)
	}
}
