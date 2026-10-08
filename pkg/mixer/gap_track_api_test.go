package mixer

import (
	"testing"

	"resonata/pkg/dsp"
)

// TestGapNewClamps proves New sanitizes non-positive construction args.
func TestGapNewClamps(t *testing.T) {
	m := New(0, 0, 0)
	if m.SampleRate() != 48000 {
		t.Fatalf("SampleRate() = %d, want 48000", m.SampleRate())
	}
	if got := m.AddTrack(&dcInstrument{level: 0.1}, 0, 1); got != 0 {
		t.Fatalf("AddTrack on clamped mixer = %d, want 0", got)
	}
	m2 := New(-44100, -5, -10)
	if m2.SampleRate() != 48000 {
		t.Fatalf("SampleRate() = %d, want 48000", m2.SampleRate())
	}
}

// TestGapSendInvalidIndex proves out-of-range send access is safe:
// reads report 0 and writes are ignored.
func TestGapSendInvalidIndex(t *testing.T) {
	m := New(48000, 2, 64)
	if got := m.TrackSend(-1); got != 0 {
		t.Fatalf("TrackSend(-1) = %v, want 0", got)
	}
	if got := m.TrackSend(99); got != 0 {
		t.Fatalf("TrackSend(99) = %v, want 0", got)
	}
	m.SetSend(-1, 1)
	m.SetSend(99, 1)
	i := m.AddTrack(&dcInstrument{level: 0.1}, 0, 1)
	m.SetSend(i, 2.5) // clamped to 1
	if got := m.TrackSend(i); got != 1 {
		t.Fatalf("TrackSend clamped = %v, want 1", got)
	}
}

// TestGapDelayNilAndInvalid proves TrackDelay is nil before assignment
// and that invalid indices never panic or attach.
func TestGapDelayNilAndInvalid(t *testing.T) {
	m := New(48000, 2, 64)
	i := m.AddTrack(&dcInstrument{level: 0.1}, 0, 1)
	if m.TrackDelay(i) != nil {
		t.Fatal("TrackDelay before SetTrackDelay != nil, want nil")
	}
	if m.TrackDelay(-1) != nil || m.TrackDelay(99) != nil {
		t.Fatal("TrackDelay(invalid) != nil, want nil")
	}
	m.SetTrackDelay(-1, dsp.DelayConfig{Wet: 0.5}, 120)
	m.SetTrackDelay(99, dsp.DelayConfig{Wet: 0.5}, 120)
	m.SetTrackDelay(i, delayCfg(0.05, 0.5), 120)
	if m.TrackDelay(i) == nil {
		t.Fatal("TrackDelay after SetTrackDelay = nil, want instance")
	}
	if m.TrackDelay(-1) != nil {
		t.Fatal("TrackDelay(-1) changed after invalid SetTrackDelay")
	}
}
