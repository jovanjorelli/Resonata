package dsp

import "math"

// sineTableBits sizes the sine lookup table: 2048 entries over one
// cycle. Linear interpolation between entries holds the worst-case
// error near 1.2e-6, far below any audible or test threshold, while
// replacing a ~30 ns math.Sin call with two loads and a lerp.
const sineTableBits = 11
const sineTableSize = 1 << sineTableBits

// sineTable holds sin(2π·i/N) plus one wraparound entry, filled once at
// startup. Reads are immutable afterwards and safe for concurrent use.
var sineTable [sineTableSize + 1]float32

func init() {
	for i := 0; i <= sineTableSize; i++ {
		sineTable[i] = float32(math.Sin(twoPi * float64(i) / sineTableSize))
	}
}

// SineFast returns sin(2π·phase) for phase in cycles, wrapping any input
// into [0, 1) and linearly interpolating the table. Hot oscillators
// (ocarina partials, vibrato LFO) use this instead of math.Sin.
func SineFast(phase float64) float32 {
	p := phase - math.Floor(phase)
	x := p * sineTableSize
	i := int(x)
	f := float32(x - float64(i))
	a := sineTable[i]
	return a + (sineTable[i+1]-a)*f
}
