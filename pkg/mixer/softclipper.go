package mixer

import "math"

// SoftClipper tames master-bus peaks with tanh saturation. Samples within
// ±threshold pass untouched; above it the curve continues smoothly from
// the threshold and asymptotes to ±1, so output can never hard-clip:
//
//	y = x                                             |x| <= t
//	y = sign(x)·(t + (1-t)·tanh((|x|-t)/(1-t)))       |x| >  t
//
// The continuous knee avoids the discontinuity a raw tanh(x) switch at
// the threshold would introduce.
type SoftClipper struct {
	threshold float32
}

// NewSoftClipper returns a clipper with the threshold clamped to
// [0.05, 1].
func NewSoftClipper(threshold float32) *SoftClipper {
	return &SoftClipper{threshold: clamp32(threshold, 0.05, 1)}
}

// SetThreshold retunes the clipper; the value is clamped to [0.05, 1].
func (c *SoftClipper) SetThreshold(t float32) {
	c.threshold = clamp32(t, 0.05, 1)
}

// Threshold reports the current threshold.
func (c *SoftClipper) Threshold() float32 { return c.threshold }

// ProcessSample soft-clips one sample.
func (c *SoftClipper) ProcessSample(x float32) float32 {
	t := c.threshold
	if x <= t && x >= -t {
		return x
	}
	over := float64(abs32(x)-t) / float64(1-t)
	y := float64(t) + float64(1-t)*math.Tanh(over)
	if x < 0 {
		y = -y
	}
	return float32(y)
}

// ProcessSlice soft-clips a slice in place.
func (c *SoftClipper) ProcessSlice(b []float32) {
	for i, x := range b {
		b[i] = c.ProcessSample(x)
	}
}
