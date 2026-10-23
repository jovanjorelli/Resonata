package sampler

import "testing"

// TestCatmullRomEmptyData verifies Catmull-Rom spline interpolation handles empty audio buffers gracefully.
func TestCatmullRomEmptyData(t *testing.T) {
	var nilSrc []float32
	emptySrc := make([]float32, 0)

	for _, src := range [][]float32{nilSrc, emptySrc} {
		for _, idx := range []int{-10, -1, 0, 1, 10} {
			v := readSample(src, idx)
			if v != 0 {
				t.Fatalf("readSample on empty src at %d: got %v, want 0", idx, v)
			}

			vBounded := readSampleBounded(src, idx, 0, 0)
			if vBounded != 0 {
				t.Fatalf("readSampleBounded on empty src at %d: got %v, want 0", idx, vBounded)
			}

			vBounded2 := readSampleBounded(src, idx, -5, 5)
			if vBounded2 != 0 {
				t.Fatalf("readSampleBounded(-5,5) on empty src at %d: got %v, want 0", idx, vBounded2)
			}
		}

		for _, phase := range []float64{-1.0, 0.0, 0.5, 1.0, 10.5} {
			vInterp := interpSample(src, phase)
			if vInterp != 0 {
				t.Fatalf("interpSample on empty src at phase %v: got %v, want 0", phase, vInterp)
			}

			vBounded := interpSampleBounded(src, phase, 0, 0)
			if vBounded != 0 {
				t.Fatalf("interpSampleBounded on empty src at phase %v: got %v, want 0", phase, vBounded)
			}

			vLinear := interpSampleLinear(src, phase)
			if vLinear != 0 {
				t.Fatalf("interpSampleLinear on empty src at phase %v: got %v, want 0", phase, vLinear)
			}
		}

		dst := make([]float32, 64)
		n := ResampleTo(dst, src, 1.0, 0)
		if n != 0 {
			t.Fatalf("ResampleTo returned %d, want 0", n)
		}

		nLinear := ResampleLinearTo(dst, src, 1.0, 0)
		if nLinear != 0 {
			t.Fatalf("ResampleLinearTo returned %d, want 0", nLinear)
		}
	}
}
