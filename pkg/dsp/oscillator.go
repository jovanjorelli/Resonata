package dsp

import "math"

// twoPi converts a phase in cycles to radians.
const twoPi = 2 * math.Pi

// DefaultNoiseSeed is a non-zero golden-ratio derived seed for Noise.
const DefaultNoiseSeed uint64 = 0x9E3779B97F4A7C15

// Sine renders y = sin(2π·f·t) from a persistent phase accumulator.
type Sine struct {
	phase float64 // cycles, wrapped to [0,1)
	inc   float64 // cycles advanced per frame
}

// NewSine returns a zero-phase sine oscillator.
func NewSine() *Sine { return &Sine{} }

// SetFrequency retunes the oscillator to freq Hz at the given sample rate.
// Phase state is preserved so retuning stays continuous.
func (s *Sine) SetFrequency(freq, sampleRate float64) {
	if sampleRate > 0 {
		s.inc = freq / sampleRate
	}
}

// Phase reports the accumulator position in cycles [0,1).
func (s *Sine) Phase() float64 { return s.phase }

// Next renders one frame and advances the accumulator. The sample comes
// from the shared sine table (see SineFast) rather than math.Sin.
func (s *Sine) Next() float32 {
	v := SineFast(s.phase)
	s.phase += s.inc
	if s.phase >= 1 {
		s.phase -= math.Floor(s.phase)
	}
	return v
}

// Fill renders len(dst) frames into dst.
func (s *Sine) Fill(dst []float32) {
	for i := range dst {
		dst[i] = s.Next()
	}
}

// Saw renders a sawtooth: 2·(phase - floor(phase)) - 1, tracking phase
// across calls like Sine.
type Saw struct {
	phase float64
	inc   float64
}

// NewSaw returns a zero-phase sawtooth oscillator.
func NewSaw() *Saw { return &Saw{} }

// SetFrequency retunes the oscillator to freq Hz at the given sample rate.
func (o *Saw) SetFrequency(freq, sampleRate float64) {
	if sampleRate > 0 {
		o.inc = freq / sampleRate
	}
}

// Phase reports the accumulator position in cycles [0,1).
func (o *Saw) Phase() float64 { return o.phase }

// Next renders one frame and advances the accumulator.
func (o *Saw) Next() float32 {
	v := 2*(o.phase-math.Floor(o.phase)) - 1
	o.phase += o.inc
	if o.phase >= 1 {
		o.phase -= math.Floor(o.phase)
	}
	return float32(v)
}

// Fill renders len(dst) frames into dst.
func (o *Saw) Fill(dst []float32) {
	for i := range dst {
		dst[i] = o.Next()
	}
}

// xorshift64 is a fast, self-contained PRNG (Vigna 2003). It keeps the
// noise path allocation-free, unlike a shared math/rand global source.
type xorshift64 uint64

func (r *xorshift64) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = xorshift64(x)
	return x
}

// Noise renders white noise in [-1, 1) from a fast xorshift PRNG. The
// stream is deterministic per seed and its state persists across calls.
type Noise struct {
	rng xorshift64
}

// NewNoise returns a noise generator; seed 0 falls back to DefaultNoiseSeed.
func NewNoise(seed uint64) *Noise {
	if seed == 0 {
		seed = DefaultNoiseSeed
	}
	return &Noise{rng: xorshift64(seed)}
}

// Next renders one noise frame from the top 24 bits of the PRNG state.
func (n *Noise) Next() float32 {
	return float32(float64(n.rng.next()>>40)*(1.0/8388608.0) - 1.0)
}

// Fill renders len(dst) frames into dst.
func (n *Noise) Fill(dst []float32) {
	for i := range dst {
		dst[i] = n.Next()
	}
}
