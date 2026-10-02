package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
)

func ocarinaTrack(id string, pan float32, notes []score.NoteEvent) score.Track {
	return score.Track{
		ID:         id,
		Name:       id,
		Instrument: score.InstrumentDef{Type: "ocarina"},
		Pan:        pan,
		Volume:     0.8,
		Notes:      notes,
	}
}

func renderStereo(t *testing.T, s *score.Score) []float32 {
	t.Helper()
	eng, err := New(s, 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	eng.Mixer().SetReverbWet(0)
	var out []float32
	done := 0
	total := eng.TotalFrames()
	for done < total {
		n := eng.BlockSize()
		if total-done < n {
			n = total - done
		}
		eng.ProcessFrames(n, func(master []float32) {
			out = append(out, master...)
		})
		done += n
	}
	return out
}

func windowMax(out []float32, sr int, fromSec, toSec float64) float32 {
	peak := float32(0)
	a := int(fromSec * float64(sr))
	b := int(toSec * float64(sr))
	if a < 0 {
		a = 0
	}
	if b > len(out)/2 {
		b = len(out) / 2
	}
	for i := a; i < b; i++ {
		for _, v := range []float32{out[2*i], out[2*i+1]} {
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
	}
	return peak
}

func TestEngineTailSilence(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{ocarinaTrack("a", 0, []score.NoteEvent{
			{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8},
		})},
	}
	eng, err := New(s, 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := int(math.Round(0.5*48000)) + int(2.5*48000)
	if eng.TotalFrames() != wantTotal {
		t.Fatalf("TotalFrames = %d, want %d", eng.TotalFrames(), wantTotal)
	}
	if eng.SampleRate() != 48000 || eng.BlockSize() != DefaultBlockSize {
		t.Fatalf("rate/block = %v/%v", eng.SampleRate(), eng.BlockSize())
	}
	if eng.TrackCount() != 1 || eng.Score() == nil {
		t.Fatalf("track/score = %d/%v", eng.TrackCount(), eng.Score())
	}
	out := renderStereo(t, s)
	if got := windowMax(out, 48000, 0, 0.5); got < 0.05 {
		t.Fatalf("note window silent: peak %v", got)
	}
	if got := windowMax(out, 48000, 2.0, 3.0); got > 1e-3 {
		t.Fatalf("tail not silent: peak %v", got)
	}
	if d := eng.Duration(); math.Abs(d-3.0) > 1e-9 {
		t.Fatalf("Duration = %v, want 3.0", d)
	}
}

func TestEngineBreathGap(t *testing.T) {
	mk := func(breath float64) *score.Score {
		return &score.Score{
			Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4", BreathMs: breath},
			Tracks: []score.Track{{
				ID: "a", Name: "a",
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Pan:        0, Volume: 0.8,
				Notes: []score.NoteEvent{
					{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8, PhraseID: 1},
					{Time: 1.0, Duration: 0.5, Pitch: 72, Velocity: 0.8, PhraseID: 2},
				},
			}},
		}
	}
	plain, err := New(mk(0), 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	breathy, err := New(mk(500), 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	_ = plain
	_ = breathy
	// The engine total derives from the unshifted score, so assert the
	// observable: without breath the second phrase sounds at 1.0 s,
	// with a 500 ms breath it stays silent until ~1.5 s.
	plainOut := renderStereo(t, mk(0))
	if got := windowMax(plainOut, 48000, 1.0, 1.4); got < 0.05 {
		t.Fatalf("plain second phrase silent: %v", got)
	}
	out := renderStereo(t, mk(500))
	if got := windowMax(out, 48000, 0, 0.5); got < 0.05 {
		t.Fatalf("first phrase silent: %v", got)
	}
	if got := windowMax(out, 48000, 0.7, 1.4); got > 0.02 {
		t.Fatalf("breath gap not silent: %v", got)
	}
	if got := windowMax(out, 48000, 1.5, 2.0); got < 0.05 {
		t.Fatalf("second phrase silent: %v", got)
	}
}

func TestEngineMultiTrackIsolation(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			ocarinaTrack("a", -1, []score.NoteEvent{{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8}}),
			ocarinaTrack("b", 1, []score.NoteEvent{{Time: 1.0, Duration: 0.5, Pitch: 72, Velocity: 0.8}}),
			ocarinaTrack("c", -1, []score.NoteEvent{{Time: 2.0, Duration: 0.5, Pitch: 76, Velocity: 0.8}}),
			ocarinaTrack("d", 1, []score.NoteEvent{{Time: 3.0, Duration: 0.5, Pitch: 79, Velocity: 0.8}}),
		},
	}
	// EQ + per-note attack on track a exercises the EQ wrapper voice path.
	s.Tracks[0].EQ.HPF = 80
	s.Tracks[0].Notes[0].AttackSec = 0.05
	eng, err := New(s, 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	eng.Mixer().SetReverbWet(0)
	type win struct {
		from, to float64
		left     bool
	}
	wins := []win{{0, 0.5, true}, {1.0, 1.5, false}, {2.0, 2.5, true}, {3.0, 3.5, false}}
	var mixed []float32
	peaks := make([][]float32, 4)
	done, total := 0, eng.TotalFrames()
	for done < total {
		n := eng.BlockSize()
		if total-done < n {
			n = total - done
		}
		eng.ProcessFrames(n, func(master []float32) {
			mixed = append(mixed, master...)
		})
		for i := range peaks {
			peaks[i] = append(peaks[i], eng.TrackPeak(i))
		}
		done += n
		if eng.PositionFrames() != done {
			t.Fatalf("PositionFrames = %d, want %d", eng.PositionFrames(), done)
		}
	}
	chanRMS := func(from, to float64, left bool) float64 {
		a, b := int(from*48000), int(to*48000)
		sum := 0.0
		for i := a; i < b; i++ {
			v := mixed[2*i]
			if !left {
				v = mixed[2*i+1]
			}
			sum += float64(v * v)
		}
		return math.Sqrt(sum / float64(b-a))
	}
	for _, w := range wins {
		dom := chanRMS(w.from, w.to, w.left)
		other := chanRMS(w.from, w.to, !w.left)
		if dom < 0.02 {
			t.Fatalf("window [%v,%v] silent on dominant side: %v", w.from, w.to, dom)
		}
		if other > dom*0.05 {
			t.Fatalf("window [%v,%v] leaks: other %v vs dominant %v", w.from, w.to, other, dom)
		}
	}
	for i := range peaks {
		hot := false
		for _, p := range peaks[i] {
			if p > 0.02 {
				hot = true
				break
			}
		}
		if !hot {
			t.Fatalf("track %d never peaked", i)
		}
	}
	l, r := eng.Peak()
	if l < 0 || r < 0 {
		t.Fatalf("peak negative: %v %v", l, r)
	}
}

func TestEngineHumanizeSeededAudio(t *testing.T) {
	base := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{ocarinaTrack("a", 0, []score.NoteEvent{
			{Time: 0, Duration: 0.4, Pitch: 69, Velocity: 0.8},
			{Time: 0.5, Duration: 0.4, Pitch: 72, Velocity: 0.8},
			{Time: 1.0, Duration: 0.4, Pitch: 76, Velocity: 0.8},
			{Time: 1.5, Duration: 0.4, Pitch: 79, Velocity: 0.8},
		})},
	}
	render := func(seed uint64) []float32 {
		return renderStereo(t, Humanize(base, 0.5, seed))
	}
	a1, a2 := render(7), render(7)
	if len(a1) != len(a2) {
		t.Fatalf("lengths differ: %d vs %d", len(a1), len(a2))
	}
	for i := range a1 {
		if a1[i] != a2[i] {
			t.Fatalf("same seed differs at %d", i)
		}
	}
	b := render(8)
	same := len(a1) == len(b)
	if same {
		diff := false
		for i := range a1 {
			if a1[i] != b[i] {
				diff = true
				break
			}
		}
		same = !diff
	}
	if same {
		t.Fatal("different seeds render identically")
	}
}

func TestEngineOfflineRendererFile(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{ocarinaTrack("a", 0, []score.NoteEvent{
			{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8},
		})},
	}
	path := filepath.Join(t.TempDir(), "off.wav")
	r, err := NewOfflineRenderer(s, path, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine() == nil || r.Frames() <= 0 || r.Done() {
		t.Fatalf("bad renderer: %+v", r)
	}
	for !r.Done() {
		r.ProcessBlock()
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if p := r.Progress(); p != 1 {
		t.Fatalf("Progress = %v, want 1", p)
	}
	if r.Peak() <= 0 {
		t.Fatal("renderer peak silent")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 44 {
		t.Fatalf("wav missing/small: %+v %v", info, err)
	}
}

func TestEngineRoomMerge(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
	}
	for i := 0; i < 8; i++ {
		tr := ocarinaTrack(string(rune('a'+i)), 0, []score.NoteEvent{
			{Time: 0, Duration: 0.2, Pitch: 60 + i, Velocity: 0.7},
		})
		size := 0.05 * float64(i+1)
		tr.Room = &score.Room{Config: score.RoomConfig{Size: size, Damping: 0.5, Width: 0.5}}
		s.Tracks = append(s.Tracks, tr)
	}
	s.Tracks[0].Room = &score.Room{IsPreset: true, Preset: "none"}
	s.Tracks[1].Room = &score.Room{IsPreset: true, Preset: "hall"}
	out := renderStereo(t, s)
	if got := windowMax(out, 48000, 0, 0.3); got < 0.02 {
		t.Fatalf("merged rooms silent: %v", got)
	}
}

// TestEngineRoomMergeVictimLast forces the >8-room merge with the victim
// as the last entry (unique least use), so survivor indexes stay valid.
func TestEngineRoomMergeVictimLast(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
	}
	id := 0
	for e := 0; e < 8; e++ {
		for k := 0; k < 2; k++ {
			tr := ocarinaTrack(string(rune('a'+id)), 0, []score.NoteEvent{
				{Time: 0.1 * float64(id), Duration: 0.2, Pitch: 60 + id%12, Velocity: 0.5},
			})
			tr.Room = &score.Room{Config: score.RoomConfig{Size: 0.1 + 0.1*float64(e), Damping: 0.5, Width: 0.5}}
			s.Tracks = append(s.Tracks, tr)
			id++
		}
	}
	last := ocarinaTrack("zz", 0, []score.NoteEvent{
		{Time: 0, Duration: 0.2, Pitch: 72, Velocity: 0.5},
	})
	last.Room = &score.Room{Config: score.RoomConfig{Size: 0.95, Damping: 0.5, Width: 0.5}}
	s.Tracks = append(s.Tracks, last)
	out := renderStereo(t, s)
	if got := windowMax(out, 48000, 0, 0.3); got < 0.02 {
		t.Fatalf("merged rooms silent: %v", got)
	}
}

// TestBUG1EngineRoomMergePanics guards BUG-1: with 9 distinct rooms
// (hall + 8 objects) at equal use the merge victim is entry 0 while the
// nearest survivor sits at the last index. The repoint must use the
// post-removal index (pkg/engine/phrase.go), otherwise assignRooms
// panics with index out of range.
func TestBUG1EngineRoomMergePanics(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
	}
	for i := 0; i < 10; i++ {
		tr := ocarinaTrack(string(rune('a'+i)), 0, []score.NoteEvent{
			{Time: 0, Duration: 0.2, Pitch: 60 + i, Velocity: 0.7},
		})
		size := 0.05 * float64(i+1)
		tr.Room = &score.Room{Config: score.RoomConfig{Size: size, Damping: 0.5, Width: 0.5}}
		s.Tracks = append(s.Tracks, tr)
	}
	s.Tracks[0].Room = &score.Room{IsPreset: true, Preset: "none"}
	s.Tracks[1].Room = &score.Room{IsPreset: true, Preset: "hall"}
	eng, err := New(s, 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[dsp.ReverbParams]bool{}
	for i := range s.Tracks {
		if i == 0 {
			if _, ok := eng.Mixer().TrackRoom(i); ok {
				t.Fatalf("track 0 (none) reports a room override")
			}
			continue
		}
		p, ok := eng.Mixer().TrackRoom(i)
		if !ok {
			t.Fatalf("track %d lost its room assignment in the merge", i)
		}
		seen[p] = true
	}
	if len(seen) > 8 {
		t.Fatalf("distinct rooms = %d, want <= 8 after merge", len(seen))
	}
	out := renderStereo(t, s)
	if got := windowMax(out, 48000, 0, 0.3); got < 0.02 {
		t.Fatalf("merged rooms silent: %v", got)
	}
}
