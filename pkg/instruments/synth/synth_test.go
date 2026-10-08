package synth

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// TestWaveShapes proves each waveform emits its defining levels.
func TestWaveShapes(t *testing.T) {
	if got := oscWave(WaveSaw, 0); got != -1 {
		t.Fatalf("saw(0) = %v, want -1", got)
	}
	if got := oscWave(WaveSaw, 0.25); got != -0.5 {
		t.Fatalf("saw(0.25) = %v, want -0.5", got)
	}
	if got := oscWave(WaveSquare, 0.25); got != 1 {
		t.Fatalf("square(0.25) = %v, want 1", got)
	}
	if got := oscWave(WaveSquare, 0.75); got != -1 {
		t.Fatalf("square(0.75) = %v, want -1", got)
	}
	if got := oscWave(WaveSine, 0.25); math.Abs(got-1) > 1e-4 {
		t.Fatalf("sine(0.25) = %v, want ~1", got)
	}
	if got := oscWave(WaveSine, 0.75); math.Abs(got+1) > 1e-4 {
		t.Fatalf("sine(0.75) = %v, want ~-1", got)
	}
}

// TestMidiFreq proves concert-pitch mapping and input clamping.
func TestMidiFreq(t *testing.T) {
	if got := midiFreq(69); got != 440 {
		t.Fatalf("69 = %v, want 440", got)
	}
	if got := midiFreq(81); got != 880 {
		t.Fatalf("81 = %v, want 880", got)
	}
	if midiFreq(-5) != midiFreq(0) || midiFreq(200) != midiFreq(127) {
		t.Fatal("out-of-range pitches not clamped")
	}
}

// TestWaveParam proves selector decoding with saw fallback.
func TestWaveParam(t *testing.T) {
	if waveParam(0) != WaveSaw || waveParam(1) != WaveSquare || waveParam(2) != WaveSine {
		t.Fatal("wave selectors misdecoded")
	}
	if waveParam(99) != WaveSaw || waveParam(-3) != WaveSaw {
		t.Fatal("bad selector did not fall back to saw")
	}
}

// TestSetParameters proves knob clamping and unknown-key tolerance.
func TestSetParameters(t *testing.T) {
	s := New(48000)
	s.SetParameters(map[string]float32{
		"waveform1": 1, "waveform2": 2, "cutoff": 800, "resonance": 0.5,
		"attack": 0.01, "decay": 0.2, "sustain": 0.7, "release": 0.4,
		"detune": 4, "mystery": 123,
	})
	if s.wave1 != WaveSquare || s.wave2 != WaveSine {
		t.Fatalf("waves = %v %v", s.wave1, s.wave2)
	}
	if s.cutoff != 800 || s.resonance != 0.5 || s.detune != 4 {
		t.Fatalf("knobs = %v %v %v", s.cutoff, s.resonance, s.detune)
	}
	s.SetParameters(map[string]float32{"cutoff": 1e9, "resonance": -5, "detune": 99, "sustain": 9})
	if s.cutoff != maxCutoff || s.resonance != 0 || s.detune != 12 || s.sustain != 1 {
		t.Fatalf("unclamped = %v %v %v %v", s.cutoff, s.resonance, s.detune, s.sustain)
	}
	s.SetParameters(nil) // must not panic or move knobs
	if s.cutoff != maxCutoff {
		t.Fatal("nil params moved knobs")
	}
}

// render renders n frames of one sounding note and returns peak and RMS.
func renderNote(t *testing.T, s *Synth, pitch int, vel float32, frames int) (float32, float64) {
	t.Helper()
	s.NoteOn(pitch, vel)
	buf := dsp.NewStereoBuffer(frames)
	buf.SetLen(frames)
	s.Process(buf, 1.0/s.sampleRate)
	peak := float32(0)
	sum := 0.0
	for i := range buf.Left {
		for _, v := range []float32{buf.Left[i], buf.Right[i]} {
			if av := absF(v); av > peak {
				peak = av
			}
			sum += float64(v) * float64(v)
		}
	}
	return peak, sum / float64(2*frames)
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestEnvelopeLifecycle proves attack rises, release ends in silence,
// and the voice frees itself.
func TestEnvelopeLifecycle(t *testing.T) {
	s := New(48000)
	s.SetParameters(map[string]float32{"attack": 0.01, "decay": 0.05, "sustain": 0.6, "release": 0.05})
	s.NoteOn(69, 0.9)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
	buf := dsp.NewStereoBuffer(480)
	buf.SetLen(480)
	s.Process(buf, 1.0/48000)
	early := peakOf(buf)
	s.NoteOff(69)
	for i := 0; i < 2000 && s.ActiveVoices() > 0; i++ {
		s.Process(buf, 1.0/48000)
	}
	if got := s.ActiveVoices(); got != 0 {
		t.Fatalf("voices = %d after release, want 0", got)
	}
	if early <= 0 {
		t.Fatal("no audio while sounding")
	}
	fresh := dsp.NewStereoBuffer(480)
	fresh.SetLen(480)
	s.Process(fresh, 1.0/48000)
	if peakOf(fresh) != 0 {
		t.Fatal("audio after voice freed")
	}
}

func peakOf(buf *dsp.StereoBuffer) float32 {
	peak := float32(0)
	for i := range buf.Left {
		if a := absF(buf.Left[i]); a > peak {
			peak = a
		}
		if a := absF(buf.Right[i]); a > peak {
			peak = a
		}
	}
	return peak
}

// TestFilterCutoff proves a low cutoff attenuates the fundamental while
// a wide-open cutoff passes it.
func TestFilterCutoff(t *testing.T) {
	dark := New(48000)
	dark.SetParameters(map[string]float32{"cutoff": 100, "resonance": 0, "attack": 0.001, "decay": 0.001, "sustain": 1.0})
	_, rmsDark := renderNote(t, dark, 69, 0.9, 48000)
	open := New(48000)
	open.SetParameters(map[string]float32{"cutoff": 18000, "resonance": 0, "attack": 0.001, "decay": 0.001, "sustain": 1.0})
	_, rmsOpen := renderNote(t, open, 69, 0.9, 48000)
	if !(rmsDark < 0.25*rmsOpen) {
		t.Fatalf("dark/open RMS = %v/%v, want strong attenuation", rmsDark, rmsOpen)
	}
}

// TestResonanceBounded proves maximum resonance stays finite.
func TestResonanceBounded(t *testing.T) {
	s := New(48000)
	s.SetParameters(map[string]float32{"cutoff": 2000, "resonance": 1, "attack": 0.001, "sustain": 1.0})
	peak, _ := renderNote(t, s, 69, 1.0, 96000)
	if math.IsNaN(float64(peak)) || math.IsInf(float64(peak), 0) || peak > 4 {
		t.Fatalf("resonant peak = %v, want finite and bounded", peak)
	}
}

// TestDetuneChangesSound proves detune audibly alters output while
// staying deterministic.
func TestDetuneChangesSound(t *testing.T) {
	mk := func(det float32) []float32 {
		s := New(48000)
		s.SetParameters(map[string]float32{"detune": det, "attack": 0.001, "sustain": 1.0})
		s.NoteOn(69, 0.9)
		buf := dsp.NewStereoBuffer(1024)
		buf.SetLen(1024)
		s.Process(buf, 1.0/48000)
		return append([]float32{}, buf.Left...)
	}
	a, b := mk(0), mk(7)
	same := true
	for i := range a {
		if a[i] != b[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("detune did not change output")
	}
	c := mk(7)
	for i := range b {
		if b[i] != c[i] {
			t.Fatal("detune render not deterministic")
		}
	}
}

// TestVoiceStealOldest proves the 17th note steals the oldest voice and
// polyphony never exceeds 16.
func TestVoiceStealOldest(t *testing.T) {
	s := New(48000)
	for p := 0; p < MaxVoices; p++ {
		s.NoteOn(60+p%8, 0.8)
	}
	if got := s.ActiveVoices(); got != MaxVoices {
		t.Fatalf("voices = %d, want %d", got, MaxVoices)
	}
	s.NoteOn(72, 0.9)
	if got := s.ActiveVoices(); got != MaxVoices {
		t.Fatalf("voices = %d after steal, want %d", got, MaxVoices)
	}
	if s.voices[0].pitch != 72 {
		t.Fatalf("voice[0] pitch = %d, want stolen 72", s.voices[0].pitch)
	}
}

// TestExpressionZero proves zero expression silences the voice.
func TestExpressionZero(t *testing.T) {
	s := New(48000)
	s.NoteOnParams(69, 0.9, instruments.NoteParams{HasExpression: true, Expression: 0})
	buf := dsp.NewStereoBuffer(512)
	buf.SetLen(512)
	s.Process(buf, 1.0/48000)
	if peakOf(buf) != 0 {
		t.Fatal("zero expression still audible")
	}
}

// TestAttackOverride proves a slow attack starts quieter than default.
func TestAttackOverride(t *testing.T) {
	fast := New(48000)
	fast.NoteOn(69, 0.9)
	buf := dsp.NewStereoBuffer(240)
	buf.SetLen(240)
	fast.Process(buf, 240.0/48000)
	pFast := peakOf(buf)
	slow := New(48000)
	slow.NoteOnEx(69, 0.9, instruments.EnvelopeOverride{Attack: 0.5, HasAttack: true})
	buf2 := dsp.NewStereoBuffer(240)
	buf2.SetLen(240)
	slow.Process(buf2, 240.0/48000)
	if pSlow := peakOf(buf2); !(pSlow < pFast) {
		t.Fatalf("slow attack peak %v not below %v", pSlow, pFast)
	}
}

// TestNoteOffUnknownPitch proves stray note-offs are safe no-ops.
func TestNoteOffUnknownPitch(t *testing.T) {
	s := New(48000)
	s.NoteOn(60, 0.8)
	s.NoteOff(61)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
	s.NoteOff(60)
}

// BenchmarkNoteOn measures trigger cost: voice lookup plus envelope
// setup over pre-allocated state.
func BenchmarkNoteOn(b *testing.B) {
	s := New(48000)
	pitch := 60
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.NoteOn(pitch, 0.8)
		pitch++
		if pitch > 72 {
			pitch = 60
		}
	}
}

// TestZeroAlloc proves note-on and steady-state processing allocate
// nothing: voices, envelopes, and phases are pre-owned.
func TestZeroAlloc(t *testing.T) {
	s := New(48000)
	if a := testing.AllocsPerRun(100, func() { s.NoteOn(60, 0.8) }); a != 0 {
		t.Fatalf("NoteOn allocs = %v", a)
	}
	buf := dsp.NewStereoBuffer(512)
	buf.SetLen(512)
	s.NoteOn(60, 0.8)
	if a := testing.AllocsPerRun(50, func() { s.Process(buf, 1.0/48000) }); a != 0 {
		t.Fatalf("Process allocs = %v", a)
	}
}
