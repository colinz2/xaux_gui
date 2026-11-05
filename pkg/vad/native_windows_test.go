//go:build tenvad && windows && cgo

package vad

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeSilenceAndSpeech(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	samples := make([]int16, FrameSamples)
	for range 125 {
		speech, err := d.Process(samples)
		if err != nil || speech {
			t.Fatalf("silence: speech=%v err=%v", speech, err)
		}
	}
	_, source, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "ten-vad", "examples", "s0724-s0730.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatal("invalid upstream WAV fixture")
	}
	var pcm []byte
	validFormat := false
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4:]))
		start := offset + 8
		if size > len(data)-start {
			t.Fatal("truncated WAV chunk")
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			format := data[start : start+size]
			validFormat = len(format) >= 16 &&
				binary.LittleEndian.Uint16(format) == 1 &&
				binary.LittleEndian.Uint16(format[2:]) == 1 &&
				binary.LittleEndian.Uint32(format[4:]) == 16000 &&
				binary.LittleEndian.Uint16(format[14:]) == 16
		case "data":
			pcm = data[start : start+size]
		}
		offset = start + size + size%2
	}
	if !validFormat || len(pcm) == 0 {
		t.Fatal("fixture must be 16 kHz mono int16")
	}
	voiced := 0
	for len(pcm) >= FrameSamples*2 {
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		}
		speech, err := d.Process(samples)
		if err != nil {
			t.Fatal(err)
		}
		if speech {
			voiced++
		}
		pcm = pcm[FrameSamples*2:]
	}
	if voiced == 0 {
		t.Fatal("TEN VAD did not detect speech in upstream speech fixture")
	}
	t.Logf("TEN VAD detected %d voiced frames in upstream fixture", voiced)
}
