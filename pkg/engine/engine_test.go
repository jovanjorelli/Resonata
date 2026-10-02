package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/score"
	"resonata/pkg/wav"
)

// writeSine renders a mono sine WAV with the project writer for sampler
// test fixtures.
func writeSine(t *testing.T, path string, sr, frames int, freq float64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := wav.NewWriter(f, wav.PCM24, sr, 1)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, frames)
	for i := range buf {
		buf[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sr)))
	}
	if _, err := w.WriteFrames(buf); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// testScore builds a two-track score: an ocarina note (0-0.2 s, panned
// left) and a staccato sampler note (0.1 s, panned right) whose SFZ and
// sample live in dir.
func testScore(t *testing.T, dir string) *score.Score {
	t.Helper()
	writeSine(t, filepath.Join(dir, "a.wav"), 48000, 4800, 440)
	sfz := "sample=a.wav lokey=0 hikey=127 pitch_keycenter=69\n"
	if err := os.WriteFile(filepath.Join(dir, "t.sfz"), []byte(sfz), 0o644); err != nil {
		t.Fatal(err)
	}
	return &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			{
				ID: "oc", Name: "Ocarina", Pan: -0.5, Volume: 0.8, ReverbSend: 0.5,
				Instrument: score.InstrumentDef{Type: "ocarina",
					Parameters: map[string]float32{"breath_noise": 0.2}},
				Notes: []score.NoteEvent{{Time: 0, Duration: 0.2, Pitch: 69, Velocity: 0.9}},
			},
			{
				ID: "sa", Name: "Sampler", Pan: 0.5, Volume: 0.7,
				Instrument: score.InstrumentDef{Type: "sampler", File: filepath.Join(dir, "t.sfz")},
				Notes: []score.NoteEvent{{Time: 0.1, Duration: 0.05, Pitch: 69, Velocity: 0.8,
					Articulation: score.Articulation{Type: "staccato"}}},
			},
		},
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(nil, 48000, 1024); err == nil {
		t.Fatal("nil score accepted")
	}
	if _, err := New(&score.Score{}, 48000, 1024); err == nil {
		t.Fatal("empty score accepted")
	}
	bad := testScore(t, t.TempDir())
	bad.Tracks[1].Instrument.Type = "theremin"
	if _, err := New(bad, 48000, 1024); err == nil {
		t.Fatal("unknown instrument type accepted")
	}
}

func TestEnginePlayback(t *testing.T) {
	eng, err := New(testScore(t, t.TempDir()), 48000, 1024)
	if err != nil {
		t.Fatal(err)
	}
	eng.Mixer().SetReverbWet(0) // dry for exact silence assertions
	if got := eng.Mixer().TrackSend(0); got != 0.5 {
		t.Errorf("score reverb_send not wired: TrackSend(0) = %v, want 0.5", got)
	}
	if got := eng.Mixer().TrackSend(1); got != 0 {
		t.Errorf("TrackSend(1) = %v, want 0", got)
	}

	// Score duration 0.2 s plus the 2.5 s tail.
	if eng.TotalFrames() != int((0.2+2.5)*48000) {
		t.Fatalf("TotalFrames = %d, want %d", eng.TotalFrames(), int(2.7*48000))
	}
	if eng.TrackCount() != 2 {
		t.Fatalf("TrackCount = %d", eng.TrackCount())
	}

	// During the ocarina note: audible, panned left, sampler still quiet.
	for eng.Position() < 0.05 {
		eng.ProcessBlock()
	}
	l, r := eng.Peak()
	if l < 0.2 || r < 0.05 {
		t.Fatalf("peaks = %v/%v, want audible", l, r)
	}
	if l <= r {
		t.Errorf("pan -0.5 should favor left: L=%v R=%v", l, r)
	}
	if eng.TrackPeak(0) <= 0 {
		t.Error("ocarina track peak is zero while sounding")
	}
	if eng.TrackPeak(1) != 0 {
		t.Error("sampler track peak before its note")
	}

	// Past all releases: exact silence (engine default envelope tails are
	// 0.2 s; both notes end by 0.35 s).
	for eng.Position() < 0.6 {
		eng.ProcessBlock()
	}
	l, r = eng.Peak()
	if l != 0 || r != 0 {
		t.Fatalf("peaks after releases = %v/%v, want exact silence", l, r)
	}
	for i := 0; i < eng.TrackCount(); i++ {
		if eng.TrackPeak(i) != 0 {
			t.Fatalf("track %d still peaks at %v", i, eng.TrackPeak(i))
		}
	}
}

func TestStaccatoScheduling(t *testing.T) {
	eng, err := New(testScore(t, t.TempDir()), 48000, 1024)
	if err != nil {
		t.Fatal(err)
	}
	// Ocarina holds the full 0.2 s; staccato sampler releases after half
	// of its 0.05 s duration: off frames 9600 and round(0.125·48000).
	var ocOff, saOff = -1, -1
	for _, ev := range eng.events {
		if !ev.on {
			switch ev.track {
			case 0:
				ocOff = ev.frame
			case 1:
				saOff = ev.frame
			}
		}
	}
	if ocOff != 9600 {
		t.Errorf("ocarina note-off frame = %d, want 9600", ocOff)
	}
	if saOff != 6000 {
		t.Errorf("staccato note-off frame = %d, want 6000", saOff)
	}
	// Events are sorted by frame, note-offs first on shared frames.
	for i := 1; i < len(eng.events); i++ {
		if eng.events[i-1].frame > eng.events[i].frame {
			t.Fatalf("events unsorted at %d", i)
		}
	}
}

func TestOfflineRenderer(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.wav")
	r, err := NewOfflineRenderer(testScore(t, dir), out, 48000)
	if err != nil {
		t.Fatal(err)
	}
	blocks := 0
	for !r.Done() {
		r.ProcessBlock()
		blocks++
		if blocks > 200 {
			t.Fatal("renderer did not finish")
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("render error: %v", err)
	}
	if p := r.Progress(); p != 1 {
		t.Errorf("progress = %v, want 1", p)
	}
	if r.Peak() <= 0 || r.Peak() > 1 {
		t.Errorf("peak = %v, want in (0, 1]", r.Peak())
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	info, samples, err := wav.DecodeFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.SampleRate != 48000 || info.Channels != 2 || info.BitsPerSample != 24 {
		t.Fatalf("wav info = %+v", info)
	}
	if info.Frames != r.Frames() {
		t.Fatalf("frames = %d, want %d", info.Frames, r.Frames())
	}
	// Early window is audible; the final 0.3 s has decayed well below it.
	rms := func(from, to int) float64 {
		var sum float64
		n := 0
		for i := from * 2; i < to*2 && i < len(samples); i++ {
			sum += float64(samples[i]) * float64(samples[i])
			n++
		}
		if n == 0 {
			return 0
		}
		return math.Sqrt(sum / float64(n))
	}
	early := rms(0, 2400)
	late := rms(info.Frames-14400, info.Frames)
	if early < 0.01 {
		t.Fatalf("early rms = %v, want audible audio", early)
	}
	if late > early/10 {
		t.Fatalf("late rms %v not decayed vs early %v", late, early)
	}
}

// TestEngineEQ checks that the score DSL's per-track EQ is applied during
// engine rendering: a -12 dB peaking cut at the note's fundamental and an
// 800 Hz highpass both attenuate exactly as the RBJ curves predict.
func TestEngineEQ(t *testing.T) {
	pure := map[string]float32{"breath_noise": 0, "vibrato_depth": 0, "brightness": 0}
	notes := []score.NoteEvent{{Time: 0, Duration: 0.6, Pitch: 69, Velocity: 1}}
	base := func(eq score.TrackEQ) *score.Score {
		return &score.Score{
			Metadata: score.Metadata{Title: "EQ", BPM: 120, TimeSignature: "4/4"},
			Tracks: []score.Track{{
				ID: "t", Name: "T", Pan: 0, Volume: 1, EQ: eq,
				Instrument: score.InstrumentDef{Type: "ocarina", Parameters: pure},
				Notes:      notes,
			}},
		}
	}
	// RMS of the master between 0.15 s and 0.5 s (attack is over, the
	// note still sustains).
	rms := func(s *score.Score) float64 {
		eng, err := New(s, 48000, 1024)
		if err != nil {
			t.Fatal(err)
		}
		eng.Mixer().SetReverbWet(0)
		var sum float64
		var n int
		seen := 0
		sink := func(master []float32) {
			for i := 0; i+1 < len(master); i += 2 {
				if seen >= 7200 {
					sum += float64(master[i]) * float64(master[i])
					n++
				}
				seen++
			}
		}
		eng.ProcessFrames(24000, sink)
		return math.Sqrt(sum / float64(n))
	}

	bypass := rms(base(score.TrackEQ{}))
	sculpt := rms(base(score.TrackEQ{MidPeak: score.EQBand{Freq: 440, Gain: -12, Q: 0.5}}))
	if ratio := sculpt / bypass; math.Abs(float64(ratio)-0.251) > 0.05 {
		t.Errorf("peaking -12 dB at the fundamental: rms ratio %v, want ~0.251", ratio)
	}
	hpf := rms(base(score.TrackEQ{HPF: 800}))
	// Butterworth HP at 440 Hz with fc 800: 1/sqrt(1+(800/440)^4) ~ 0.29.
	if ratio := hpf / bypass; math.Abs(float64(ratio)-0.29) > 0.06 {
		t.Errorf("HPF 800 on a 440 Hz note: rms ratio %v, want ~0.29", ratio)
	}
	// A score with EQ must still validate and build through New().
	if err := score.Validate(base(score.TrackEQ{HPF: 80, MidPeak: score.EQBand{Freq: 300, Gain: -3, Q: 1.2}})); err != nil {
		t.Fatalf("EQ score rejected: %v", err)
	}
}
