package midi

import (
	"resonata/pkg/score"
	"testing"
)

// TestMidiCoverageEdges verifies MIDI parser and converter boundary conditions and error handling.
func TestMidiCoverageEdges(t *testing.T) {
	_, p := ReadVLQ([]byte{1, 2}, 5)
	if p != 2 {
		t.Fatalf("expected p=2, got %d", p)
	}

	if _, err := parseTrack([]byte{0x80}); err == nil {
		t.Fatal("expected error on truncated VLQ")
	}
	if _, err := parseTrack([]byte{0x00, 0xF1}); err == nil {
		t.Fatal("expected error on truncated system message")
	}

	if mpq := MicrosFromBPM(1); mpq != 6000000 {
		t.Fatalf("expected 6000000, got %d", mpq)
	}
	if mpq := MicrosFromBPM(-5); mpq != 500000 {
		t.Fatalf("expected 500000, got %d", mpq)
	}

	fTS := &File{
		Tracks: [][]Event{
			{
				{Tick: 10, Type: EventMeta, Data1: MetaTimeSignature, Data: []byte{3, 2}},
				{Tick: 0, Type: EventMeta, Data1: MetaTimeSignature, Data: []byte{4, 2}},
				{Tick: 5, Type: EventMeta, Data1: MetaTimeSignature, Data: []byte{4, 6}},
			},
		},
	}
	tsList := fTS.TimeSignatures()
	if len(tsList) != 2 || tsList[0].Tick != 0 {
		t.Fatalf("unexpected time signatures: %+v", tsList)
	}

	if _, err := ToScore(nil); err == nil {
		t.Fatal("expected error for nil file")
	}
	if _, err := ToScore(&File{}); err == nil {
		t.Fatal("expected error for empty file")
	}
	if _, err := ToScore(&File{Header: Header{TicksPerQuarter: 0}, Tracks: [][]Event{{{}}}}); err == nil {
		t.Fatal("expected error for 0 PPQ")
	}

	fNoNotes := &File{
		Header: Header{Format: 1, TicksPerQuarter: 480},
		Tracks: [][]Event{{{Tick: 0, Type: EventMeta, Data1: MetaEndOfTrack}}},
	}
	if _, err := ToScore(fNoNotes); err == nil {
		t.Fatal("expected error for file with no notes")
	}

	fBpm := &File{
		Header: Header{Format: 1, TicksPerQuarter: 480},
		Tracks: [][]Event{
			{
				{Tick: 0, Type: EventMeta, Data1: MetaTempo, Data: []byte{0x00, 0x01, 0x00}},
				{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 64},
				{Tick: 10, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 64},
			},
		},
	}
	sBpm, err := ToScore(fBpm)
	if err != nil || sBpm.Metadata.BPM != 1000 {
		t.Fatalf("expected clamped BPM 1000, got %v (%v)", sBpm.Metadata.BPM, err)
	}

	fCh := &File{
		Header: Header{Format: 1, TicksPerQuarter: 480},
		Tracks: [][]Event{
			{
				{Tick: 0, Type: EventControlChange, Channel: 0, Data1: 64, Data2: 127},
				{Tick: 5, Type: EventControlChange, Channel: 0, Data1: 64, Data2: 0},
				{Tick: 0, Type: EventNoteOn, Channel: 9, Data1: 36, Data2: 64},
				{Tick: 10, Type: EventNoteOff, Channel: 9, Data1: 36, Data2: 64},
				{Tick: 0, Type: EventNoteOn, Channel: 0, Data1: 60, Data2: 64},
				{Tick: 10, Type: EventNoteOff, Channel: 0, Data1: 60, Data2: 64},
			},
		},
	}
	if _, err := ToScore(fCh); err != nil {
		t.Fatal(err)
	}

	vBass := familyVoicing(32, false)
	if vBass["brightness"] != 0.3 {
		t.Fatalf("expected bass voicing, got %+v", vBass)
	}
	vLead := familyVoicing(80, false)
	if vLead["brightness"] != 0.55 {
		t.Fatalf("expected lead voicing, got %+v", vLead)
	}

	tracks := make([]score.Track, 11)
	for i := range tracks {
		tracks[i] = score.Track{
			ID:         "tr",
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Notes: []score.NoteEvent{
				{Pitch: 60, Time: -1, Duration: 0},
				{Pitch: 62, Time: 1e12, Duration: 1},
			},
		}
	}
	sMany := &score.Score{
		Metadata: score.Metadata{BPM: 0},
		Tracks:   tracks,
	}
	fMany := FromScore(sMany, 0)
	if fMany == nil || fMany.Header.TicksPerQuarter != DefaultTicksPerQuarter {
		t.Fatalf("bad FromScore result: %+v", fMany)
	}

	evs := []Event{
		{Tick: 0, Type: EventSysex, Data1: 0, Data: []byte{0x7E, 0x7F}},
	}
	enc := EncodeTrack(evs)
	if len(enc) == 0 {
		t.Fatal("empty encoded track")
	}

	fZeroPPQ := &File{
		Header: Header{Format: 1, TicksPerQuarter: 0},
		Tracks: [][]Event{{{Tick: 0, Type: EventMeta, Data1: MetaEndOfTrack}}},
	}
	raw := WriteFile(fZeroPPQ)
	if len(raw) == 0 {
		t.Fatal("empty WriteFile output")
	}

	if st := statusByte(Event{Type: EventMeta}); st != 0 {
		t.Fatalf("expected 0 for meta status byte, got %d", st)
	}
}
