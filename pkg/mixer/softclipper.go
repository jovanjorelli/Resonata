package mixer

import "math"

// SoftClipper tames master-bus peaks. In tape mode (the default) samples
// within ±threshold pass untouched and above it follow a smooth
// tanh knee asymptoting to ±1, so output can never hard-clip:
//
//	y = x                                             |x| <= t
//	y = sign(x)·(t + (1-t)·tanh((|x|-t)/(1-t)))       |x| >  t
//
// In clean mode the clipper is bypassed to a linear hard clamp at ±1:
// samples pass through unchanged below the ceiling with no saturation
// coloration. Both modes are branch-only and allocate nothing.
// The continuous tanh knee avoids the discontinuity a raw tanh(x) switch
// at the threshold would introduce.
type SoftClipper struct {
	threshold float32
	clean     bool // linear hard clamp at ±1 instead of tanh saturation
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

// SetClean selects clean mode: linear passthrough with a hard clamp at
// ±1 instead of tanh saturation. It reports nothing and allocates nothing.
func (c *SoftClipper) SetClean(clean bool) { c.clean = clean }

// Clean reports whether clean mode is active.
func (c *SoftClipper) Clean() bool { return c.clean }

// ProcessSample soft-clips one sample.
func (c *SoftClipper) ProcessSample(x float32) float32 {
	if c.clean {
		if x > 1 {
			return 1
		}
		if x < -1 {
			return -1
		}
		return x
	}
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
