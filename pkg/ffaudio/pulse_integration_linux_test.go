//go:build linux

package ffaudio

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type pulseSignalWriter struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (w *pulseSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.Write(p)
}

func (w *pulseSignalWriter) snapshot() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data.Bytes()...)
}

// This opt-in test expects a dedicated PulseAudio server with virtual devices.
func TestPulseVirtualDevices(t *testing.T) {
	if os.Getenv("XAUX_PULSE_VIRTUAL_TEST") != "1" {
		t.Skip("set XAUX_PULSE_VIRTUAL_TEST=1 for the isolated virtual-device test")
	}
	if os.Getenv("PULSE_SERVER") == "" {
		t.Fatal("PULSE_SERVER must point to the dedicated virtual-device server")
	}
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(UnInit)

	playback, err := ListDevPlayback()
	if err != nil {
		t.Fatal(err)
	}
	capture, err := ListDevCapture()
	if err != nil {
		t.Fatal(err)
	}
	var sinkA, sinkB, microphone string
	for _, dev := range playback {
		switch dev.DevIDStr {
		case "xaux_sink_a":
			sinkA = dev.Name
			if dev.IsDefault {
				t.Fatal("sink A should not be the default")
			}
		case "xaux_sink_b":
			sinkB = dev.Name
			if !dev.IsDefault {
				t.Fatal("sink B should be the default")
			}
		}
	}
	for _, dev := range capture {
		if strings.Contains(dev.DevIDStr, ".monitor") {
			t.Fatalf("monitor appeared in microphone list: %+v", dev)
		}
		if dev.DevIDStr == "xaux_virtual_mic" {
			microphone = dev.Name
			if !dev.IsDefault {
				t.Fatal("virtual microphone should be the default")
			}
		}
	}
	if sinkA == "" || sinkB == "" || microphone == "" {
		t.Fatalf("virtual devices not found: playback=%+v capture=%+v", playback, capture)
	}
	if sinkA == sinkB {
		t.Fatalf("output devices have ambiguous names: %q", sinkA)
	}

	for _, tc := range []struct {
		name       string
		mode       int32
		deviceName string
		frequency  int
	}{
		{"selected sink A", ModeLoopBack, sinkA, 440},
		{"selected sink B", ModeLoopBack, sinkB, 880},
		{"default sink", ModeLoopBack, "", 880},
		{"selected microphone", ModeCapture, microphone, 440},
		{"default microphone", ModeCapture, "", 440},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			audio := NewFFAudio()
			writer := &pulseSignalWriter{}
			audio.Writer = writer
			if err := audio.Open(&Config{DeviceName: tc.deviceName, BufferLengthMsec: 20}, tc.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = audio.Close() })
			if audio.Format != FormatInt16 || audio.Channels != 1 || audio.SampleRate != 16000 {
				t.Fatalf("unexpected PCM format: %d/%d/%d", audio.Format, audio.Channels, audio.SampleRate)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			startedAt := time.Now()
			go func() { done <- audio.ListenAndRun(ctx) }()
			deadline := time.Now().Add(8 * time.Second)
			for len(writer.snapshot()) < audio.SampleRate/5 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			beforePause := len(writer.snapshot())
			if beforePause < audio.SampleRate/5 {
				t.Fatalf("virtual source produced insufficient PCM: %d bytes", beforePause)
			}
			t.Logf("received first 100 ms of PCM after %s", time.Since(startedAt).Round(time.Millisecond))
			if err := audio.Pause(); err != nil {
				t.Fatal(err)
			}
			resumedAt := time.Now()
			if err := audio.Resume(); err != nil {
				t.Fatal(err)
			}
			deadline = time.Now().Add(8 * time.Second)
			for len(writer.snapshot()) < beforePause+audio.SampleRate/5 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("capture did not stop")
			}
			data := writer.snapshot()
			if len(data) < beforePause+audio.SampleRate/5 {
				t.Fatalf("virtual source produced no PCM after resume: %d bytes", len(data)-beforePause)
			}
			t.Logf("received 100 ms of PCM after resume in %s", time.Since(resumedAt).Round(time.Millisecond))
			crossings := 0
			var previous int16
			for i := 0; i+1 < len(data); i += 2 {
				sample := int16(binary.LittleEndian.Uint16(data[i:]))
				if previous <= 0 && sample > 0 {
					crossings++
				}
				previous = sample
			}
			frequency := crossings * audio.SampleRate / (len(data) / 2)
			if frequency < tc.frequency*9/10 || frequency > tc.frequency*11/10 {
				t.Fatalf("captured approximately %d Hz, want %d Hz", frequency, tc.frequency)
			}
		})
	}
}
