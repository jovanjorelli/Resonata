package dsp

import "math"

// Stage identifies the current ADSR segment.
type Stage uint8

// ADSR stages.
const (
	StageIdle Stage = iota
	StageAttack
	StageDecay
	StageSustain
	StageRelease
)

// ADSR is an alias of Envelope: sampled-instrument code refers to the
// classic ADSR name for the same generator.
type ADSR = Envelope

// expConvergence maps a segment duration to an exponential rate so the
// segment reaches ~99.99% of its target within that duration.
const expConvergence = 9.0

// silenceEpsilon is the level below which exponential segments snap to
// their target, avoiding denormal drift.
const silenceEpsilon = 1e-4

// Envelope is an ADSR generator with a linear attack and exponential
// decay/release following
//
//	v = target - (target - current)·exp(-rate·dt)
//	  = start + (target - start)·(1 - exp(-rate·dt)).
type Envelope struct {
	attack      float64 // seconds
	decay       float64 // seconds
	sustain     float64 // level in [0,1]
	release     float64 // seconds
	decayRate   float64 // exponential rate for the decay segment
	releaseRate float64 // exponential rate for the release segment
	stage       Stage
	value       float64
}

// NewEnvelope builds an ADSR envelope. Times are in seconds and sustain is
// a level in [0,1]; non-positive times behave as instantaneous segments.
func NewEnvelope(attack, decay, sustain, release float64) *Envelope {
	e := &Envelope{}
	e.SetParameters(attack, decay, sustain, release)
	return e
}

// SetParameters updates the ADSR parameters, keeping the current stage and
// value so parameters can change mid-note without clicks.
func (e *Envelope) SetParameters(attack, decay, sustain, release float64) {
	e.attack = clampMin(attack, 1e-6)
	e.decay = clampMin(decay, 1e-6)
	e.release = clampMin(release, 1e-6)
	e.sustain = clamp01(sustain)
	e.decayRate = expConvergence / e.decay
	e.releaseRate = expConvergence / e.release
}

// Trigger starts a note: the envelope restarts attack from zero.
func (e *Envelope) Trigger() {
	e.value = 0
	e.stage = StageAttack
}

// Release moves a sounding envelope into its release segment. Idle or
// already-releasing envelopes are unaffected.
func (e *Envelope) Release() {
	switch e.stage {
	case StageAttack, StageDecay, StageSustain:
		e.stage = StageRelease
	}
}

// Stage reports the current segment.
func (e *Envelope) Stage() Stage { return e.stage }

// AttackTime reports the attack duration in seconds.
func (e *Envelope) AttackTime() float64 { return e.attack }

// DecayTime reports the decay duration in seconds.
func (e *Envelope) DecayTime() float64 { return e.decay }

// SustainLevel reports the sustain level in [0,1].
func (e *Envelope) SustainLevel() float64 { return e.sustain }

// ReleaseTime reports the release duration in seconds.
func (e *Envelope) ReleaseTime() float64 { return e.release }

// Value reports the current multiplier without advancing time.
func (e *Envelope) Value() float64 { return e.value }

// Next advances the envelope by dt seconds and returns its multiplier in
// [0,1] for that instant.
func (e *Envelope) Next(dt float64) float64 {
	switch e.stage {
	case StageAttack:
		// Linear rise to full level.
		e.value += dt / e.attack
		if e.value >= 1 {
			e.value = 1
			e.stage = StageDecay
		}
	case StageDecay:
		// Exponential approach of the sustain level.
		e.value = e.sustain + (e.value-e.sustain)*math.Exp(-e.decayRate*dt)
		if math.Abs(e.value-e.sustain) < silenceEpsilon {
			e.value = e.sustain
			e.stage = StageSustain
		}
	case StageSustain:
		e.value = e.sustain
	case StageRelease:
		// Exponential approach of zero.
		e.value *= math.Exp(-e.releaseRate * dt)
		if e.value < silenceEpsilon {
			e.value = 0
			e.stage = StageIdle
		}
	case StageIdle:
		e.value = 0
	}
	return clamp01(e.value)
}

// Fill writes envelope multipliers into dst, advancing dt per frame.
func (e *Envelope) Fill(dst []float32, dt float64) {
	for i := range dst {
		dst[i] = float32(e.Next(dt))
	}
}

// Apply scales dst in place by the envelope, advancing dt per frame.
func (e *Envelope) Apply(dst []float32, dt float64) {
	for i := range dst {
		dst[i] *= float32(e.Next(dt))
	}
}

// clampMin returns v bounded below by lo.
func clampMin(v, lo float64) float64 {
	if v < lo {
		return lo
	}
	return v
}

// clamp01 bounds v to [0,1].
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// ClampF32 bounds v to [lo, hi]; NaN maps to lo.
func ClampF32(v, lo, hi float32) float32 {
	switch {
	case v < lo || v != v:
		return lo
	case v > hi:
		return hi
	}
	return v
}
