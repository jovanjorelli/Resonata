package mixer

import "math"

// PanGains returns the constant-power gains for a pan position in
// [-1, 1]. The angle (pan+1)·π/4 sweeps 0° to 90°, so hard left is
// (1, 0), center is equal -3 dB on both sides, and hard right is (0, 1).
// The square-root form equals cos/sin of that angle while producing exact
// zeros at the extremes. Positions outside [-1, 1] are clamped.
func PanGains(pan float32) (left, right float32) {
	p := clampF(float64(pan), -1, 1)
	left = float32(math.Sqrt((1 - p) / 2))
	right = float32(math.Sqrt((1 + p) / 2))
	return left, right
}
