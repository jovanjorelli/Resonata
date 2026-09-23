// Package ocarina implements a physical model of the ocarina: a
// Helmholtz resonator excited by a turbulent air jet, with vibrato and a
// breath envelope.
package ocarina

import (
	"math"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// Ocarina satisfies the instruments.Instrument contract.
var _ instruments.Instrument = (*Ocarina)(nil)

// Ocarina voices the full per-note payload: envelope overrides,
// vibrato, and expression.
var _ instruments.ParamsVoicer = (*Ocarina)(nil)

// Physical constants and default geometry of a soprano ocarina.
const (
	speedOfSound = 343.0 // m/s in air at ~20°C
	twoPi        = 2 * math.Pi

	defaultVolume     = 2.5e-4 // m³ cavity volume V (~250 cm³)
	defaultNeckLength = 0.015  // m effective neck length L_eff, end correction included

	// Harmonic balance of the resonator response: 2nd partial at -12 dB,
	// 3rd at -18 dB. Ocarinas have characteristically weak upper partials.
	harmonic2Gain = 0.25118864315095801 // 10^(-12/20)
	harmonic3Gain = 0.12589254117941673 // 10^(-18/20)

	// Breath formants: parallel bandpass filters on white noise modelling
	// air-jet turbulence.
	formantLow  = 2000.0
	formantMid  = 4000.0
	formantHigh = 8000.0
	formantQ    = 2.0

	breathMix = 0.3 // noise blend factor in the output stage

	// Breath envelope: 50 ms buildup, 100 ms air decay, flat sustain.
	attackTime   = 0.05
	releaseTime  = 0.10
	sustainLevel = 1.0
	decayTime    = 1e-6 // effectively instant: attack goes straight to sustain
)

// Default parameter values; SetParameters clamps into the documented
// ranges around them.
const (
	defaultBreathNoise  float32 = 0.3
	defaultVibratoRate  float32 = 5.0
	defaultVibratoDepth float32 = 0.02
	defaultBrightness   float32 = 1.0
)

// Ocarina is a monophonic wind instrument. All synthesis state persists
// across Process calls and rendering performs no allocations.
type Ocarina struct {
	// Cavity geometry in SI units.
	volume     float64
	neckLength float64
	neckArea   float64 // open-hole area S derived from the active pitch
	freq       float64 // resulting Helmholtz resonance in Hz

	// Voicing state.
	note     int // active MIDI pitch, -1 when no note is held
	velocity float32

	// Persistent synthesis state.
	tonePhase    float64 // cycles [0,1)
	vibratoPhase float64 // cycles [0,1)
	noise        *dsp.Noise
	formants     [3]*dsp.Biquad // 2/4/8 kHz turbulence bandpasses
	env          *dsp.Envelope

	// Exposed parameters.
	breathNoise  float32
	vibratoRate  float32
	vibratoDepth float32
	brightness   float32
	expr         float32 // score expression gain, 1.0 is full volume
}

// New returns an ocarina with default geometry and parameters. sampleRate
// tunes the breath formant filters.
func New(sampleRate float64) *Ocarina {
	o := &Ocarina{
		volume:       defaultVolume,
		neckLength:   defaultNeckLength,
		note:         -1,
		noise:        dsp.NewNoise(0),
		env:          dsp.NewEnvelope(attackTime, decayTime, sustainLevel, releaseTime),
		breathNoise:  defaultBreathNoise,
		vibratoRate:  defaultVibratoRate,
		vibratoDepth: defaultVibratoDepth,
		brightness:   defaultBrightness,
		expr:         1,
	}
	o.formants[0] = dsp.NewBiquad(dsp.Bandpass, formantLow, formantQ, sampleRate)
	o.formants[1] = dsp.NewBiquad(dsp.Bandpass, formantMid, formantQ, sampleRate)
	o.formants[2] = dsp.NewBiquad(dsp.Bandpass, formantHigh, formantQ, sampleRate)
	return o
}

// HelmholtzFrequency returns the resonant frequency in Hz of a Helmholtz
// resonator:
//
//	f = (c / 2π) · √(S / (V · L_eff))
//
// with neck area S, cavity volume V, and effective neck length L_eff in
// SI units. Non-positive inputs yield 0.
func HelmholtzFrequency(neckArea, volume, neckLength float64) float64 {
	if neckArea <= 0 || volume <= 0 || neckLength <= 0 {
		return 0
	}
	return speedOfSound / twoPi * math.Sqrt(neckArea/(volume*neckLength))
}

// AreaForFrequency inverts the Helmholtz formula for the open-hole area
// that tunes the cavity to freq:
//
//	S = V · L_eff · (2πf / c)²
//
// Physically this is the effective hole area the player "opens" for a
// pitch: covering less of the cavity raises the resonance.
func (o *Ocarina) AreaForFrequency(freq float64) float64 {
	w := twoPi * freq / speedOfSound
	return o.volume * o.neckLength * w * w
}

// NoteOn starts a note. The open-hole area is derived from the pitch so
// the cavity resonates at exactly that frequency, then the breath
// envelope triggers. Velocity (0-1) scales tone and breath levels. A
// NoteOn while sounding re-triggers the envelope; oscillator phases stay
// continuous to avoid clicks.
func (o *Ocarina) NoteOn(pitch int, velocity float32) {
	o.NoteOnParams(pitch, velocity, instruments.NoteParams{Expression: 1, HasExpression: true})
}

// NoteOnEx starts a note like NoteOn, applying per-note envelope time
// overrides to the breath envelope with no vibrato change and full
// expression.
func (o *Ocarina) NoteOnEx(pitch int, velocity float32, env instruments.EnvelopeOverride) {
	o.NoteOnParams(pitch, velocity, instruments.NoteParams{Env: env, Expression: 1, HasExpression: true})
}

// NoteOnParams starts a note like NoteOn, applying the full per-note
// payload: envelope time overrides on the breath envelope, score
// vibrato replacing the default LFO rate and depth (semitones become
// fractional modulation), and expression scaling the output after the
// envelope. Times without a Has flag and an unset expression keep the
// built-in values.
func (o *Ocarina) NoteOnParams(pitch int, velocity float32, params instruments.NoteParams) {
	o.note = pitch
	o.velocity = dsp.ClampF32(velocity, 0, 1)
	o.neckArea = o.AreaForFrequency(midiToFreq(pitch))
	o.freq = HelmholtzFrequency(o.neckArea, o.volume, o.neckLength)
	env := params.Env
	attack, decay, release := attackTime, decayTime, releaseTime
	if env.HasAttack && env.Attack > 0 {
		attack = env.Attack
	}
	if env.HasDecay && env.Decay > 0 {
		decay = env.Decay
	}
	if env.HasRelease && env.Release > 0 {
		release = env.Release
	}
	o.env.SetParameters(attack, decay, sustainLevel, release)
	if params.HasVibrato {
		if r := params.VibratoRate; r > 0 {
			o.vibratoRate = dsp.ClampF32(float32(r), 1, 10)
		}
		if d := params.VibratoDepth; d > 0 {
			o.vibratoDepth = dsp.ClampF32(float32(math.Pow(2, d/12)-1), 0, 0.1)
		}
	}
	expr := float32(1)
	if params.HasExpression {
		expr = params.Expression
		if math.IsNaN(float64(expr)) {
			expr = 1
		} else if expr < 0 {
			expr = 0
		} else if expr > 1 {
			expr = 1
		}
	}
	o.expr = expr
	o.env.Trigger()
}

// NoteOff releases the sounding note if pitch matches; other pitches are
// ignored so a sequencer can broadcast note-offs safely.
func (o *Ocarina) NoteOff(pitch int) {
	if o.note == pitch && o.env.Stage() != dsp.StageIdle {
		o.env.Release()
		o.note = -1
	}
}

// SetParameters applies recognized parameters, clamped to their ranges:
//
//	breath_noise   [0.0, 1.0]  air-jet noise amplitude
//	vibrato_rate   [1.0, 10.0] LFO frequency in Hz
//	vibrato_depth  [0.0, 0.1]  fractional frequency modulation
//	brightness     [0.0, 1.0]  harmonic content scale
//
// Unknown keys are ignored; a nil map is a no-op.
func (o *Ocarina) SetParameters(params map[string]float32) {
	if v, ok := params["breath_noise"]; ok {
		o.breathNoise = dsp.ClampF32(v, 0, 1)
	}
	if v, ok := params["vibrato_rate"]; ok {
		o.vibratoRate = dsp.ClampF32(v, 1, 10)
	}
	if v, ok := params["vibrato_depth"]; ok {
		o.vibratoDepth = dsp.ClampF32(v, 0, 0.1)
	}
	if v, ok := params["brightness"]; ok {
		o.brightness = dsp.ClampF32(v, 0, 1)
	}
}

// Frequency reports the current Helmholtz resonance in Hz.
func (o *Ocarina) Frequency() float64 { return o.freq }

// Note reports the active MIDI pitch, or -1 when no note is held.
func (o *Ocarina) Note() int { return o.note }

// BreathNoise reports the air-jet noise amplitude parameter.
func (o *Ocarina) BreathNoise() float32 { return o.breathNoise }

// VibratoRate reports the vibrato LFO rate in Hz.
func (o *Ocarina) VibratoRate() float32 { return o.vibratoRate }

// VibratoDepth reports the fractional vibrato depth.
func (o *Ocarina) VibratoDepth() float32 { return o.vibratoDepth }

// Brightness reports the harmonic content scale.
func (o *Ocarina) Brightness() float32 { return o.brightness }

// Process renders buffer.Len() frames into both channels, advancing
// synthesis state deltaTime seconds per frame. When the envelope is idle
// the buffer is silenced without touching oscillator state. Per sample:
//
//	f_mod  = f · (1 + vibrato_depth · sin(2π · vibrato_rate · t))
//	tone   = (sin(2πφ) + h2·sin(4πφ) + h3·sin(6πφ)) / (1 + h2 + h3)
//	breath = (formants₂ₖ/₄ₖ/₈ₖ(noise)) · breath_noise · velocity
//	out    = (tone + breath · 0.3) · envelope · velocity · expression
//
// The partial sum is normalized by its own gain total, so the tone stays
// within ±1 at any brightness setting.
func (o *Ocarina) Process(buffer *dsp.Buffer, deltaTime float64) {
	if buffer == nil || deltaTime <= 0 {
		return
	}
	left, right := buffer.Left, buffer.Right
	if o.env.Stage() == dsp.StageIdle {
		clear(left)
		clear(right)
		return
	}

	vel := float64(o.velocity)
	depth := float64(o.vibratoDepth)
	rate := float64(o.vibratoRate)
	breathGain := float64(o.breathNoise) * vel
	h2 := harmonic2Gain * float64(o.brightness)
	h3 := harmonic3Gain * float64(o.brightness)
	norm := 1 / (1 + h2 + h3)
	dt := deltaTime

	for i := range left {
		// Vibrato LFO modulates the resonant frequency.
		f := o.freq * (1 + depth*float64(dsp.SineFast(o.vibratoPhase)))
		o.vibratoPhase = wrap01(o.vibratoPhase + rate*dt)

		// Resonator response: fundamental plus weak 2nd/3rd harmonics.
		// Table lookup replaces three math.Sin calls in the hot loop.
		tp := o.tonePhase
		tone := (float64(dsp.SineFast(tp)) + h2*float64(dsp.SineFast(2*tp)) +
			h3*float64(dsp.SineFast(3*tp))) * norm
		o.tonePhase = wrap01(tp + f*dt)

		// Air jet: white noise through the breath turbulence formants.
		nz := o.noise.Next()
		breath := o.formants[0].ProcessSample(nz) +
			o.formants[1].ProcessSample(nz) +
			o.formants[2].ProcessSample(nz)

		out := float32((tone + float64(breath)*breathMix*breathGain) * o.env.Next(dt) * vel * float64(o.expr))
		left[i], right[i] = out, out
	}
}

// midiToFreq converts a MIDI note number to frequency in Hz (69 = A440).
func midiToFreq(pitch int) float64 {
	return 440 * math.Pow(2, float64(pitch-69)/12)
}

// wrap01 wraps a phase in cycles into [0,1).
func wrap01(p float64) float64 {
	if p >= 1 {
		p -= math.Floor(p)
	}
	return p
}
