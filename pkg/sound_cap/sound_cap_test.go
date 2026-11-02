package sound_cap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"xaux_gui/pkg/ffaudio"
)

func TestNewSoundCapValidatesConfigBeforeConnecting(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{name: "nil config"},
		{name: "invalid mode", config: &Config{ProxyAddr: "unused"}},
		{name: "invalid ASR sample rate", config: &Config{
			ProxyAddr: "unused", Mode: CaptureMicrophone, SampleRate: 48000,
		}},
		{name: "invalid ASR channels", config: &Config{
			ProxyAddr: "unused", Mode: CaptureMicrophone, Channels: 2,
		}},
		{name: "invalid ASR sample size", config: &Config{
			ProxyAddr: "unused", Mode: CaptureMicrophone, BitsPerSample: 32,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture, err := NewSoundCap(context.Background(), tt.config, nil)
			if err == nil {
				if capture != nil {
					capture.Close()
				}
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestNewSoundCapMarksProxyUnavailable(t *testing.T) {
	capture, err := NewSoundCap(context.Background(), &Config{
		ProxyAddr: "invalid address",
		Mode:      CaptureMicrophone,
	}, nil)
	if err == nil {
		if capture != nil {
			capture.Close()
		}
		t.Fatal("expected proxy connection error")
	}
	if !errors.Is(err, ErrASRProxyUnavailable) {
		t.Fatalf("error does not identify an unavailable proxy: %v", err)
	}
}

func TestNewSoundCapRecordingOnly(t *testing.T) {
	capture, err := NewSoundCap(context.Background(), &Config{Mode: CaptureMicrophone}, nil)
	if err != nil {
		t.Fatalf("NewSoundCap() without ASR proxy: %v", err)
	}
	defer capture.Close()
	if capture.asrClient != nil {
		t.Fatal("recording-only capture created an ASR client")
	}
	if capture.SampleRate != 16000 || capture.Channels != 1 || capture.BitsPerSample != 16 {
		t.Fatalf("unexpected output format: rate=%d channels=%d bits=%d",
			capture.SampleRate, capture.Channels, capture.BitsPerSample)
	}
}

func TestRecordingOnlyWrite(t *testing.T) {
	recordPath := filepath.Join(t.TempDir(), "recording-only.wav")
	capture, err := NewSoundCap(context.Background(), &Config{
		Mode: CaptureMicrophone, RecordFilePath: recordPath,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	format := ffaudio.PCMFormat{SampleRate: 16000, Channels: 1, Format: ffaudio.FormatInt16}
	capture.converter, err = ffaudio.NewPCMConverter(format, format)
	if err != nil {
		t.Fatal(err)
	}
	capture.recording, err = newWAVWriter(recordPath, 16000, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	capture.transitionState(StateCreated, StateStarting)
	capture.transitionState(StateStarting, StateRunning)

	payload := []byte{0, 0, 1, 0, 0xff, 0xff}
	if n, err := capture.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("Write() n=%d err=%v", n, err)
	}
	capture.Close()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 44+len(payload) || !bytes.Equal(data[44:], payload) {
		t.Fatalf("unexpected WAV payload: length=%d data=%v", len(data), data)
	}
}

func TestInvalidCaptureModeError(t *testing.T) {
	_, err := NewSoundCap(context.Background(), &Config{ProxyAddr: "unused"}, nil)
	if !errors.Is(err, ErrInvalidCaptureMode) {
		t.Fatalf("error=%v, want ErrInvalidCaptureMode", err)
	}
}
