package ffaudio

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func requireHardware(t *testing.T) {
	t.Helper()
	if os.Getenv("FFAUDIO_HARDWARE_TEST") != "1" {
		t.Skip("set FFAUDIO_HARDWARE_TEST=1 to run real-device capture tests")
	}
	if err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(UnInit)
}

func TestListDevicesHardware(t *testing.T) {
	requireHardware(t)
	devices, err := GetDevPlaybackAndCapture()
	if err != nil {
		t.Fatalf("GetDevPlaybackAndCapture: %v", err)
	}
	t.Logf("capture=%v playback=%v", devices.CaptureDevNameList, devices.PlayBackDevNameList)
}

type pcmCountWriter struct {
	bytes          atomic.Int64
	convertedBytes atomic.Int64
	converter      *PCMConverter
}

func (w *pcmCountWriter) Write(p []byte) (int, error) {
	w.bytes.Add(int64(len(p)))
	if w.converter != nil {
		converted, err := w.converter.Convert(p)
		if err != nil {
			return 0, err
		}
		w.convertedBytes.Add(int64(len(converted)))
	}
	return len(p), nil
}

func TestCaptureHardware(t *testing.T) {
	requireHardware(t)
	devices, err := GetDevPlaybackAndCapture()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		mode       int32
		deviceName string
	}{
		{name: "microphone", mode: ModeCapture, deviceName: devices.CaptureDefault},
		{name: "speaker-loopback", mode: ModeLoopBack, deviceName: devices.PlaybackDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.deviceName == "" {
				t.Skip("no device available")
			}
			audio := NewFFAudio()
			writer := &pcmCountWriter{}
			audio.Writer = writer
			if err := audio.Open(&Config{DeviceName: tt.deviceName}, tt.mode); err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = audio.Close() })
			if audio.SampleRate <= 0 || audio.Channels <= 0 || !validPCMFormat(PCMFormat{
				SampleRate: audio.SampleRate, Channels: audio.Channels, Format: audio.Format,
			}) {
				t.Fatalf("invalid negotiated format: rate=%d channels=%d format=%d",
					audio.SampleRate, audio.Channels, audio.Format)
			}
			t.Parallel() // Exercise microphone and loopback simultaneously.
			t.Logf("device=%q rate=%d channels=%d format=%d", tt.deviceName,
				audio.SampleRate, audio.Channels, audio.Format)
			converter, err := NewPCMConverter(
				PCMFormat{SampleRate: audio.SampleRate, Channels: audio.Channels, Format: audio.Format},
				PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatInt16},
			)
			if err != nil {
				t.Fatalf("NewPCMConverter: %v", err)
			}
			writer.converter = converter

			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() { errCh <- audio.ListenAndRun(ctx) }()
			time.Sleep(100 * time.Millisecond)
			if err := audio.Pause(); err != nil {
				cancel()
				t.Fatalf("Pause: %v", err)
			}
			time.Sleep(20 * time.Millisecond)
			if err := audio.Resume(); err != nil {
				cancel()
				t.Fatalf("Resume: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
			cancel()
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("ListenAndRun: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("capture did not stop after cancellation")
			}
			if writer.bytes.Load() == 0 {
				if tt.mode == ModeLoopBack {
					// WASAPI may report an empty buffer while the render
					// endpoint is silent; open/read/pause/resume still passed.
					if os.Getenv("FFAUDIO_REQUIRE_LOOPBACK_DATA") == "1" {
						t.Fatal("loopback returned no PCM while data was required")
					}
					t.Log("loopback endpoint was silent; no PCM was produced")
				} else {
					t.Fatal("capture returned no PCM data")
				}
			} else if writer.convertedBytes.Load() == 0 {
				t.Fatal("native PCM was captured but no 16 kHz/int16/mono output was produced")
			}
		})
	}
}
