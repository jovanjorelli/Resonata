package mixer

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"resonata/pkg/instruments/ocarina"
)

// dcInstrument renders a constant level on both channels, giving exact
// expectations for summing, panning, and gain tests.
type dcInstrument struct{ level float32 }

func (d *dcInstrument) Process(buffer *dsp.Buffer, deltaTime float64) {
	for i := range buffer.Left {
		buffer.Left[i] = d.level
		buffer.Right[i] = d.level
	}
}
func (d *dcInstrument) NoteOn(pitch int, velocity float32)      {}
func (d *dcInstrument) NoteOff(pitch int)                       {}
func (d *dcInstrument) SetParameters(params map[string]float32) {}

var _ instruments.Instrument = (*dcInstrument)(nil)

func TestMixerSumming(t *testing.T) {
	m := New(48000, 4, 256)
	m.SetReverbWet(0) // isolate the summing stage
	for _, lvl := range []float32{0.2, 0.3, 0.4} {
		if i := m.AddTrack(&dcInstrument{level: lvl}, 0, 1); i < 0 {
			t.Fatal("AddTrack rejected a track")
		}
	}
	if m.TrackCount() != 3 {
		t.Fatalf("TrackCount = %d, want 3", m.TrackCount())
	}
	m.Process(256)
	if m.Frames() != 256 || len(m.Master()) != 512 {
		t.Fatalf("frames/master = %d/%d, want 256/512", m.Frames(), len(m.Master()))
	}
	// Sum 0.9 through center pan (√0.5 per side) stays under the 0.9 clip
	// threshold, so the master holds the exact linear sum.
	want := float32(0.9 * math.Sqrt(0.5))
	for i, v := range m.Master() {
		if math.Abs(float64(v-want)) > 1e-5 {
			t.Fatalf("master[%d] = %v, want %v", i, v, want)
		}
	}
}

func TestMixerPanVolumeMute(t *testing.T) {
	m := New(48000, 3, 16)
	m.SetReverbWet(0)
	a := m.AddTrack(&dcInstrument{level: 0.5}, -1, 1)  // hard left
	b := m.AddTrack(&dcInstrument{level: 0.5}, 1, 0.5) // hard right, half gain
	c := m.AddTrack(&dcInstrument{level: 0.5}, 0, 1)   // center, muted
	m.SetMuted(c, true)
	if !m.Muted(c) {
		t.Fatal("mute not recorded")
	}
	m.Process(16)
	// Hard pans give exact zeros on the far side.
	if l, r := m.Master()[0], m.Master()[1]; l != 0.5 || r != 0.25 {
		t.Fatalf("L/R = %v/%v, want exact 0.5/0.25", l, r)
	}

	m.SetMuted(c, false)
	m.Process(16)
	center := float32(0.5 * math.Sqrt(0.5))
	if l, r := m.Master()[0], m.Master()[1]; math.Abs(float64(l-(0.5+center))) > 1e-6 ||
		math.Abs(float64(r-(0.25+center))) > 1e-6 {
		t.Fatalf("unmuted L/R = %v/%v", l, r)
	}

	// Volume changes apply to the next block; zero silences the strip.
	m.SetVolume(a, 0)
	m.Process(16)
	if l := m.Master()[0]; math.Abs(float64(l-center)) > 1e-6 {
		t.Fatalf("zeroed track still contributes: L = %v, want %v", l, center)
	}
	_ = b
}

func TestMixerTrackLimit(t *testing.T) {
	m := New(48000, 2, 16)
	if m.AddTrack(&dcInstrument{}, 0, 1) != 0 || m.AddTrack(&dcInstrument{}, 0, 1) != 1 {
		t.Fatal("first tracks should register")
	}
	if m.AddTrack(&dcInstrument{}, 0, 1) != -1 {
		t.Fatal("over-capacity AddTrack should return -1")
	}
	if m.AddTrack(nil, 0, 1) != -1 {
		t.Fatal("nil instrument should return -1")
	}
}

func TestMixerBlockClamp(t *testing.T) {
	m := New(48000, 1, 128)
	m.SetReverbWet(0)
	m.AddTrack(&dcInstrument{level: 0.1}, 0, 1)
	m.Process(1000)
	if m.Frames() != 128 || len(m.Master()) != 256 {
		t.Fatalf("oversized block: frames/master = %d/%d", m.Frames(), len(m.Master()))
	}
	m.Process(0)
	if m.Frames() != 0 || len(m.Master()) != 0 {
		t.Fatal("zero block should render nothing")
	}
	m.Process(-5)
	if m.Frames() != 0 {
		t.Fatal("negative block should render nothing")
	}
	m.Process(64)
	if m.Frames() != 64 {
		t.Fatalf("frames = %d, want 64", m.Frames())
	}
}

func TestMixerClipperEngaged(t *testing.T) {
	m := New(48000, 3, 64)
	m.SetReverbWet(0)
	for k := 0; k < 3; k++ {
		m.AddTrack(&dcInstrument{level: 1.0}, 0, 1)
	}
	m.Process(64)
	// Linear sum would be 3·√0.5 ≈ 2.12; saturation must land just under
	// full scale and never beyond it.
	if v := m.Master()[0]; v <= 0.99 || v > 1.0 {
		t.Fatalf("clipped master = %v, want in (0.99, 1.0]", v)
	}
}

// noiseInstrument renders seeded white noise — broadband excitation whose
// wet return adds energy incoherently (a sine would interfere by phase
// and could cancel instead of building).
type noiseInstrument struct {
	noise *dsp.Noise
	level float32
}

func (s *noiseInstrument) Process(buffer *dsp.Buffer, deltaTime float64) {
	for i := range buffer.Left {
		v := s.level * s.noise.Next()
		buffer.Left[i], buffer.Right[i] = v, v
	}
}
func (s *noiseInstrument) NoteOn(pitch int, velocity float32)      {}
func (s *noiseInstrument) NoteOff(pitch int)                       {}
func (s *noiseInstrument) SetParameters(params map[string]float32) {}

func rmsOf(b []float32) float64 {
	var s float64
	for _, v := range b {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(b)))
}

func peakOf(b []float32) float32 {
	p := float32(0)
	for _, v := range b {
		if a := abs32(v); a > p {
			p = a
		}
	}
	return p
}

func TestMixerReverbSends(t *testing.T) {
	// Send = 0: the master stays exactly dry no matter the wet level.
	m := New(48000, 1, 256)
	m.SetReverbWet(0.5)
	dc := m.AddTrack(&dcInstrument{level: 0.5}, 0, 1)
	if m.TrackSend(dc) != 0 {
		t.Fatalf("default send = %v, want 0", m.TrackSend(dc))
	}
	m.Process(256)
	dry := float32(0.5 * math.Sqrt(0.5))
	for i, v := range m.Master() {
		if math.Abs(float64(v-dry)) > 1e-5 {
			t.Fatalf("send 0 leaked wet: master[%d] = %v, want %v", i, v, dry)
		}
	}

	// Send > 0: broadband excitation — the FDN return adds energy, the
	// RMS grows above the dry baseline, and the clipper holds full scale.
	newNoise := func() *noiseInstrument { return &noiseInstrument{noise: dsp.NewNoise(3), level: 0.5} }
	dryMix := New(48000, 1, 4096)
	dryMix.SetReverbWet(0)
	dryMix.SetSend(dryMix.AddTrack(newNoise(), 0, 1), 1)
	dryMix.Process(4096)
	dryRMS := rmsOf(dryMix.Master())

	wetMix := New(48000, 1, 4096)
	wetMix.SetReverbWet(1)
	wetMix.SetSend(wetMix.AddTrack(newNoise(), 0, 1), 1)
	for k := 0; k < 25; k++ { // ~2 s: the return reaches steady state
		wetMix.Process(4096)
	}
	wetRMS := rmsOf(wetMix.Master())
	if wetRMS <= dryRMS*1.03 {
		t.Fatalf("reverb return did not build: dry rms %v, wet rms %v", dryRMS, wetRMS)
	}
	if p := peakOf(wetMix.Master()); p > 1 {
		t.Fatalf("master peak %v exceeds full scale", p)
	}

	// Send clamping and preset switching.
	wetMix.SetSend(0, 5)
	if got := wetMix.TrackSend(0); got != 1 {
		t.Fatalf("send clamp = %v, want 1", got)
	}
	if !wetMix.SetReverbPreset("cathedral") {
		t.Error("cathedral preset rejected")
	}
	if wetMix.SetReverbPreset("closet") {
		t.Error("unknown preset accepted")
	}
	if rt := wetMix.Reverb().RT60Seconds(); rt < 2 {
		t.Errorf("cathedral RT60 = %.1f s, want a long tail", rt)
	}
}

func TestMixerWithOcarina(t *testing.T) {
	m := New(48000, 2, 512)
	m.SetReverbWet(0.3)
	o1 := ocarina.New(48000)
	o1.NoteOn(69, 0.9)
	o2 := ocarina.New(48000)
	o2.SetParameters(map[string]float32{"vibrato_rate": 6.5})
	o2.NoteOn(76, 0.8)
	m.AddTrack(o1, -0.5, 0.8)
	m.AddTrack(o2, 0.5, 0.8)

	peak, diff := float32(0), float32(0)
	for k := 0; k < 40; k++ { // ~0.43 s
		m.Process(512)
		master := m.Master()
		for j := 0; j < 512; j++ {
			l, r := master[2*j], master[2*j+1]
			if a := abs32(l); a > peak {
				peak = a
			}
			if a := abs32(r); a > peak {
				peak = a
			}
			if d := abs32(l - r); d > diff {
				diff = d
			}
		}
	}
	if peak < 0.15 {
		t.Errorf("peak = %v, mix is inaudible", peak)
	}
	if peak > 1 {
		t.Errorf("peak = %v, master exceeded full scale", peak)
	}
	if diff < 0.01 {
		t.Errorf("L/R difference = %v, panning produced no stereo image", diff)
	}
}

func TestMixerZeroAlloc(t *testing.T) {
	m := New(48000, 8, 512)
	o := ocarina.New(48000)
	o.NoteOn(69, 0.8)
	m.AddTrack(o, -0.3, 0.7)
	m.SetSend(0, 0.5) // exercise the reverb send path in the hot loop
	m.AddTrack(&dcInstrument{level: 0.1}, 0.4, 0.5)
	m.Process(512) // warm every path
	m.Process(128)
	if allocs := testing.AllocsPerRun(30, func() { m.Process(512) }); allocs != 0 {
		t.Fatalf("Process(512) allocates %v times per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(10, func() { m.Process(128) }); allocs != 0 {
		t.Fatalf("Process(128) allocates %v times per call, want 0", allocs)
	}
}

func TestMixerAddTrackZeroAlloc(t *testing.T) {
	m := New(48000, 4, 64)
	dc := &dcInstrument{level: 0.1}
	m.AddTrack(dc, 0, 1) // consume one slot outside the measured run
	if allocs := testing.AllocsPerRun(3, func() { m.AddTrack(dc, 0, 1) }); allocs != 0 {
		t.Fatalf("AddTrack allocates %v times per call, want 0", allocs)
	}
}
