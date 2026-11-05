package gui

import (
	"encoding/binary"
	"errors"
	"image/color"
	"math"
	"sync"
	"testing"

	"xaux_gui/pkg/vad"
)

type plotTestDetector struct {
	err error
}

func (d plotTestDetector) Process(samples []int16) (bool, error) {
	return samples[0] > 0, d.err
}

func (plotTestDetector) Close() error { return nil }

func plotTestPCM(value int16, frames int) []byte {
	data := make([]byte, vad.FrameSamples*2*frames)
	for i := 0; i < len(data); i += 2 {
		binary.LittleEndian.PutUint16(data[i:], uint16(value))
	}
	return data
}

func TestVADScopeFramesLevelsAndHistory(t *testing.T) {
	s := &vadScope{detector: plotTestDetector{}}
	quiet := plotTestPCM(0, 1)
	for _, b := range quiet {
		if err := s.process([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.process(plotTestPCM(16384, 1)); err != nil {
		t.Fatal(err)
	}
	snapshot := s.snapshot()
	if snapshot.total != 2 || snapshot.voiced != 1 || len(snapshot.frames) != 2 {
		t.Fatalf("wrong frame accounting: %+v", snapshot)
	}
	if snapshot.frames[0].speech || snapshot.frames[0].level != -96 ||
		!snapshot.frames[1].speech || math.Abs(snapshot.frames[1].level+6.0206) > 0.001 {
		t.Fatalf("wrong detector state / dBFS: %+v", snapshot.frames)
	}
	if err := s.process(plotTestPCM(-32768, vadPlotFrames+2)); err != nil {
		t.Fatal(err)
	}
	latest := s.snapshot()
	if len(latest.frames) != vadPlotFrames || latest.total != vadPlotFrames+4 || latest.voiced != 1 {
		t.Fatalf("history is unbounded or lost cumulative counts: %+v", latest)
	}
	if latest.frames[0].low != -1 || latest.frames[len(latest.frames)-1].level != 0 {
		t.Fatal("history is not ordered, or negative samples were decoded incorrectly")
	}
	if snapshot.frames[1].high != 0.5 {
		t.Fatal("snapshot changed when capture continued")
	}
}

func TestVADScopeErrorAndConcurrentSnapshots(t *testing.T) {
	want := errors.New("native VAD error")
	broken := &vadScope{detector: plotTestDetector{err: want}}
	if err := broken.process(plotTestPCM(1, 1)); !errors.Is(err, want) {
		t.Fatalf("VAD error lost: %v", err)
	}
	if broken.snapshot().total != 0 {
		t.Fatal("failed frame was displayed as a successful decision")
	}
	s := &vadScope{detector: plotTestDetector{}}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for range 600 {
			if err := s.process(plotTestPCM(1, 1)); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for range 600 {
		snapshot := s.snapshot()
		if len(snapshot.frames) > vadPlotFrames {
			t.Error("history exceeds eight seconds")
		}
	}
	workers.Wait()
	if snapshot := s.snapshot(); snapshot.total != 600 || snapshot.voiced != 600 {
		t.Fatalf("concurrent snapshots lost frames: %+v", snapshot)
	}
}

func TestVADPlotDrawsWaveformAndDecision(t *testing.T) {
	frames := []vadPlotFrame{{}, {low: -0.5, high: 0.25, speech: true}}
	img := renderVADPlot(frames, 1000, 300)
	cases := []struct {
		x, y int
		want color.NRGBA
	}{
		{998, 105, vadPlotVoice},
		{998, 234, vadPlotVoice},
		{998, 12, vadPlotHighlight},
		{996, 276, vadPlotQuiet},
		{100, 12, vadPlotBackground},
		{125, 12, vadPlotGrid},
	}
	for _, tc := range cases {
		if got := color.NRGBAModel.Convert(img.At(tc.x, tc.y)); got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
	if img := renderVADPlot(nil, 0, 0); img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Fatal("zero-size layout must remain safe")
	}
}
