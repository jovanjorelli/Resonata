// Package instruments defines the common interface for Resonata sound
// generators.
package instruments

import "resonata/pkg/dsp"

// EnvelopeOverride carries per-note ADSR time overrides in seconds.
// Each Has flag reports whether the corresponding time was set; times
// without a flag fall back to the instrument or SFZ region default.
// Sustain is never overridden: it is a level, not a time.
type EnvelopeOverride struct {
	Attack, Decay, Release          float64
	HasAttack, HasDecay, HasRelease bool
}

// NoteParams carries a full per-note performance payload: envelope
// time overrides, vibrato, and expression gain. The zero value means a
// plain note: SFZ or built-in envelope times, no vibrato, full volume.
type NoteParams struct {
	Env EnvelopeOverride
	// VibratoRate is the LFO frequency in Hz and VibratoDepth the
	// amplitude in semitones; both apply only when HasVibrato is set.
	VibratoRate, VibratoDepth float64
	HasVibrato                bool
	// Expression is the sustained-dynamic gain multiplier (MIDI CC 11
	// semantics): 1.0 is full volume, 0.5 is half. It scales output
	// after the envelope, independently of the attack velocity. Only
	// applies when HasExpression is set; otherwise the note plays full.
	Expression    float32
	HasExpression bool
	// XFadeGain is the velocity/key crossfade multiplier (equal-power
	// blend across overlapping layers). Only applies when HasXFade is
	// set; otherwise the voice plays full. The sampler resolves it per
	// region at trigger time, so engine payloads leave it unset.
	XFadeGain float32
	HasXFade  bool
}

// ParamsVoicer is optionally implemented by instruments that accept the
// full per-note payload. The engine prefers NoteOnParams when the
// scheduled note carries any override and falls back to NoteOn
// otherwise, so instruments without support keep working unchanged.
type ParamsVoicer interface {
	Instrument
	NoteOnParams(pitch int, velocity float32, params NoteParams)
}

// EnvelopeVoicer is optionally implemented by instruments that accept
// per-note envelope overrides. NoteOnEx is a narrow predecessor of
// NoteOnParams kept for compatibility; it applies envelope times with
// no vibrato and full expression.
type EnvelopeVoicer interface {
	Instrument
	NoteOnEx(pitch int, velocity float32, env EnvelopeOverride)
}

// Instrument is a sound generator driven by MIDI-style note events.
// Implementations render into pre-allocated buffers and must not allocate
// during Process.
type Instrument interface {
	// Process renders buffer.Len() frames of audio, advancing internal
	// state deltaTime seconds per frame.
	Process(buffer *dsp.Buffer, deltaTime float64)
	// NoteOn starts a note at the given MIDI pitch (60 = C4) with
	// velocity in [0, 1].
	NoteOn(pitch int, velocity float32)
	// NoteOff releases a sounding note.
	NoteOff(pitch int)
	// SetParameters applies named parameters; unknown keys are ignored.
	SetParameters(params map[string]float32)
}
