package wav

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// TestExtensible24In32BitContainer verifies decoding 24-bit samples packed inside 32-bit container in WAVE_FORMAT_EXTENSIBLE.
func TestExtensible24In32BitContainer(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(40))
	binary.Write(&buf, binary.LittleEndian, uint16(Extensible))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint32(48000))
	binary.Write(&buf, binary.LittleEndian, uint32(192000))
	binary.Write(&buf, binary.LittleEndian, uint16(4))
	binary.Write(&buf, binary.LittleEndian, uint16(32))

	binary.Write(&buf, binary.LittleEndian, uint16(22))
	binary.Write(&buf, binary.LittleEndian, uint16(24))
	binary.Write(&buf, binary.LittleEndian, uint32(1))
	binary.Write(&buf, binary.LittleEndian, uint32(1))
	buf.Write(make([]byte, 12))

	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(8))
	binary.Write(&buf, binary.LittleEndian, int32(0x7FFFFF00))
	binary.Write(&buf, binary.LittleEndian, int32(-1073741824))

	raw := buf.Bytes()
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))

	info, samples, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if info.BitsPerSample != 32 {
		t.Fatalf("BitsPerSample = %d, want 32", info.BitsPerSample)
	}
	if info.Frames != 2 || len(samples) != 2 {
		t.Fatalf("frames = %d, samples = %d, want 2", info.Frames, len(samples))
	}
	if math.Abs(float64(samples[0])-0.99999988) > 1e-5 {
		t.Fatalf("sample 0 = %v, want ~1.0", samples[0])
	}
	if math.Abs(float64(samples[1])-(-0.5)) > 1e-5 {
		t.Fatalf("sample 1 = %v, want -0.5", samples[1])
	}
}

// TestRIFFOddChunkAlignment verifies RIFF odd chunk size word alignment padding in WAVE streams.
func TestRIFFOddChunkAlignment(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(PCM16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint32(48000))
	binary.Write(&buf, binary.LittleEndian, uint32(96000))
	binary.Write(&buf, binary.LittleEndian, uint16(2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))

	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	buf.Write([]byte{0x00, 0x40, 0xFF})
	buf.WriteByte(0x00)

	buf.WriteString("smpl")
	binary.Write(&buf, binary.LittleEndian, uint32(36+24))
	var smplHdr [36]byte
	binary.LittleEndian.PutUint32(smplHdr[28:32], 1)
	buf.Write(smplHdr[:])

	var loopHdr [24]byte
	binary.LittleEndian.PutUint32(loopHdr[8:12], 10)
	binary.LittleEndian.PutUint32(loopHdr[12:16], 20)
	buf.Write(loopHdr[:])

	raw := buf.Bytes()
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))

	info, samples, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode with odd data chunk failed: %v", err)
	}
	if info.Frames != 1 || len(samples) != 1 {
		t.Fatalf("Frames = %d, samples = %d, want 1", info.Frames, len(samples))
	}
	if !info.HasLoop || info.LoopStart != 10 || info.LoopEnd != 20 {
		t.Fatalf("Loop metadata corrupt after odd data chunk: %+v", info)
	}
}

// TestTruncatedDataChunkNoOverSeek verifies truncated data chunk parsing avoids seeking past end of stream.
func TestTruncatedDataChunkNoOverSeek(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(PCM16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint32(48000))
	binary.Write(&buf, binary.LittleEndian, uint32(96000))
	binary.Write(&buf, binary.LittleEndian, uint16(2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))

	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(100000))
	binary.Write(&buf, binary.LittleEndian, int16(16384))
	binary.Write(&buf, binary.LittleEndian, int16(-16384))

	raw := buf.Bytes()
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))

	info, samples, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode truncated file failed: %v", err)
	}
	if info.Frames != 2 || len(samples) != 2 {
		t.Fatalf("Frames = %d, samples = %d, want 2", info.Frames, len(samples))
	}
	if math.Abs(float64(samples[0])-0.5) > 1e-4 {
		t.Fatalf("sample 0 = %v, want 0.5", samples[0])
	}
	if math.Abs(float64(samples[1])-(-0.5)) > 1e-4 {
		t.Fatalf("sample 1 = %v, want -0.5", samples[1])
	}
}
