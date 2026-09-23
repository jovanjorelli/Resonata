package dsp

import "math"

// EQBandSpec describes one parametric band. Freq <= 0 disables the band;
// Q <= 0 falls back to the per-band-type default.
type EQBandSpec struct {
	Freq   float32 // Hz
	GainDB float32 // dB
	Q      float32
}

// EQSpec is the four-stage track sculpt mirrored from the score DSL's
// TrackEQ: highpass, low shelf, peaking mid, high shelf.
type EQSpec struct {
	HPF       float32 // Hz; <= 0 bypasses
	LowShelf  EQBandSpec
	MidPeak   EQBandSpec
	HighShelf EQBandSpec
}

// Default Q values per band type when a spec leaves Q at zero.
const (
	eqDefaultShelfQ = 0.7071067811865476 // Butterworth
	eqDefaultPeakQ  = 1.0
)

// EQ is a biquad cascade sculpting one channel: HPF → low shelf → mid
// peak → high shelf. All stages are pre-allocated; SetSpec retunes
// coefficients in place (dynamic filtering at control rate) without
// allocation, and disabled stages are skipped in the audio path. Use one
// EQ per channel so the biquad states stay independent.
type EQ struct {
	sampleRate                      float64
	spec                            EQSpec
	hpf                             Biquad
	low                             Biquad
	mid                             Biquad
	high                            Biquad
	useHPF, useLow, useMid, useHigh bool
}

// NewEQ builds the cascade for sampleRate Hz.
func NewEQ(sampleRate float64, spec EQSpec) *EQ {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	e := &EQ{sampleRate: sampleRate}
	e.SetSpec(spec)
	return e
}

// SetSpec retunes every stage in place; bands with Freq <= 0 bypass.
func (e *EQ) SetSpec(spec EQSpec) {
	e.spec = spec

	e.useHPF = spec.HPF > 0
	if e.useHPF {
		e.hpf = Biquad{mode: Highpass, q: eqDefaultShelfQ, sampleRate: e.sampleRate}
		e.hpf.SetFrequency(float64(spec.HPF))
	}
	e.useLow = spec.LowShelf.Freq > 0
	if e.useLow {
		e.low = Biquad{mode: LowShelf, q: bandQ(spec.LowShelf.Q, eqDefaultShelfQ),
			gainDB: float64(spec.LowShelf.GainDB), sampleRate: e.sampleRate}
		e.low.SetFrequency(float64(spec.LowShelf.Freq))
	}
	e.useMid = spec.MidPeak.Freq > 0
	if e.useMid {
		e.mid = Biquad{mode: Peaking, q: bandQ(spec.MidPeak.Q, eqDefaultPeakQ),
			gainDB: float64(spec.MidPeak.GainDB), sampleRate: e.sampleRate}
		e.mid.SetFrequency(float64(spec.MidPeak.Freq))
	}
	e.useHigh = spec.HighShelf.Freq > 0
	if e.useHigh {
		e.high = Biquad{mode: HighShelf, q: bandQ(spec.HighShelf.Q, eqDefaultShelfQ),
			gainDB: float64(spec.HighShelf.GainDB), sampleRate: e.sampleRate}
		e.high.SetFrequency(float64(spec.HighShelf.Freq))
	}
}

// Spec reports the current configuration.
func (e *EQ) Spec() EQSpec { return e.spec }

// Bands reports how many stages are active.
func (e *EQ) Bands() int {
	n := 0
	for _, on := range []bool{e.useHPF, e.useLow, e.useMid, e.useHigh} {
		if on {
			n++
		}
	}
	return n
}

// ProcessSample sculpts one sample through the enabled stages.
func (e *EQ) ProcessSample(x float32) float32 {
	if e.useHPF {
		x = e.hpf.ProcessSample(x)
	}
	if e.useLow {
		x = e.low.ProcessSample(x)
	}
	if e.useMid {
		x = e.mid.ProcessSample(x)
	}
	if e.useHigh {
		x = e.high.ProcessSample(x)
	}
	return x
}

// ProcessInPlace sculpts one channel in place.
func (e *EQ) ProcessInPlace(b []float32) {
	if e.useHPF {
		e.hpf.ProcessInPlace(b)
	}
	if e.useLow {
		e.low.ProcessInPlace(b)
	}
	if e.useMid {
		e.mid.ProcessInPlace(b)
	}
	if e.useHigh {
		e.high.ProcessInPlace(b)
	}
}

// Process sculpts src into dst; dst may alias src.
func (e *EQ) Process(dst, src []float32) {
	if e.useHPF {
		e.hpf.Process(dst, src)
	}
	// Chain remaining stages through dst once it holds the HPF result.
	if !e.useHPF {
		copyN(dst, src)
	}
	if e.useLow {
		e.low.ProcessInPlace(dst)
	}
	if e.useMid {
		e.mid.ProcessInPlace(dst)
	}
	if e.useHigh {
		e.high.ProcessInPlace(dst)
	}
}

// Reset clears every stage's delay state.
func (e *EQ) Reset() {
	e.hpf.Reset()
	e.low.Reset()
	e.mid.Reset()
	e.high.Reset()
}

// ResponseAt returns the cascade's theoretical magnitude response at
// freq (linear gain): the product of the enabled stages.
func (e *EQ) ResponseAt(freq float64) float64 {
	r := 1.0
	if e.useHPF {
		r *= e.hpf.ResponseAt(freq)
	}
	if e.useLow {
		r *= e.low.ResponseAt(freq)
	}
	if e.useMid {
		r *= e.mid.ResponseAt(freq)
	}
	if e.useHigh {
		r *= e.high.ResponseAt(freq)
	}
	return r
}

// ResponseAt returns one biquad's magnitude response at freq (linear),
// evaluated from the normalized coefficients:
//
//	|H(e^jw)| = |b0 + b1·e^-jw + b2·e^-2jw| / |1 + a1·e^-jw + a2·e^-2jw|
func (f *Biquad) ResponseAt(freq float64) float64 {
	w := twoPi * freq / f.sampleRate
	c1, s1 := math.Cos(w), math.Sin(w)
	c2, s2 := math.Cos(2*w), math.Sin(2*w)
	numR := f.b0 + f.b1*c1 + f.b2*c2
	numI := -(f.b1*s1 + f.b2*s2)
	denR := 1 + f.a1*c1 + f.a2*c2
	denI := -(f.a1*s1 + f.a2*s2)
	return math.Hypot(numR, numI) / math.Hypot(denR, denI)
}

// bandQ resolves a spec Q to a concrete value, falling back per band.
func bandQ(q float32, fallback float64) float64 {
	if q <= 0 {
		return fallback
	}
	return float64(q)
}

// copyN copies min(len(dst), len(src)) samples.
func copyN(dst, src []float32) {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
}
