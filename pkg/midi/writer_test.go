package midi

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestWriteVLQ(t *testing.T) {
	vectors := []struct {
		value uint32
		bytes []byte
	}{
		{0, []byte{0x00}},
		{0x40, []byte{0x40}},
		{0x7F, []byte{0x7F}},
		{0x80, []byte{0x81, 0x00}},
		{0x2000, []byte{0xC0, 0x00}},
		{0x3FFF, []byte{0xFF, 0x7F}},
		{0x1FFFFF, []byte{0xFF, 0xFF, 0x7F}},
		{0x0FFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
		{0x7FFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0x7F}}, // clamped to 28 bits
	}
	for _, tc := range vectors {
		if got := WriteVLQ(tc.value); !bytes.Equal(got, tc.bytes) {
			t.Errorf("WriteVLQ(%#x) = % x, want % x", tc.value, got, tc.bytes)
		}
	}
	// Encoder/decoder round trip on random values.
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 10000; i++ {
		v := rng.Uint32() & 0x0FFFFFFF
		enc := WriteVLQ(v)
		got, pos, err := readVLQ(enc, 0)
		if err != nil || got != v || pos != len(enc) {
			t.Fatalf("round trip %#x -> % x -> %#x (pos %d, err %v)", v, enc, got, pos, err)
		}
	}
}

func TestEncodeTrackRunningStatus(t *testing.T) {
	events := []Event{
		{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 100},
		{Tick: 96, Type: EventNoteOn, Channel: 0, Data1: 62, Data2: 100},
		{Tick: 192, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 40},
		{Tick: 192, Type: EventNoteOff, Channel: 0, Data1: 62, Data2: 40},
	}
	payload := EncodeTrack(events)
	// Expected bytes: explicit 0x90 once, then running status; explicit
	// 0x80 once, then running.
	want := []byte{
		0x00, 0x90, 0x3C, 0x64,
		0x60, 0x3E, 0x64,
		0x60, 0x80, 0x3C, 0x28,
		0x00, 0x3E, 0x28,
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("payload = % x, want % x", payload, want)
	}
	// The reader must decode it back identically.
	back, err := parseTrack(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 4 {
		t.Fatalf("decoded %d events, want 4", len(back))
	}
}

func TestEncodeTrackSameTickOrdering(t *testing.T) {
	// Note-off must precede the retriggered note-on at the same tick, and
	// end-of-track stays last.
	events := []Event{
		{Tick: 100, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 90},
		{Tick: 100, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 40},
		{Tick: 100, Type: EventMeta, Data1: MetaEndOfTrack},
	}
	payload := EncodeTrack(events)
	back, err := parseTrack(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("events = %d", len(back))
	}
	if back[0].Type != EventNoteOff || back[1].Type != EventNoteOn ||
		back[2].Type != EventMeta || back[2].Data1 != MetaEndOfTrack {
		t.Fatalf("same-tick order wrong: %v %v %v", back[0].Type, back[1].Type, back[2].Type)
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	in := &File{
		Header: Header{Format: 1, Tracks: 2, TicksPerQuarter: 480},
		Tracks: [][]Event{
			{
				{Tick: 0, Type: EventMeta, Data1: MetaTempo, Data: be24(500000)},
				{Tick: 0, Type: EventMeta, Data1: MetaTimeSignature, Data: []byte{3, 2, 24, 8}},
				{Tick: 0, Type: EventMeta, Data1: MetaTrackName, Data: []byte("Conductor")},
				{Tick: 5760, Type: EventMeta, Data1: MetaEndOfTrack},
			},
			{
				// Same-tick events are listed in the writer's canonical
				// order (meta before sysex) so the round trip is exact.
				{Tick: 0, Type: EventMeta, Data1: MetaTrackName, Data: []byte("Everything")},
				{Tick: 0, Type: EventSysex, Data1: 0xF0, Data: []byte{0x7E, 0x7F, 0x09, 0x01, 0xF7}},
				{Tick: 0, Type: EventProgramChange, Channel: 3, Data1: 79},
				{Tick: 0, Type: EventControlChange, Channel: 3, Data1: 7, Data2: 100},
				{Tick: 0, Type: EventNoteOn, Channel: 3, Data1: 60, Data2: 100},
				// Pitch bend: Value is canonical; Data1 mirrors the wire
				// LSB ((Value+8192) & 0x7F = 46) after a read-back.
				{Tick: 240, Type: EventPitchBend, Channel: 3, Data1: 46, Value: -1234},
				{Tick: 300, Type: EventPolyPressure, Channel: 3, Data1: 60, Data2: 80},
				{Tick: 360, Type: EventChannelPressure, Channel: 3, Data1: 90},
				{Tick: 480, Type: EventNoteOff, Channel: 3, Data1: 60, Data2: 40},
				{Tick: 480, Type: EventMeta, Data1: MetaEndOfTrack},
			},
		},
	}
	data := WriteFile(in)
	out, err := ReadFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Header.Format != 1 || out.Header.Tracks != 2 || out.Header.TicksPerQuarter != 480 {
		t.Fatalf("header = %+v", out.Header)
	}
	if len(out.Tracks) != 2 {
		t.Fatalf("tracks = %d", len(out.Tracks))
	}
	for ti := range in.Tracks {
		a, b := in.Tracks[ti], out.Tracks[ti]
		if len(a) != len(b) {
			t.Fatalf("track %d: %d events out, %d in", ti, len(b), len(a))
		}
		for i := range a {
			x, y := a[i], b[i]
			if x.Tick != y.Tick || x.Type != y.Type || x.Channel != y.Channel ||
				x.Data1 != y.Data1 || x.Data2 != y.Data2 || x.Value != y.Value ||
				!bytes.Equal(x.Data, y.Data) {
				t.Errorf("track %d event %d:\n in  %+v\n out %+v", ti, i, x, y)
			}
		}
	}
}

func TestWriteFileFormat0Flattens(t *testing.T) {
	in := &File{
		Header: Header{Format: 0, TicksPerQuarter: 96},
		Tracks: [][]Event{
			{{Tick: 0, Type: EventMeta, Data1: MetaTempo, Data: be24(600000)}},
			{
				{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 90},
				{Tick: 96, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 40},
				{Tick: 96, Type: EventMeta, Data1: MetaEndOfTrack},
			},
		},
	}
	out, err := ReadFile(WriteFile(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tracks) != 1 {
		t.Fatalf("format 0 kept %d tracks, want 1", len(out.Tracks))
	}
	if len(out.Tracks[0]) != 4 {
		t.Fatalf("merged track has %d events, want 4", len(out.Tracks[0]))
	}
}
