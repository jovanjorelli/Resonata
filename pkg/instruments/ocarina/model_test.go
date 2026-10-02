package ocarina

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// Compile-time interface check (also present in model.go).
var _ instruments.Instrument = (*Ocarina)(nil)

func TestHelmholtzFrequency(t *testing.T) {
	// f = (c/2π)·√(S/(V·L)) for S=1e-4 m², V=2.5e-4 m³, L=0.015 m.
	want := 343.0 / (2 * math.Pi) * math.Sqrt(1e-4/(2.5e-4*0.015))
	if got := HelmholtzFrequency(1e-4, 2.5e-4, 0.015); math.Abs(got-want) > 1e-9 {
		t.Fatalf("HelmholtzFrequency = %v, want %v", got, want)
	}
	if got := HelmholtzFrequency(0, 1, 1); got != 0 {
		t.Fatalf("zero area: got %v, want 0", got)
	}
	if got := HelmholtzFrequency(1, -1, 1); got != 0 {
		t.Fatalf("negative volume: got %v, want 0", got)
	}
	if got := HelmholtzFrequency(1, 1, 0); got != 0 {
		t.Fatalf("zero neck length: got %v, want 0", got)
	}
}

func TestNoteOnTunesHelmholtz(t *testing.T) {
	o := New(48000)
	if o.Note() != -1 {
		t.Fatalf("idle note = %d, want -1", o.Note())
	}
	for pitch, want := range map[int]float64{60: 261.6256, 69: 440.0, 72: 523.2511, 81: 880.0} {
		o.NoteOn(pitch, 0.8)
		if math.Abs(o.Frequency()-want) > 0.01 {
			t.Errorf("pitch %d: freq = %v, want %v", pitch, o.Frequency(), want)
		}
		if o.Note() != pitch {
			t.Errorf("note = %d, want %d", o.Note(), pitch)
		}
		// Round trip: the derived hole area must reproduce the frequency
		// through the Helmholtz formula.
		f := HelmholtzFrequency(o.AreaForFrequency(want), defaultVolume, defaultNeckLength)
		if math.Abs(f-want) > 1e-6 {
			t.Errorf("pitch %d: area round trip = %v, want %v", pitch, f, want)
		}
	}
}

func TestProcessSilenceAndSignal(t *testing.T) {
	o := New(48000)
	buf := dsp.NewStereoBuffer(512)
	dt := 1.0 / 48000

	// Idle instrument renders exact zeros over dirty buffer content.
	buf.SetLen(512)
	for i := range buf.Left {
		buf.Left[i], buf.Right[i] = 9, 9
	}
	o.Process(buf, dt)
	for i, v := range buf.Left {
		if v != 0 || buf.Right[i] != 0 {
			t.Fatalf("idle frame %d = %v/%v, want silence", i, v, buf.Right[i])
		}
	}

	// Sounding note renders in-range audio on both channels.
	o.NoteOn(69, 0.9)
	peak := float32(0)
	for k := 0; k < 90; k++ { // ~0.96 s, well past the 50 ms attack
		buf.SetLen(512)
		o.Process(buf, dt)
		for i, v := range buf.Left {
			if buf.Right[i] != v {
				t.Fatalf("frame %d: channels differ", i)
			}
			a := abs32(v)
			if a > peak {
				peak = a
			}
			if a > 1.2 {
				t.Fatalf("frame %d out of range: %v", i, v)
			}
		}
	}
	if peak < 0.6 || peak > 1.1 {
		t.Fatalf("peak = %v, want a strong in-range signal", peak)
	}

	// NoteOff completes the 100 ms release, then output is exact silence.
	o.NoteOff(69)
	if o.Note() != -1 {
		t.Fatalf("note after off = %d, want -1", o.Note())
	}
	silent := false
	for k := 0; k < 20 && !silent; k++ {
		buf.SetLen(512)
		o.Process(buf, dt)
		silent = true
		for _, v := range buf.Left {
			if v != 0 {
				silent = false
				break
			}
		}
	}
	if !silent {
		t.Fatal("still sounding after the release completed")
	}

	// A note-off for another pitch is ignored.
	o.NoteOn(72, 0.8)
	o.NoteOff(60)
	if o.Note() != 72 {
		t.Fatalf("mismatched note-off released the active note")
	}
}

func TestEnvelopeShape(t *testing.T) {
	o := New(48000)
	o.SetParameters(map[string]float32{"breath_noise": 0, "brightness": 0, "vibrato_depth": 0})
	o.NoteOn(69, 1.0)
	dt := 1.0 / 48000
	buf := dsp.NewStereoBuffer(1)

	// Peak amplitude per 5 ms bucket; pure sine at velocity 1 means the
	// bucket peak tracks the envelope value.
	bucketFrames := int(0.005 / dt)
	peaks := make([]float64, 40) // 200 ms
	for i := range peaks {
		for s := 0; s < bucketFrames; s++ {
			buf.SetLen(1)
			o.Process(buf, dt)
			if a := math.Abs(float64(buf.Left[0])); a > peaks[i] {
				peaks[i] = a
			}
		}
	}
	// Linear 50 ms attack: ~0.5 at 20-25 ms, ~1.0 by 45-50 ms.
	if peaks[4] < 0.30 || peaks[4] > 0.60 {
		t.Errorf("attack at 20-25 ms = %v, want ~0.5", peaks[4])
	}
	if peaks[9] < 0.85 {
		t.Errorf("attack at 45-50 ms = %v, want ~1.0", peaks[9])
	}
	if peaks[15] < 0.95 || peaks[15] > 1.001 {
		t.Errorf("sustain at 75-80 ms = %v, want ~1.0", peaks[15])
	}

	// Release: exponential 100 ms decay to exact zero.
	o.NoteOff(69)
	rel := make([]float64, 30) // 150 ms
	for i := range rel {
		for s := 0; s < bucketFrames; s++ {
			buf.SetLen(1)
			o.Process(buf, dt)
			if a := math.Abs(float64(buf.Left[0])); a > rel[i] {
				rel[i] = a
			}
		}
	}
	if rel[0] < 0.8 {
		t.Errorf("release start = %v, want near full level", rel[0])
	}
	if rel[10] > 0.05 {
		t.Errorf("release at 50-55 ms = %v, want fast exponential decay", rel[10])
	}
	if rel[24] != 0 || rel[29] != 0 {
		t.Errorf("release at 120+ ms = %v/%v, want exact silence", rel[24], rel[29])
	}
}

func TestVibratoModulatesPitch(t *testing.T) {
	// Measure the longest/shortest fundamental period ratio from
	// interpolated rising zero crossings of a pure tone.
	periodRatio := func(depth float32) float64 {
		o := New(48000)
		o.SetParameters(map[string]float32{
			"breath_noise": 0, "brightness": 0,
			"vibrato_depth": depth, "vibrato_rate": 5,
		})
		o.NoteOn(69, 1.0)
		dt := 1.0 / 48000
		buf := dsp.NewStereoBuffer(1024)
		var crossings []float64
		var prev float32
		frame := 0.0
		const skip = 4800         // skip attack and LFO warm-up (0.1 s)
		for k := 0; k < 47; k++ { // ~1 s of audio
			buf.SetLen(1024)
			o.Process(buf, dt)
			for _, v := range buf.Left {
				if frame >= skip && prev < 0 && v >= 0 {
					frac := float64(-prev / (v - prev))
					crossings = append(crossings, (frame-1+frac)*dt)
				}
				prev = v
				frame++
			}
		}
		min, max := math.MaxFloat64, 0.0
		for i := 1; i < len(crossings); i++ {
			p := crossings[i] - crossings[i-1]
			if p < min {
				min = p
			}
			if p > max {
				max = p
			}
		}
		return max / min
	}
	if r := periodRatio(0); r > 1.02 {
		t.Fatalf("depth 0: period ratio %v, want ~1.0", r)
	}
	// depth 0.05 swings the frequency ±5%: ratio (1+d)/(1-d) ≈ 1.105.
	if r := periodRatio(0.05); r < 1.06 || r > 1.16 {
		t.Fatalf("depth 0.05: period ratio %v, want ~1.105", r)
	}
}

func TestHarmonicBalance(t *testing.T) {
	o := New(48000)
	o.SetParameters(map[string]float32{"breath_noise": 0, "vibrato_depth": 0})
	o.NoteOn(69, 1.0)
	dt := 1.0 / 48000
	buf := dsp.NewStereoBuffer(4800)
	buf.SetLen(4800)
	o.Process(buf, dt) // burn the 50 ms attack
	buf.SetLen(4800)
	o.Process(buf, dt) // 0.1 s window: 440 Hz falls exactly on bin 44

	// Naive single-bin DFT amplitude.
	mag := func(f float64) float64 {
		var re, im float64
		for i, v := range buf.Left {
			a := twoPi * f * float64(i) * dt
			re += float64(v) * math.Cos(a)
			im -= float64(v) * math.Sin(a)
		}
		return 2 * math.Sqrt(re*re+im*im) / float64(len(buf.Left))
	}
	m1, m2, m3 := mag(440), mag(880), mag(1320)
	// brightness 1: the fundamental is normalized by the full partial sum.
	wantFund := 1 / (1 + harmonic2Gain + harmonic3Gain)
	if math.Abs(m1-wantFund) > 0.02 {
		t.Errorf("fundamental amplitude = %v, want ~%v (normalized)", m1, wantFund)
	}
	if r := m2 / m1; math.Abs(r-harmonic2Gain) > 0.02 {
		t.Errorf("2nd harmonic ratio = %v, want ~%v (-12 dB)", r, harmonic2Gain)
	}
	if r := m3 / m1; math.Abs(r-harmonic3Gain) > 0.02 {
		t.Errorf("3rd harmonic ratio = %v, want ~%v (-18 dB)", r, harmonic3Gain)
	}
}

func TestBreathNoiseScales(t *testing.T) {
	// High-frequency energy (first-difference RMS) of a pure 440 Hz tone
	// grows with the breath_noise parameter and with velocity.
	render := func(breath, velocity float32) float64 {
		o := New(48000)
		o.SetParameters(map[string]float32{
			"breath_noise": breath, "vibrato_depth": 0, "brightness": 0,
		})
		o.NoteOn(69, velocity)
		dt := 1.0 / 48000
		buf := dsp.NewStereoBuffer(4800)
		buf.SetLen(4800)
		o.Process(buf, dt) // burn attack
		buf.SetLen(4800)
		o.Process(buf, dt)
		var sum float64
		prev := buf.Left[0]
		for _, v := range buf.Left[1:] {
			d := float64(v - prev)
			sum += d * d
			prev = v
		}
		return math.Sqrt(sum / float64(len(buf.Left)-1))
	}
	off, on := render(0, 1.0), render(1.0, 1.0)
	if on < 2.5*off {
		t.Fatalf("breath HF rms: off=%v on=%v, want a clear noise component", off, on)
	}
	soft := render(1.0, 0.3)
	if on < 3*soft {
		t.Fatalf("velocity does not scale breath: vel1=%v vel0.3=%v", on, soft)
	}
}

func TestSetParametersClamp(t *testing.T) {
	o := New(48000)
	if o.BreathNoise() != 0.3 || o.VibratoRate() != 5.0 ||
		o.VibratoDepth() != 0.02 || o.Brightness() != 1.0 {
		t.Fatalf("defaults = %v %v %v %v", o.BreathNoise(), o.VibratoRate(), o.VibratoDepth(), o.Brightness())
	}
	o.SetParameters(map[string]float32{
		"breath_noise": 3, "vibrato_rate": 100, "vibrato_depth": 1, "brightness": -2,
	})
	if o.BreathNoise() != 1 || o.VibratoRate() != 10 || o.VibratoDepth() != 0.1 || o.Brightness() != 0 {
		t.Fatalf("clamping failed: %v %v %v %v",
			o.BreathNoise(), o.VibratoRate(), o.VibratoDepth(), o.Brightness())
	}
	o.SetParameters(map[string]float32{"vibrato_rate": 6.5})
	if o.VibratoRate() != 6.5 {
		t.Fatalf("vibrato_rate = %v, want 6.5", o.VibratoRate())
	}
	o.SetParameters(map[string]float32{"nonsense": 42})
	o.SetParameters(nil)
	if o.VibratoRate() != 6.5 {
		t.Fatalf("unknown keys or nil map changed state: %v", o.VibratoRate())
	}
}

func TestProcessZeroAlloc(t *testing.T) {
	o := New(48000)
	o.NoteOn(69, 0.8)
	buf := dsp.NewStereoBuffer(256)
	dt := 1.0 / 48000
	allocs := testing.AllocsPerRun(50, func() {
		buf.SetLen(256)
		o.Process(buf, dt)
	})
	if allocs != 0 {
		t.Fatalf("Process allocates %v times per call, want 0", allocs)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
