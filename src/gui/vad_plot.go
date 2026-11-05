package gui

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"xaux_gui/pkg/vad"
)

const vadPlotFrames = 500 // Eight seconds at 16 ms per frame.

type vadPlotFrame struct {
	low, high float64
	level     float64
	speech    bool
}

type vadPlotSnapshot struct {
	frames        []vadPlotFrame
	total, voiced uint64
}

// Capture writes complete detector frames; the UI reads bounded snapshots.
type vadScope struct {
	mu       sync.Mutex
	detector vad.Detector
	pending  []byte
	history  [vadPlotFrames]vadPlotFrame
	total    uint64
	voiced   uint64
}

func (s *vadScope) process(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, pcm...)
	var samples [vad.FrameSamples]int16
	for len(s.pending) >= vad.FrameSamples*2 {
		frame := vadPlotFrame{}
		var sum float64
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(s.pending[i*2:]))
			value := float64(samples[i]) / 32768
			frame.low = min(frame.low, value)
			frame.high = max(frame.high, value)
			sum += value * value
		}
		var err error
		frame.speech, err = s.detector.Process(samples[:])
		if err != nil {
			return err
		}
		frame.level = max(-96, 20*math.Log10(max(math.Sqrt(sum/vad.FrameSamples), 1e-10)))
		s.history[s.total%vadPlotFrames] = frame
		s.total++
		if frame.speech {
			s.voiced++
		}
		s.pending = s.pending[vad.FrameSamples*2:]
	}
	if len(s.pending) == 0 {
		s.pending = nil
	}
	return nil
}

func (s *vadScope) snapshot() vadPlotSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := min(s.total, uint64(vadPlotFrames))
	snapshot := vadPlotSnapshot{
		frames: make([]vadPlotFrame, count), total: s.total, voiced: s.voiced,
	}
	for i := range snapshot.frames {
		snapshot.frames[i] = s.history[(s.total-count+uint64(i))%vadPlotFrames]
	}
	return snapshot
}

var (
	vadPlotBackground = color.NRGBA{R: 15, G: 23, B: 38, A: 255}
	vadPlotGrid       = color.NRGBA{R: 37, G: 51, B: 69, A: 255}
	vadPlotQuiet      = color.NRGBA{R: 125, G: 150, B: 177, A: 255}
	vadPlotVoice      = color.NRGBA{R: 52, G: 211, B: 153, A: 255}
	vadPlotHighlight  = color.NRGBA{R: 19, G: 57, B: 53, A: 255}
)

type vadPlot struct {
	mu     sync.Mutex
	frames []vadPlotFrame
}

func (p *vadPlot) setFrames(frames []vadPlotFrame) {
	p.mu.Lock()
	p.frames = frames
	p.mu.Unlock()
}

func (p *vadPlot) render(width, height int) image.Image {
	p.mu.Lock()
	frames := p.frames
	p.mu.Unlock()
	return renderVADPlot(frames, width, height)
}

func renderVADPlot(frames []vadPlotFrame, width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, max(1, width), max(1, height)))
	fill := func(rect image.Rectangle, c color.NRGBA) {
		draw.Draw(img, rect, image.NewUniform(c), image.Point{}, draw.Src)
	}
	fill(img.Bounds(), vadPlotBackground)
	if width < 1 || height < 1 {
		return img
	}
	center, amplitude := height*35/100, float64(height)*0.28
	voiceY, quietY := height*78/100, height*92/100
	for i, frame := range frames {
		slot := vadPlotFrames - len(frames) + i
		x0, x1 := slot*width/vadPlotFrames, (slot+1)*width/vadPlotFrames
		if frame.speech {
			fill(image.Rect(x0, 0, max(x0+1, x1), height), vadPlotHighlight)
		}
	}
	for second := 0; second <= 8; second++ {
		x := min(width-1, second*width/8)
		fill(image.Rect(x, 0, x+1, height), vadPlotGrid)
	}
	for _, y := range []int{center, height * 70 / 100, voiceY, quietY} {
		fill(image.Rect(0, y, width, y+1), vadPlotGrid)
	}
	previousY := quietY
	for i, frame := range frames {
		slot := vadPlotFrames - len(frames) + i
		x0, x1 := slot*width/vadPlotFrames, (slot+1)*width/vadPlotFrames
		x1 = max(x0+1, x1)
		ink, y := vadPlotQuiet, quietY
		if frame.speech {
			ink, y = vadPlotVoice, voiceY
		}
		// Fixed display gain makes quiet microphones visible without changing VAD input.
		top := center - int(min(1, frame.high*4)*amplitude)
		bottom := center - int(max(-1, frame.low*4)*amplitude)
		fill(image.Rect(x0, top, x1, max(top+1, bottom+1)), ink)
		fill(image.Rect(x0, y, x1, y+2), ink)
		if i > 0 && previousY != y {
			fill(image.Rect(x0, min(y, previousY), x0+1, max(y, previousY)+2), ink)
		}
		previousY = y
	}
	return img
}
