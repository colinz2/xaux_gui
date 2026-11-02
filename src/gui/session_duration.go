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
	label     *widget.Label
	startedAt time.Time
	active    atomic.Bool
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func newSessionDurationView(label *widget.Label, startedAt time.Time) *sessionDurationView {
	view := &sessionDurationView{
		label:     label,
		startedAt: startedAt,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	view.active.Store(true)
	label.SetText("时长：" + formatSessionDuration(0))
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
			text := "时长：" + formatSessionDuration(now.Sub(v.startedAt))
			fyne.Do(func() {
				if v.active.Load() {
					v.label.SetText(text)
				}
			})
		case <-v.stop:
			return
		}
	}
}

func (v *sessionDurationView) Stop() {
	if v == nil {
		return
	}
	v.active.Store(false)
	v.stopOnce.Do(func() {
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
