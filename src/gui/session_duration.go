package gui

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type sessionDurationView struct {
	label            *widget.Label
	segmentStartedAt time.Time
	elapsed          time.Duration
	paused           bool
	mu               sync.Mutex
	active           atomic.Bool
	stop             chan struct{}
	done             chan struct{}
	stopOnce         sync.Once
	locale           *localeBindings
}

func newSessionDurationView(label *widget.Label, startedAt time.Time) *sessionDurationView {
	view := &sessionDurationView{
		label:            label,
		segmentStartedAt: startedAt,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		locale:           &localeBindings{},
	}
	view.active.Store(true)
	view.locale.setLabel(label, "common.duration", map[string]any{"Duration": formatSessionDuration(time.Since(startedAt))})
	view.locale.start()
	go view.run()
	return view
}

func (v *sessionDurationView) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer close(v.done)
	for {
		select {
		case now := <-ticker.C:
			duration := formatSessionDuration(v.elapsedAt(now))
			fyne.Do(func() {
				if v.active.Load() {
					v.locale.setLabel(v.label, "common.duration", map[string]any{"Duration": duration})
				}
			})
		case <-v.stop:
			return
		}
	}
}

func (v *sessionDurationView) Pause() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.paused {
		return
	}
	v.elapsed += time.Since(v.segmentStartedAt)
	v.paused = true
}

func (v *sessionDurationView) Resume() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.paused {
		return
	}
	v.segmentStartedAt = time.Now()
	v.paused = false
}

func (v *sessionDurationView) elapsedAt(now time.Time) time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	elapsed := v.elapsed
	if !v.paused {
		elapsed += now.Sub(v.segmentStartedAt)
	}
	return elapsed
}

func (v *sessionDurationView) Stop() {
	if v == nil {
		return
	}
	v.stopOnce.Do(func() {
		v.active.Store(false)
		v.locale.stop()
		close(v.stop)
	})
	<-v.done
}

func formatSessionDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	seconds := int64(duration / time.Second)
	return fmt.Sprintf(
		"%02d:%02d:%02d",
		seconds/3600,
		(seconds/60)%60,
		seconds%60,
	)
}
