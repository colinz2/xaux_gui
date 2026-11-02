package sound_cap

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "recording.wav")
	writer, err := newWAVWriter(path, 16000, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 0, 2, 0, 3, 0}
	if n, err := writer.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("Write n=%d err=%v", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 44+len(payload) {
		t.Fatalf("file size=%d, want %d", len(data), 44+len(payload))
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || string(data[36:40]) != "data" {
		t.Fatalf("invalid WAV signatures: %q %q %q", data[:4], data[8:12], data[36:40])
	}
	if got := binary.LittleEndian.Uint32(data[4:8]); got != uint32(36+len(payload)) {
		t.Fatalf("RIFF size=%d", got)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != uint32(len(payload)) {
		t.Fatalf("data size=%d", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 16000 {
		t.Fatalf("sample rate=%d", got)
	}
	if string(data[44:]) != string(payload) {
		t.Fatalf("payload=%v, want %v", data[44:], payload)
	}
}
