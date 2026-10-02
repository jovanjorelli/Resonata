package sampler

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
)

// loopTestSampler builds a sampler holding a low-frequency sine with a
// loop_continuous region [loopStart, loopEnd). sampleRate equals the
// render rate so pitch 69 plays at rate 1.0.
func loopTestSampler(t *testing.T, freq float64, total, loopStart, loopEnd int) *Sampler {
	t.Helper()
	sine := make([]float32, total)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / testRate))
	}
	f := &SFZFile{Regions: []Region{{
		SamplePath: "loop.wav", LoKey: 0, HiKey: 127, PitchKeyCenter: 69,
		LoVel: 0, HiVel: 127, LoopMode: LoopContinuous,
		LoRand: 0, HiRand: 1,
		Sample: &SampleData{Samples: sine, SampleRate: testRate, Channels: 1,
			LoopStart: loopStart, LoopEnd: loopEnd},
	}}}
	s := New(testRate)
	s.Load(f)
	return s
}

// renderMono renders d seconds through the sampler and returns the left
// channel frames.
func renderMono(s *Sampler, d float64) []float32 {
	dt := 1.0 / testRate
	buf := dsp.NewStereoBuffer(512)
	frames := int(d * testRate)
	out := make([]float32, 0, frames)
	var scratch [512]float32
	_ = scratch
	for done := 0; done < frames; {
		n := min(512, frames-done)
		buf.SetLen(n)
		s.Process(buf, dt)
		out = append(out, buf.Left[:n]...)
		done += n
	}
	return out
}

func finiteFrames(out []float32) bool {
	for _, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}

func peakOf(out []float32) float32 {
	var peak float32
	for _, v := range out {
		if a := abs32(v); a > peak {
			peak = a
		}
	}
	return peak
}

func maxStep(out []float32, from int) float32 {
	var m float32
	for i := from + 1; i < len(out); i++ {
		if d := abs32(out[i] - out[i-1]); d > m {
			m = d
		}
	}
	return m
}

// TestLoopCrossfadeClick renders a sustained looped note over many loop
// cycles with loop points cut mid-cycle (a hard wrap would jump by ~100%
// of peak) and asserts the seam stays continuous below 5% of peak, both
// globally and in a window around every loop boundary.
func TestLoopCrossfadeClick(t *testing.T) {
	const loopStart, loopEnd = 0, 4910 // 110 Hz sine: sample 4909 is near +peak, sample 0 is zero
	s := loopTestSampler(t, 110, 8000, loopStart, loopEnd)
	s.NoteOn(69, 1.0)
	out := renderMono(s, 2.0) // ~96000 frames, ~20 loop seams
	if !finiteFrames(out) {
		t.Fatal("non-finite output")
	}
	peak := peakOf(out)
	if peak < 0.5 {
		t.Fatalf("peak = %v, looping voice should sustain", peak)
	}
	threshold := 0.05 * peak
	// Global continuity past the 5 ms attack.
	if m := maxStep(out, 600); m > threshold {
		t.Errorf("max consecutive step = %v, want below 5%% of peak %v", m, threshold)
	}
	// Per-boundary windows. First seam at loopEnd frames; after each
	// crossfade the phase resumes at loopStart+384, so later seams
	// repeat every loopEnd-loopStart-384 frames at rate 1.0.
	xfLen := loopCrossfadeSamples
	first := loopEnd
	period := loopEnd - loopStart - xfLen
	checked := 0
	for b := first; b+2 < len(out); b += period {
		worst := float32(0)
		for i := b - 2; i <= b+2; i++ {
			if i > 600 && i < len(out) {
				if d := abs32(out[i] - out[i-1]); d > worst {
					worst = d
				}
			}
		}
		if worst > threshold {
			t.Errorf("seam near frame %d: step = %v, want below %v", b, worst, threshold)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("checked %d seams, want at least 10", checked)
	}
}

// TestLoopShortFallback renders a loop shorter than the 384-frame
// crossfade and asserts the voice silently falls back to a hard wrap
// with valid sustained output and never enters the crossfade state.
func TestLoopShortFallback(t *testing.T) {
	s := loopTestSampler(t, 220, testRate, 1000, 1100) // 100-frame loop
	s.NoteOn(69, 0.9)
	out := renderMono(s, 1.0)
	if !finiteFrames(out) {
		t.Fatal("non-finite output")
	}
	if peak := peakOf(out); peak < 0.3 {
		t.Fatalf("peak = %v, short-loop voice should sustain", peak)
	}
	if s.ActiveVoices() != 1 {
		t.Fatalf("voices = %d, short-loop voice stopped", s.ActiveVoices())
	}
	for i := range s.voices {
		if s.voices[i].Active && s.voices[i].inCrossfade {
			t.Fatal("short loop entered crossfade, want hard-wrap fallback")
		}
	}
}

// fillSustain fills all 16 voices and renders until every envelope has
// settled into sustain.
func fillSustain(t *testing.T, s *Sampler) {
	t.Helper()
	for p := 60; p < 60+MaxVoices; p++ {
		s.NoteOn(p, 0.8)
		renderMono(s, 1.0/testRate)
	}
	renderMono(s, 0.5)
	if s.ActiveVoices() != MaxVoices {
		t.Fatalf("voices = %d, want %d", s.ActiveVoices(), MaxVoices)
	}
}

// TestVoiceStealFade steals the oldest voice and asserts the handoff is
// seamless: the old note fades to zero over 240 frames, consecutive
// steps stay below the click threshold, and the pending note then sounds
// with the new pitch.
func TestVoiceStealFade(t *testing.T) {
	s, _ := newTestSampler(t)
	fillSustain(t, s)

	s.NoteOn(90, 0.9) // steals voices[0] (pitch 60)
	stolen := &s.voices[0]
	if stolen.Pitch != 90 {
		t.Fatalf("stolen voice pitch = %d, want 90", stolen.Pitch)
	}
	if !stolen.isStealing || stolen.stealFadeProgress != 0 {
		t.Fatalf("stolen voice not in fresh fade: %+v", stolen)
	}
	if s.ActiveVoices() != MaxVoices {
		t.Fatalf("voices = %d, want %d during fade", s.ActiveVoices(), MaxVoices)
	}

	// A note-off for the faded old pitch is ignored; a note-off for the
	// pending pitch is stored, not applied yet.
	s.NoteOff(60)
	if stolen.pendingRelease {
		t.Fatal("note-off for the old pitch set pendingRelease")
	}
	s.NoteOff(90)
	if !stolen.pendingRelease {
		t.Fatal("note-off for the pending pitch was not stored")
	}
	// Clear it so the fade-output measurement below is not released.
	stolen.pendingRelease = false

	// Render the stolen voice alone: 240 fade frames plus new-note audio.
	mono := make([]float32, 512)
	stolen.Process(mono, 512.0/testRate)
	if !finiteFrames(mono) {
		t.Fatal("non-finite stolen-voice output")
	}
	peak := peakOf(mono[:240])
	if peak < 0.2 {
		t.Fatalf("fade peak = %v, old note should still sound", peak)
	}
	threshold := 0.05 * peak
	if m := maxStep(mono[:241], 0); m > threshold {
		t.Errorf("fade max step = %v, want below 5%% of peak %v", m, threshold)
	}
	// The fade reaches silence before the handoff: the last fade frames
	// sit near zero (old amplitude 0.8 scaled by 1/240 plus slope).
	if a := abs32(mono[239]); a > 0.05 {
		t.Errorf("frame 239 = %v, want near zero at fade end", mono[239])
	}
	// Handoff continuity: the new attack starts from zero, so the seam
	// between the faded old note and the fresh note stays small.
	if d := abs32(mono[240] - mono[239]); d > threshold {
		t.Errorf("handoff step = %v, want below %v", d, threshold)
	}
	if stolen.isStealing {
		t.Fatal("voice still stealing after 512 frames (fade is 240)")
	}
	if stolen.Pitch != 90 || !stolen.Active {
		t.Fatalf("voice after handoff = pitch %d active %v", stolen.Pitch, stolen.Active)
	}
	// The new note sounds: its tail carries energy.
	if tail := peakOf(mono[240:]); tail < 0.05 {
		t.Fatalf("new-note tail peak = %v, want audible audio", tail)
	}
}

// TestDenseStealStress triggers 64 fast sixteenth notes without
// note-offs so stealing engages on every note past the 16th, then
// asserts the full mix stays finite with no hard-cut jumps.
func TestDenseStealStress(t *testing.T) {
	s, _ := newTestSampler(t)
	const interval = 0.05 // sixteenth at a driving tempo
	var out []float32
	dt := 1.0 / testRate
	buf := dsp.NewStereoBuffer(512)
	for k := 0; k < 64; k++ {
		s.NoteOn(40+k%32, 0.7) // distinct pitches 40..71, repeated once
		frames := int(interval * testRate)
		for done := 0; done < frames; {
			n := min(512, frames-done)
			buf.SetLen(n)
			s.Process(buf, dt)
			out = append(out, buf.Left[:n]...)
			done += n
		}
		if s.ActiveVoices() > MaxVoices {
			t.Fatalf("voices = %d, exceeds %d", s.ActiveVoices(), MaxVoices)
		}
	}
	out = append(out, renderMono(s, 0.5)...)
	if !finiteFrames(out) {
		t.Fatal("non-finite output")
	}
	// One hard steal jumps by ~0.5 (a full-scale voice cut); the faded
	// mix moves at the polyphonic sine slope (~0.1 typical). A 0.4
	// ceiling separates clicks from music with wide margin.
	if m := maxStep(out, 600); m > 0.4 {
		t.Errorf("max step = %v, want below 0.4 (no hard cuts)", m)
	}
	if peak := peakOf(out); peak < 0.3 {
		t.Fatalf("peak = %v, dense passage should stay loud", peak)
	}
}

// TestArtifactZeroAlloc benchmarks the note-on steal path, the voice
// render path with an engaged loop crossfade, and the steal fade, and
// asserts zero heap allocations throughout.
func TestArtifactZeroAlloc(t *testing.T) {
	// Note-on while stealing: every trigger reuses a sounding voice.
	s, _ := newTestSampler(t)
	fillSustain(t, s)
	if allocs := testing.AllocsPerRun(20, func() {
		s.NoteOn(90, 0.8)
	}); allocs != 0 {
		t.Fatalf("stealing NoteOn allocates %v times, want 0", allocs)
	}

	// Voice render inside the crossfade region: park the phase at the
	// crossfade entry so every benchmark frame mixes two streams.
	looped := loopTestSampler(t, 110, 8000, 0, 4910)
	looped.NoteOn(69, 1.0)
	renderMono(looped, float64(4910-384)/testRate) // arrive at the seam
	var vv *Voice
	for i := range looped.voices {
		if looped.voices[i].Active {
			vv = &looped.voices[i]
		}
	}
	if vv == nil {
		t.Fatal("no active looped voice")
	}
	mono := make([]float32, 256)
	if allocs := testing.AllocsPerRun(20, func() {
		vv.Process(mono, 256.0/testRate)
		// Re-park at the crossfade entry when the loop wraps so the
		// measured path always includes the two-stream mix.
		if !vv.inCrossfade && vv.Phase < float64(4910-384) {
			vv.Phase = float64(4910 - 384)
		}
	}); allocs != 0 {
		t.Fatalf("crossfade Voice.Process allocates %v times, want 0", allocs)
	}

	// Steal-fade render: keep a voice stealing across the measurement by
	// re-triggering whenever the previous handoff completes.
	s2, _ := newTestSampler(t)
	fillSustain(t, s2)
	s2.NoteOn(90, 0.9)
	stolen := &s2.voices[0]
	if allocs := testing.AllocsPerRun(20, func() {
		stolen.Process(mono, 256.0/testRate)
		if !stolen.isStealing {
			s2.NoteOn(91, 0.9)
			for i := range s2.voices {
				if s2.voices[i].isStealing {
					stolen = &s2.voices[i]
					break
				}
			}
		}
	}); allocs != 0 {
		t.Fatalf("steal-fade Voice.Process allocates %v times, want 0", allocs)
	}
}
