// Package synth implements a polyphonic virtual-analog subtractive
// synthesizer with no sample files and zero disk footprint: two
// variable-waveform oscillators per voice through a resonant one-pole
// lowpass under an ADSR envelope. Sixteen fixed voices with oldest-note
// stealing keep the per-block render loop allocation-free, and all
// randomness-free math makes renders deterministic for a given score.
package synth

import (
	"math"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// Synth satisfies the instruments.Instrument contract.
var _ instruments.Instrument = (*Synth)(nil)

// Synth voices the per-note payload: envelope overrides and expression.
// Vibrato payloads are accepted and ignored; the voice has no LFO.
var _ instruments.ParamsVoicer = (*Synth)(nil)

// Synth accepts narrow per-note envelope overrides for compatibility.
var _ instruments.EnvelopeVoicer = (*Synth)(nil)

// MaxVoices caps polyphony: with all voices busy the oldest sounding
// voice is stolen, mirroring the sampler's fixed-voice pattern.
const MaxVoices = 16

// Waveform selects an oscillator shape.
type Waveform int

const (
	// WaveSaw is a naive band-unlimited sawtooth, 2·frac−1.
	WaveSaw Waveform = iota
	// WaveSquare is a ±1 square wave switching at half phase.
	WaveSquare
	// WaveSine is a table-lookup sine.
	WaveSine
)

// Parameter defaults and ranges; SetParameters clamps into them.
const (
	defaultWaveform  = WaveSaw
	defaultCutoff    = 4000.0 // Hz
	defaultResonance = 0.2    // 0..1 edge emphasis
	defaultAttack    = 0.005  // s, [0.0005, 2]
	defaultDecay     = 0.1    // s, [0.001, 2]
	defaultSustain   = 0.8    // level, [0, 1]
	defaultRelease   = 0.2    // s, [0.005, 10]
	defaultDetune    = 0.0    // semitones between oscillators, [-12, 12]

	minCutoff = 20.0
	maxCutoff = 20000.0
	maxRes    = 0.8 // feedback stability bound for the resonant stage
)

// voice is one monophonic subtractive voice: two phase accumulators, a
// resonant one-pole lowpass state, and an ADSR envelope. All state is
// owned up front; rendering allocates nothing.
type voice struct {
	active   bool
	pitch    int
	velocity float32
	freq     float64 // Hz at note-on
	phase1   float64 // oscillator cycles [0,1)
	phase2   float64
	filtY    float64 // lowpass state
	filtPrev float64 // previous state, feeds the resonance term
	env      *dsp.Envelope
	start    float64 // trigger clock for oldest-steal ordering
	expr     float32 // per-note expression gain
}

// Synth is a 16-voice polyphonic subtractive synthesizer. Parameters
// apply at setup time; voices capture rate-dependent coefficients when
// retuned so the audio loop only advances accumulators.
type Synth struct {
	sampleRate float64
	voices     [MaxVoices]voice
	envs       [MaxVoices]dsp.Envelope
	clock      float64

	wave1, wave2      Waveform
	cutoff, resonance float32
	attack, decay     float64
	sustain, release  float64
	detune            float32
	filterAlpha       float64 // one-pole coefficient for cutoff
	filterRes         float64 // resonance feedback amount
}

// New returns a synth rendering at sampleRate Hz with default
// parameters. Voices and envelopes are pre-allocated.
func New(sampleRate float64) *Synth {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	s := &Synth{sampleRate: sampleRate}
	s.wave1, s.wave2 = defaultWaveform, defaultWaveform
	s.cutoff, s.resonance = defaultCutoff, defaultResonance
	s.attack, s.decay = defaultAttack, defaultDecay
	s.sustain, s.release = defaultSustain, defaultRelease
	s.detune = defaultDetune
	s.retuneFilter()
	for i := range s.voices {
		s.voices[i].env = &s.envs[i]
		s.voices[i].env.SetParameters(s.attack, s.decay, s.sustain, s.release)
	}
	return s
}

// retuneFilter precomputes the one-pole coefficient (a = 1−exp(−2π·fc/fs)
// in Hz) and resonance feedback from the cutoff and resonance knobs.
func (s *Synth) retuneFilter() {
	fc := float64(s.cutoff)
	if fc < minCutoff {
		fc = minCutoff
	}
	if fc > maxCutoff {
		fc = maxCutoff
	}
	s.filterAlpha = 1 - math.Exp(-2*math.Pi*fc/s.sampleRate)
	if s.filterAlpha > 1 {
		s.filterAlpha = 1
	}
	r := float64(s.resonance)
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	s.filterRes = r * maxRes
}

// waveParam decodes a waveform selector: 0 saw, 1 square, 2 sine;
// out-of-range values fall back to saw.
func waveParam(v float32) Waveform {
	switch int(math.Round(float64(v))) {
	case 1:
		return WaveSquare
	case 2:
		return WaveSine
	default:
		return WaveSaw
	}
}

// SetParameters applies recognized knobs, clamped:
//
//	waveform1, waveform2  0/1/2 = saw/square/sine
//	cutoff                20..20000 Hz one-pole lowpass cutoff
//	resonance             0..1 edge emphasis around cutoff
//	attack                0.0005..2 s, decay 0.001..2 s
//	sustain               0..1 level, release 0.005..10 s
//	detune                -12..12 semitones between oscillators
//
// Unknown keys are ignored. Retuning recomputes filter coefficients
// and envelope times once, outside the audio loop.
func (s *Synth) SetParameters(params map[string]float32) {
	if v, ok := params["waveform1"]; ok {
		s.wave1 = waveParam(v)
	}
	if v, ok := params["waveform2"]; ok {
		s.wave2 = waveParam(v)
	}
	if v, ok := params["cutoff"]; ok {
		s.cutoff = dsp.ClampF32(v, minCutoff, maxCutoff)
	}
	if v, ok := params["resonance"]; ok {
		s.resonance = dsp.ClampF32(v, 0, 1)
	}
	if v, ok := params["attack"]; ok {
		s.attack = float64(dsp.ClampF32(v, 0.0005, 2))
	}
	if v, ok := params["decay"]; ok {
		s.decay = float64(dsp.ClampF32(v, 0.001, 2))
	}
	if v, ok := params["sustain"]; ok {
		s.sustain = float64(dsp.ClampF32(v, 0, 1))
	}
	if v, ok := params["release"]; ok {
		s.release = float64(dsp.ClampF32(v, 0.005, 10))
	}
	if v, ok := params["detune"]; ok {
		s.detune = dsp.ClampF32(v, -12, 12)
	}
	s.retuneFilter()
	for i := range s.voices {
		s.voices[i].env.SetParameters(s.attack, s.decay, s.sustain, s.release)
	}
}

// midiFreq converts a MIDI note number to Hz (69 = A440), clamped to
// the valid range.
func midiFreq(pitch int) float64 {
	if pitch < 0 {
		pitch = 0
	}
	if pitch > 127 {
		pitch = 127
	}
	return 440 * math.Pow(2, float64(pitch-69)/12)
}

// oscWave reads one oscillator sample at phase cycles.
func oscWave(w Waveform, phase float64) float64 {
	frac := phase - math.Floor(phase)
	switch w {
	case WaveSquare:
		if frac < 0.5 {
			return 1
		}
		return -1
	case WaveSine:
		return float64(dsp.SineFast(frac))
	default:
		return 2*frac - 1
	}
}

// NoteOn starts a note with a plain payload.
func (s *Synth) NoteOn(pitch int, velocity float32) {
	s.NoteOnParams(pitch, velocity, instruments.NoteParams{})
}

// NoteOnEx applies per-note envelope overrides with full expression.
func (s *Synth) NoteOnEx(pitch int, velocity float32, env instruments.EnvelopeOverride) {
	s.NoteOnParams(pitch, velocity, instruments.NoteParams{Env: env})
}

// NoteOnParams starts a note with envelope overrides and expression
// gain; vibrato payloads are accepted and ignored. Velocity clamps to
// [0, 1]; expression defaults to full volume.
func (s *Synth) NoteOnParams(pitch int, velocity float32, params instruments.NoteParams) {
	v := s.allocVoice()
	vel := dsp.ClampF32(velocity, 0, 1)
	freq := midiFreq(pitch)
	v.pitch = pitch
	v.velocity = vel
	v.phase1, v.phase2 = 0, 0
	v.filtY, v.filtPrev = 0, 0
	v.expr = 1
	if params.HasExpression {
		e := float64(params.Expression)
		if math.IsNaN(e) {
			e = 1
		} else if e < 0 {
			e = 0
		} else if e > 1 {
			e = 1
		}
		v.expr = float32(e)
	}
	attack, decay, release := s.attack, s.decay, s.release
	if env := params.Env; env.HasAttack && env.Attack > 0 {
		attack = env.Attack
	}
	if env := params.Env; env.HasDecay && env.Decay > 0 {
		decay = env.Decay
	}
	if env := params.Env; env.HasRelease && env.Release > 0 {
		release = env.Release
	}
	v.freq = freq
	v.env.SetParameters(attack, decay, s.sustain, release)
	v.env.Trigger()
	v.start = s.clock
	v.active = true
	s.clock++
}

// NoteOff releases every sounding voice at the pitch.
func (s *Synth) NoteOff(pitch int) {
	for i := range s.voices {
		v := &s.voices[i]
		if v.active && v.pitch == pitch {
			v.env.Release()
		}
	}
}

// ActiveVoices reports how many voices are currently sounding.
func (s *Synth) ActiveVoices() int {
	n := 0
	for i := range s.voices {
		if s.voices[i].active {
			n++
		}
	}
	return n
}

// allocVoice returns a free voice, else the oldest sounding one for
// stealing. Deterministic: ties break by voice index.
func (s *Synth) allocVoice() *voice {
	for i := range s.voices {
		if !s.voices[i].active {
			return &s.voices[i]
		}
	}
	oldest := &s.voices[0]
	for i := range s.voices[1:] {
		if s.voices[i+1].start < oldest.start {
			oldest = &s.voices[i+1]
		}
	}
	return oldest
}

// Process renders the mix of all sounding voices into both channels.
// Inactive voices cost one branch; finished envelopes free their voice.
// It advances only pre-allocated state and allocates nothing.
func (s *Synth) Process(buffer *dsp.Buffer, deltaTime float64) {
	if buffer == nil || deltaTime <= 0 {
		return
	}
	left, right := buffer.Left, buffer.Right
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	detuneRatio := math.Pow(2, float64(s.detune)/12) // semitones to ratio
	for i := range s.voices {
		v := &s.voices[i]
		if !v.active {
			continue
		}
		freq := v.freq
		inc1 := freq * deltaTime
		inc2 := freq * detuneRatio * deltaTime
		vel := float64(v.velocity) * float64(v.expr)
		for j := 0; j < n; j++ {
			x := 0.5 * (oscWave(s.wave1, v.phase1) + oscWave(s.wave2, v.phase2))
			v.phase1 += inc1
			if v.phase1 >= 1 {
				v.phase1 -= math.Floor(v.phase1)
			}
			v.phase2 += inc2
			if v.phase2 >= 1 {
				v.phase2 -= math.Floor(v.phase2)
			}
			// Resonant one-pole lowpass: the feedback term adds edge
			// near cutoff and vanishes with resonance at 0, leaving a
			// pure one-pole.
			old := v.filtY
			v.filtY += s.filterAlpha*(x-old) + s.filterRes*(old-v.filtPrev)
			v.filtPrev = old
			out := float32(v.filtY * v.env.Next(deltaTime) * vel)
			left[j] += out
			right[j] += out
		}
		if v.env.Stage() == dsp.StageIdle {
			v.active = false
		}
	}
}
