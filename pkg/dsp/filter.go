package dsp

import "math"

// FilterMode selects the biquad response.
type FilterMode uint8

// Supported biquad modes.
const (
	Lowpass FilterMode = iota
	Highpass
	Bandpass
	Peaking
	LowShelf
	HighShelf
)

// defaultQ is Butterworth damping (1/√2), used when a non-positive Q is
// requested.
const defaultQ = 0.7071067811865476

// Biquad is a direct-form I biquad filter using Robert Bristow-Johnson's
// cookbook coefficients. The delay lines persist across Process calls so
// consecutive blocks join seamlessly.
type Biquad struct {
	mode       FilterMode
	freq       float64 // cutoff or center frequency in Hz
	q          float64
	gainDB     float64 // peaking/shelf gain in dB
	sampleRate float64
	// Normalized coefficients (a0 == 1).
	b0, b1, b2, a1, a2 float64
	// Delay state: previous two inputs and outputs.
	x1, x2, y1, y2 float64
}

// NewBiquad builds a filter tuned to freq Hz at the given sample rate.
// q <= 0 falls back to Butterworth and sampleRate <= 0 falls back to
// 48 kHz.
func NewBiquad(mode FilterMode, freq, q, sampleRate float64) *Biquad {
	return NewBiquadGain(mode, freq, q, 0, sampleRate)
}

// NewBiquadGain builds a filter with a gain in dB, used by the Peaking,
// LowShelf, and HighShelf modes (ignored by the others).
func NewBiquadGain(mode FilterMode, freq, q, gainDB, sampleRate float64) *Biquad {
	if q <= 0 {
		q = defaultQ
	}
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	f := &Biquad{mode: mode, q: q, gainDB: gainDB, sampleRate: sampleRate}
	f.SetFrequency(freq)
	return f
}

// SetGainDB retunes a peaking/shelf filter's gain and recomputes
// coefficients.
func (f *Biquad) SetGainDB(db float64) {
	f.gainDB = db
	f.computeCoeffs()
}

// GainDB reports the current gain setting.
func (f *Biquad) GainDB() float64 { return f.gainDB }

// SetFrequency retunes the filter and recomputes coefficients. The cutoff
// is clamped into (0, Nyquist).
func (f *Biquad) SetFrequency(freq float64) {
	nyquist := f.sampleRate / 2
	if freq <= 0 {
		freq = 1e-3
	}
	if freq >= nyquist {
		freq = math.Nextafter(nyquist, 0)
	}
	f.freq = freq
	f.computeCoeffs()
}

// SetQ updates the resonance and recomputes coefficients.
func (f *Biquad) SetQ(q float64) {
	if q <= 0 {
		q = defaultQ
	}
	f.q = q
	f.computeCoeffs()
}

// Frequency reports the current cutoff in Hz.
func (f *Biquad) Frequency() float64 { return f.freq }

// Q reports the current resonance.
func (f *Biquad) Q() float64 { return f.q }

// Reset clears the delay lines for a fresh signal.
func (f *Biquad) Reset() {
	f.x1, f.x2, f.y1, f.y2 = 0, 0, 0, 0
}

// computeCoeffs evaluates the RBJ Audio EQ Cookbook formulas:
//
//	w0 = 2π·f0/fs,  alpha = sin(w0)/(2Q),  A = 10^(gainDB/40)
//	LP:  b0 = (1-cos w0)/2,  b1 = 1-cos w0,   b2 = (1-cos w0)/2
//	HP:  b0 = (1+cos w0)/2,  b1 = -(1+cos w0), b2 = (1+cos w0)/2
//	BP:  b0 = alpha, b1 = 0, b2 = -alpha      (constant 0 dB peak gain)
//	Peak: b0 = 1+alpha·A, b1 = -2cos w0, b2 = 1-alpha·A
//	      a0 = 1+alpha/A, a1 = -2cos w0, a2 = 1-alpha/A
//	Shelves use A and alpha·√A per the cookbook; all sections normalize
//	by a0 = 1+alpha (LP/HP/BP) or the shelf-specific a0.
func (f *Biquad) computeCoeffs() {
	w0 := twoPi * f.freq / f.sampleRate
	sin, cos := math.Sincos(w0)
	alpha := sin / (2 * f.q)
	var b0, b1, b2, a0, a1, a2 float64
	switch f.mode {
	case Highpass:
		b0 = (1 + cos) / 2
		b1 = -(1 + cos)
		b2 = (1 + cos) / 2
		a0, a1, a2 = 1+alpha, -2*cos, 1-alpha
	case Bandpass:
		b0, b1, b2 = alpha, 0, -alpha
		a0, a1, a2 = 1+alpha, -2*cos, 1-alpha
	case Peaking:
		A := math.Pow(10, f.gainDB/40) // A = sqrt(10^(dB/20))
		b0 = 1 + alpha*A
		b1 = -2 * cos
		b2 = 1 - alpha*A
		a0 = 1 + alpha/A
		a1 = -2 * cos
		a2 = 1 - alpha/A
	case LowShelf:
		A := math.Pow(10, f.gainDB/40)
		twoSqrtAAlpha := 2 * math.Sqrt(A) * alpha
		b0 = A * ((A + 1) - (A-1)*cos + twoSqrtAAlpha)
		b1 = 2 * A * ((A - 1) - (A+1)*cos)
		b2 = A * ((A + 1) - (A-1)*cos - twoSqrtAAlpha)
		a0 = (A + 1) + (A-1)*cos + twoSqrtAAlpha
		a1 = -2 * ((A - 1) + (A+1)*cos)
		a2 = (A + 1) + (A-1)*cos - twoSqrtAAlpha
	case HighShelf:
		A := math.Pow(10, f.gainDB/40)
		twoSqrtAAlpha := 2 * math.Sqrt(A) * alpha
		b0 = A * ((A + 1) + (A-1)*cos + twoSqrtAAlpha)
		b1 = -2 * A * ((A - 1) + (A+1)*cos)
		b2 = A * ((A + 1) + (A-1)*cos - twoSqrtAAlpha)
		a0 = (A + 1) - (A-1)*cos + twoSqrtAAlpha
		a1 = 2 * ((A - 1) - (A+1)*cos)
		a2 = (A + 1) - (A-1)*cos - twoSqrtAAlpha
	default: // Lowpass
		b0 = (1 - cos) / 2
		b1 = 1 - cos
		b2 = (1 - cos) / 2
		a0, a1, a2 = 1+alpha, -2*cos, 1-alpha
	}
	f.b0 = b0 / a0
	f.b1 = b1 / a0
	f.b2 = b2 / a0
	f.a1 = a1 / a0
	f.a2 = a2 / a0
}

// ProcessSample filters a single sample, carrying state across calls.
func (f *Biquad) ProcessSample(x float32) float32 {
	xi := float64(x)
	y := f.b0*xi + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, xi
	f.y2, f.y1 = f.y1, y
	return float32(y)
}

// Process filters src into dst frame-by-frame, carrying state across
// calls. dst may alias src; if dst is shorter, trailing frames are
// dropped.
func (f *Biquad) Process(dst, src []float32) {
	for i, in := range src {
		y := f.ProcessSample(in)
		if i < len(dst) {
			dst[i] = y
		}
	}
}

// ProcessInPlace filters one channel in place. For stereo, use one Biquad
// per channel so the delay lines stay independent.
func (f *Biquad) ProcessInPlace(b []float32) {
	f.Process(b, b)
}
