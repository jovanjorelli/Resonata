package mixer

import (
	"testing"

	"resonata/pkg/dsp"
)

// roomParams builds a space with the given size, damping, and width.
func roomParams(size, damping, width float32) dsp.ReverbParams {
	return dsp.ReverbParams{
		RoomSize: size, Damping: damping, Width: width,
		Wet: 0.4, Dry: 0.6, PreDelay: 0.02,
	}
}

// TestTrackRoomSharing reuses one FDN instance per configuration.
func TestTrackRoomSharing(t *testing.T) {
	m := New(48000, 4, 256)
	for k := 0; k < 3; k++ {
		m.AddTrack(&noiseInstrument{noise: dsp.NewNoise(uint64(k + 1)), level: 0.5}, 0, 0.8)
	}
	hall, _ := dsp.PresetParams("hall")
	a := m.SetTrackRoom(0, hall)
	b := m.SetTrackRoom(1, hall)
	if a != b || a != 1 {
		t.Fatalf("shared room indices = %d/%d, want 1/1", a, b)
	}
	cath, _ := dsp.PresetParams("cathedral")
	c := m.SetTrackRoom(2, cath)
	if c != 2 {
		t.Fatalf("distinct room index = %d, want 2", c)
	}
	if len(m.reverbs) != 3 {
		t.Fatalf("reverb instances = %d, want master + 2", len(m.reverbs))
	}
	got, ok := m.TrackRoom(0)
	if !ok || got != hall {
		t.Fatalf("TrackRoom = %+v/%v, want hall/true", got, ok)
	}
	if _, ok := m.TrackRoom(3); ok {
		t.Fatal("out-of-range strip reports a room")
	}
	m.SetTrackRoom(9, hall) // ignored, no panic
}

// TestTrackRoomNone renders a sending track dry.
func TestTrackRoomNone(t *testing.T) {
	mk := func() *Mixer {
		m := New(48000, 2, 256)
		m.SetReverbWet(1)
		m.AddTrack(&noiseInstrument{noise: dsp.NewNoise(7), level: 0.5}, 0, 1)
		m.SetSend(0, 1)
		return m
	}
	wet := mk()
	hall, _ := dsp.PresetParams("hall")
	wet.SetTrackRoom(0, hall)
	wetPeak := float32(0)
	for k := 0; k < 40; k++ {
		wet.Process(256)
		if p := dspPeak(wet.Master()); p > wetPeak {
			wetPeak = p
		}
	}

	dry := mk()
	dry.SetTrackRoomNone(0)
	if _, ok := dry.TrackRoom(0); ok {
		t.Fatal("none-track reports an override")
	}
	dryPeak := float32(0)
	for k := 0; k < 40; k++ {
		dry.Process(256)
		if p := dspPeak(dry.Master()); p > dryPeak {
			dryPeak = p
		}
	}
	if !(wetPeak > dryPeak*1.05) {
		t.Fatalf("wet peak %v not above dry peak %v", wetPeak, dryPeak)
	}
}

// dspPeak returns the peak magnitude of an interleaved stereo bus.
func dspPeak(master []float32) float32 {
	peak := float32(0)
	for _, v := range master {
		a := v
		if a < 0 {
			a = -a
		}
		if a > peak {
			peak = a
		}
	}
	return peak
}

// TestTrackRoomCap merges the ninth space into its nearest neighbor.
func TestTrackRoomCap(t *testing.T) {
	m := New(48000, 12, 256)
	for k := 0; k < 10; k++ {
		m.AddTrack(&noiseInstrument{noise: dsp.NewNoise(uint64(k + 1)), level: 0.5}, 0, 0.8)
	}
	m.SetTrackRoom(0, roomParams(0.1, 0.5, 0.5))
	for k := 1; k <= 7; k++ {
		m.SetTrackRoom(k, roomParams(0.1+float32(k)*0.1, 0.5, 0.5))
	}
	// Nine distinct spaces would exceed the cap of eight: the 0.85 room
	// merges into its nearest neighbor instead of allocating.
	got := m.SetTrackRoom(8, roomParams(0.85, 0.5, 0.5))
	want := m.SetTrackRoom(7, roomParams(0.8, 0.5, 0.5))
	if got != want {
		t.Fatalf("merged index = %d, want neighbor %d", got, want)
	}
	if len(m.reverbs) > 9 {
		t.Fatalf("reverb instances = %d, want at most master + 8", len(m.reverbs))
	}
	m.Process(256) // all spaces render without errors
}

// TestMixerAccessors covers the trivial getters and setters.
func TestMixerAccessors(t *testing.T) {
	m := New(48000, 2, 256)
	if m.SampleRate() != 48000 {
		t.Fatalf("sample rate = %d", m.SampleRate())
	}
	m.AddTrack(&noiseInstrument{noise: dsp.NewNoise(1), level: 0.5}, 0, 0.8)
	m.SetPan(0, 2)
	m.SetVolume(0, 5)
	m.SetClipThreshold(0.5)
	if m.ClipThreshold() != 0.5 {
		t.Fatalf("clip threshold = %v", m.ClipThreshold())
	}
	if m.ReverbWet() != DefaultReverbWet {
		t.Fatalf("wet = %v", m.ReverbWet())
	}
	m.Process(128)
	if m.TrackPeak(0) < 0 || m.TrackPeak(9) != 0 {
		t.Fatalf("track peaks %v/%v", m.TrackPeak(0), m.TrackPeak(9))
	}
}
