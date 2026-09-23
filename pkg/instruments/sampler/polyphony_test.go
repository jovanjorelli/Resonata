package sampler

import (
	"testing"
)

// npSampler builds a one-region sampler with the given note_polyphony
// over a constant sample.
func npSampler(notePoly int) *Sampler {
	sd := &SampleData{Samples: make([]float32, 48000), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	rg := newRegion()
	rg.SamplePath, rg.Sample = "a.wav", sd
	rg.NotePolyphony = notePoly
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{rg}})
	return s
}

// activePitches returns the pitches of all sounding voices in slot order.
func activePitches(s *Sampler) []int {
	var out []int
	for i := range s.voices {
		if s.voices[i].Active {
			out = append(out, s.voices[i].Pitch)
		}
	}
	return out
}

// TestNotePolyphonyParsing inherits note_polyphony down the
// global-group-region chain with region override winning.
func TestNotePolyphonyParsing(t *testing.T) {
	f, err := ParseSFZ([]byte("<group>\nnote_polyphony=2\n<region>\nsample=a.wav\nkey=60\n<region>\nsample=b.wav\nkey=61\nnote_polyphony=1\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Regions) != 2 {
		t.Fatalf("regions = %d, want 2", len(f.Regions))
	}
	if f.Regions[0].NotePolyphony != 2 {
		t.Errorf("group inheritance = %d, want 2", f.Regions[0].NotePolyphony)
	}
	if f.Regions[1].NotePolyphony != 1 {
		t.Errorf("region override = %d, want 1", f.Regions[1].NotePolyphony)
	}
}

// TestNotePolyphonyLimit fades the oldest voice past a limit of one:
// the second trigger steals nothing, frees the first over 10 ms, and
// plays the new note fresh.
func TestNotePolyphonyLimit(t *testing.T) {
	s := npSampler(1)
	s.NoteOn(60, 0.9)
	s.NoteOn(60, 0.9)
	if !s.voices[0].isStealing || !s.voices[0].stealFree {
		t.Fatalf("first voice not fading to free: %+v", s.voices[0])
	}
	if s.voices[0].fadeStep != autoFadeStep {
		t.Fatalf("fade step = %v, want 10 ms auto fade", s.voices[0].fadeStep)
	}
	if !s.voices[1].Active || s.voices[1].isStealing {
		t.Fatalf("second voice not playing fresh: %+v", s.voices[1])
	}
	// The first voice melts to zero over 480 frames, then frees.
	mono := make([]float32, 512)
	s.voices[0].Process(mono, 512.0/testRate)
	if s.voices[0].Active {
		t.Fatal("faded voice still active after 512 frames")
	}
	if a := abs32(mono[479]); a > 0.05 {
		t.Errorf("frame 479 = %v, want near zero at fade end", mono[479])
	}
	if m := maxStep(mono, 0); m > 0.05 {
		t.Errorf("fade max step = %v, want smooth", m)
	}
	for i := 480; i < 512; i++ {
		if mono[i] != 0 {
			t.Fatalf("frame %d = %v past the fade, want exact silence", i, mono[i])
		}
	}
}

// TestNotePolyphonyTwo keeps two sounding and fades only the oldest.
func TestNotePolyphonyTwo(t *testing.T) {
	s := npSampler(2)
	s.NoteOn(60, 0.9)
	s.NoteOn(60, 0.9)
	s.NoteOn(60, 0.9)
	if !s.voices[0].isStealing {
		t.Error("first voice not fading after the third trigger")
	}
	for _, i := range []int{1, 2} {
		if !s.voices[i].Active || s.voices[i].isStealing {
			t.Errorf("voice %d not sounding fresh: %+v", i, s.voices[i])
		}
	}
}

// TestNotePolyphonyUnlimited stacks four instances with no limit:
// earlier takes fade (anti-chorus) but stay active until rendered,
// so all four sound simultaneously.
func TestNotePolyphonyUnlimited(t *testing.T) {
	s := npSampler(0)
	for k := 0; k < 4; k++ {
		s.NoteOn(60, 0.9)
	}
	if got := len(activePitches(s)); got != 4 {
		t.Fatalf("active voices = %d, want 4", got)
	}
	if s.voices[3].isStealing {
		t.Fatalf("newest voice fading: %+v", s.voices[3])
	}
}

// TestAutoFadeUnlimited fades the old instance even with no limit.
func TestAutoFadeUnlimited(t *testing.T) {
	s := npSampler(0)
	s.NoteOn(60, 0.9)
	s.NoteOn(60, 0.9)
	if !s.voices[0].isStealing || !s.voices[0].stealFree {
		t.Fatalf("first voice not auto-fading: %+v", s.voices[0])
	}
	if s.voices[0].fadeStep != autoFadeStep {
		t.Fatalf("fade step = %v, want 10 ms", s.voices[0].fadeStep)
	}
	if !s.voices[1].Active || s.voices[1].isStealing {
		t.Fatalf("second voice not fresh: %+v", s.voices[1])
	}
}

// TestAutoFadeDifferentPitch leaves other pitches alone.
func TestAutoFadeDifferentPitch(t *testing.T) {
	s := npSampler(0)
	s.NoteOn(60, 0.9)
	s.NoteOn(64, 0.9)
	if s.voices[0].isStealing {
		t.Fatalf("C4 voice faded by an E4 trigger: %+v", s.voices[0])
	}
	if !s.voices[0].Active || !s.voices[1].Active {
		t.Fatal("both voices should sound")
	}
}

// TestNotePolyphonyZeroAlloc hammers one pitch under a limit of one
// with zero heap allocations across note-on, auto-fade, and tracking.
func TestNotePolyphonyZeroAlloc(t *testing.T) {
	s := npSampler(1)
	if allocs := testing.AllocsPerRun(1000, func() {
		s.NoteOn(60, 0.9)
	}); allocs != 0 {
		t.Fatalf("limited NoteOn allocates %v times, want 0", allocs)
	}
}

// TestAutoFadeInterruptsLoopCrossfade retriggers a looping voice mid
// seam: the auto-fade takes over, output stays smooth, and the old
// voice frees while the new one sustains.
func TestAutoFadeInterruptsLoopCrossfade(t *testing.T) {
	s := loopTestSampler(t, 110, 8000, 0, 4910)
	s.NoteOn(69, 1.0)
	// Park the voice inside the crossfade region.
	v := soundingVoice(t, s)
	for k := 0; k < 4910-384; k += 256 {
		mono := make([]float32, 256)
		v.Process(mono, 256.0/testRate)
	}
	mono := make([]float32, 64)
	v.Process(mono, 64.0/testRate)
	if !v.inCrossfade {
		t.Fatalf("voice not in crossfade before retrigger (phase %v)", v.Phase)
	}
	s.NoteOn(69, 1.0) // same region, unlimited: auto-fades the looper
	if !v.isStealing || !v.stealFree {
		t.Fatalf("looping voice not auto-fading: %+v", v)
	}
	out := renderMono(s, 0.05) // old fades 10 ms, new sustains
	if !finiteFrames(out) {
		t.Fatal("non-finite output")
	}
	peak := peakOf(out)
	if peak < 0.3 {
		t.Fatalf("peak = %v, new voice should sustain", peak)
	}
	if m := maxStep(out, 600); m > 0.05*peak {
		t.Errorf("max step = %v, want smooth handoff below 5%% of peak", m)
	}
	if v.Active {
		t.Fatal("old voice still active well past its 10 ms fade")
	}
}
