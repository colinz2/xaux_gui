package gui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"xaux_gui/pkg/sound_cap"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

// The subtitle strip is a slim dark bar docked near the bottom of the screen.
// It listens to the speaker only and behaves like live captions: text wraps
// inside the bar and older lines scroll up as new words arrive, so nothing
// ever shifts sideways or gets cut off at the edges. Nothing is recorded or
// saved — closing the strip discards everything.
const (
	subtitleWindowWidth  = 1080
	subtitleVisibleLines = 2
	subtitleHistoryRunes = 200 // recent final text kept to backfill the top line

	subtitleDefaultTextSize = 26
	subtitleMinTextSize     = 18
	subtitleMaxTextSize     = 38
	subtitleTextSizeStep    = 4
	subtitleTextSizeKey     = "subtitle_text_size"

	// Fallback wrap width used before the first layout pass sizes the lines.
	subtitleFallbackWidth = subtitleWindowWidth - 200
)

var subtitleTextStyle = fyne.TextStyle{Bold: true}

var (
	subtitleBackgroundColor = color.NRGBA{R: 12, G: 12, B: 16, A: 255}
	subtitleTextColor       = color.NRGBA{R: 240, G: 240, B: 245, A: 255}
	subtitlePendingColor    = color.NRGBA{R: 128, G: 128, B: 138, A: 255}
	subtitleErrorColor      = color.NRGBA{R: 235, G: 120, B: 110, A: 255}
)

var activeSubtitle struct {
	sync.Mutex
	window *subtitleWindow
}

type subtitleWindow struct {
	window   fyne.Window
	lines    []*canvas.Text
	textSize float32

	history     string // tail of recently finalized sentences
	currentText string // interim text of the sentence being recognized
	errorText   string

	running      atomic.Bool
	onClosed     func()
	releaseOnce  sync.Once
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	locale       *localeBindings
}

func newSubtitleWindow() *subtitleWindow {
	if !acquireVoiceTask(subtitleMenuName) {
		return nil
	}
	size := fyne.CurrentApp().Preferences().FloatWithFallback(subtitleTextSizeKey, subtitleDefaultTextSize)
	return &subtitleWindow{
		textSize:     clampSubtitleTextSize(float32(size)),
		shutdownDone: make(chan struct{}),
		locale:       &localeBindings{},
	}
}

func clampSubtitleTextSize(size float32) float32 {
	if size < subtitleMinTextSize {
		return subtitleMinTextSize
	}
	if size > subtitleMaxTextSize {
		return subtitleMaxTextSize
	}
	return size
}

func runSubtitle() {
	activeSubtitle.Lock()
	window := activeSubtitle.window
	activeSubtitle.Unlock()
	if window != nil {
		window.window.Show()
		window.window.RequestFocus()
		return
	}

	setting, err := currentAppSetting()
	if err != nil {
		showLocalizedError(err, currentMainWin.win)
		return
	}
	if setting.proxyAddr == "" {
		showLocalizedInformation(tr("menu.subtitle"), tr("speech.proxy_missing"), currentMainWin.win)
		return
	}
	if !setting.speakerSelected {
		showLocalizedInformation(tr("menu.subtitle"), tr("subtitle.speaker_required"), currentMainWin.win)
		return
	}

	window = newSubtitleWindow()
	if window == nil {
		showLocalizedInformation(
			tr("menu.subtitle"),
			trData("subtitle.active_task", map[string]any{"Task": voiceTaskDisplayName(activeVoiceTask())}),
			currentMainWin.win,
		)
		return
	}

	// Same ASR pipeline as the meeting window, but speaker only and with an
	// empty record path so no WAV file or session is created.
	capture, err := newASRSoundCap(
		context.Background(), setting.proxyAddr, setting.speakerName, "", false,
		window.setCaptureError, nil,
	)
	if err != nil {
		window.release()
		if errors.Is(err, sound_cap.ErrASRProxyUnavailable) {
			log.Printf("ASR proxy unavailable: %v", err)
			showLocalizedInformation(
				tr("menu.subtitle"),
				trData("speech.proxy_unavailable", map[string]any{"Address": setting.proxyAddr}),
				currentMainWin.win,
			)
			return
		}
		showLocalizedError(err, currentMainWin.win)
		return
	}
	capture.target = window
	go func() {
		if err := capture.Run(); err != nil {
			window.setCaptureError(err)
		}
	}()
	if err := <-capture.soundCap.StartDone(); err != nil {
		capture.Stop()
		window.release()
		showLocalizedError(fmt.Errorf("start %s: %w", capture.deviceName, err), currentMainWin.win)
		return
	}

	window.start(func() { capture.Stop() })
}

func (s *subtitleWindow) start(onClosed func()) {
	s.onClosed = onClosed
	s.buildUI()
	s.running.Store(true)
	activeSubtitle.Lock()
	activeSubtitle.window = s
	activeSubtitle.Unlock()
	s.window.Show()
	moveSubtitleToBottom(s.window)
	s.locale.start()
}

func (s *subtitleWindow) buildUI() {
	s.lines = make([]*canvas.Text, subtitleVisibleLines)
	lineObjects := make([]fyne.CanvasObject, subtitleVisibleLines)
	for i := range s.lines {
		line := canvas.NewText("", subtitleTextColor)
		line.TextStyle = subtitleTextStyle
		line.Alignment = fyne.TextAlignLeading
		s.lines[i] = line
		lineObjects[i] = line
	}

	zoomOut := widget.NewButtonWithIcon("", theme.ZoomOutIcon(), func() { s.adjustTextSize(-subtitleTextSizeStep) })
	zoomIn := widget.NewButtonWithIcon("", theme.ZoomInIcon(), func() { s.adjustTextSize(subtitleTextSizeStep) })
	closeButton := widget.NewButtonWithIcon("", theme.WindowCloseIcon(), s.shutdown)
	for _, button := range []*widget.Button{zoomOut, zoomIn, closeButton} {
		button.Importance = widget.LowImportance
	}
	controls := container.NewCenter(container.NewHBox(zoomOut, zoomIn, closeButton))

	background := canvas.NewRectangle(subtitleBackgroundColor)
	dragArea := newSubtitleDragArea(func(dx, dy float32) { moveSubtitleBy(s.window, dx, dy) })
	textArea := container.NewPadded(container.NewVBox(lineObjects...))
	content := container.NewStack(
		background,
		dragArea,
		container.NewBorder(nil, nil, nil, controls, textArea),
	)

	s.window = newSubtitleShell()
	s.window.SetTitle(tr("menu.subtitle"))
	s.locale.add(func() {
		s.window.SetTitle(tr("menu.subtitle"))
		s.renderLines()
	})
	s.window.SetContent(content)
	s.resizeForText()
	s.window.SetCloseIntercept(s.shutdown)
	s.window.SetOnClosed(s.release)
	s.renderLines()
}

// newSubtitleShell prefers a borderless splash window where the platform can
// dock it to the bottom of the screen; elsewhere a normal window keeps the
// strip movable by the user.
func newSubtitleShell() fyne.Window {
	if driver, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok && subtitleUsesSplashShell {
		if splash := driver.CreateSplashWindow(); splash != nil {
			return splash
		}
	}
	return fyne.CurrentApp().NewWindow(tr("menu.subtitle"))
}

func (s *subtitleWindow) adjustTextSize(delta float32) {
	size := clampSubtitleTextSize(s.textSize + delta)
	if size == s.textSize {
		return
	}
	s.textSize = size
	fyne.CurrentApp().Preferences().SetFloat(subtitleTextSizeKey, float64(size))
	s.resizeForText()
	s.renderLines()
}

func (s *subtitleWindow) resizeForText() {
	lineHeight := fyne.MeasureText("字幕", s.textSize, subtitleTextStyle).Height
	s.window.Resize(fyne.NewSize(subtitleWindowWidth, lineHeight*subtitleVisibleLines+40))
}

func (s *subtitleWindow) updateASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if capture == nil || response == nil {
		return
	}
	if response.Type == x.TypeSentenceStart {
		capture.acceptASRResult(response)
		return
	}
	if response.Type != x.TypeRecognizing && response.Type != x.TypeSentenceEnd {
		return
	}
	if accepted, _ := capture.acceptASRResult(response); !accepted {
		return
	}
	fyne.Do(func() {
		s.applyASRResult(response)
	})
}

func (s *subtitleWindow) applyASRResult(response *x.AllResponse) {
	if !s.running.Load() {
		return
	}
	text := response.Result.Result
	if text == "" {
		return
	}
	s.errorText = ""
	if asrResponseIsFinal(response) {
		// Finished sentences join the history tail that backfills the display.
		s.history = tailRunes(strings.TrimSpace(s.history+" "+text), subtitleHistoryRunes)
		s.currentText = ""
	} else {
		s.currentText = text
	}
	s.renderLines()
}

func (s *subtitleWindow) setCaptureError(err error) {
	if err == nil {
		return
	}
	log.Printf("subtitle capture: %v", err)
	fyne.Do(func() {
		if !s.running.Load() {
			return
		}
		s.errorText = trData("speech.capture_error", map[string]any{"Error": err})
		s.renderLines()
	})
}

func (s *subtitleWindow) displayText() string {
	return strings.TrimSpace(s.history + " " + s.currentText)
}

func (s *subtitleWindow) renderLines() {
	if len(s.lines) == 0 {
		return
	}
	text, textColor := s.displayText(), subtitleTextColor
	switch {
	case s.errorText != "":
		text, textColor = s.errorText, subtitleErrorColor
	case text == "":
		text, textColor = tr("speech.waiting"), subtitlePendingColor
	}
	wrapped := wrapSubtitleText(text, s.textSize, subtitleTextStyle, s.lineWidth())
	if len(wrapped) > len(s.lines) {
		// Older lines scroll up and away, keeping the newest words visible.
		wrapped = wrapped[len(wrapped)-len(s.lines):]
	}
	for i, line := range s.lines {
		line.Text = ""
		if i < len(wrapped) {
			line.Text = wrapped[i]
		}
		line.TextSize = s.textSize
		line.Color = textColor
		line.Refresh()
	}
}

func (s *subtitleWindow) lineWidth() float32 {
	if width := s.lines[0].Size().Width; width > 0 {
		return width
	}
	return subtitleFallbackWidth
}

// tailRunes keeps at most limit trailing runes of text.
func tailRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}

// wrapSubtitleText wraps text into caption lines that fit maxWidth, breaking
// by rune so CJK and Latin text both stay inside the strip.
func wrapSubtitleText(text string, size float32, style fyne.TextStyle, maxWidth float32) []string {
	if text == "" || maxWidth <= 0 {
		return nil
	}
	var lines []string
	for remaining := []rune(text); len(remaining) > 0; {
		count := fitRuneCount(remaining, size, style, maxWidth)
		lines = append(lines, string(remaining[:count]))
		remaining = remaining[count:]
	}
	return lines
}

// fitRuneCount returns how many leading runes fit within maxWidth, always
// consuming at least one rune so wrapping progresses.
func fitRuneCount(runes []rune, size float32, style fyne.TextStyle, maxWidth float32) int {
	if fyne.MeasureText(string(runes), size, style).Width <= maxWidth {
		return len(runes)
	}
	low, high := 1, len(runes)-1
	for low < high {
		mid := (low + high + 1) / 2
		if fyne.MeasureText(string(runes[:mid]), size, style).Width <= maxWidth {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

// subtitleDragArea lets the user drag the borderless strip from anywhere on
// its surface; the buttons above it keep handling their own events.
type subtitleDragArea struct {
	widget.BaseWidget
	onDrag func(dx, dy float32)
}

func newSubtitleDragArea(onDrag func(dx, dy float32)) *subtitleDragArea {
	area := &subtitleDragArea{onDrag: onDrag}
	area.ExtendBaseWidget(area)
	return area
}

func (a *subtitleDragArea) Dragged(event *fyne.DragEvent) {
	a.onDrag(event.Dragged.DX, event.Dragged.DY)
}

func (a *subtitleDragArea) DragEnd() {}

func (a *subtitleDragArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (s *subtitleWindow) shutdown() {
	s.shutdownOnce.Do(func() {
		s.running.Store(false)
		go func() {
			if s.onClosed != nil {
				s.onClosed()
			}
			fyne.DoAndWait(func() {
				if s.window != nil {
					s.window.SetCloseIntercept(nil)
					s.window.Close()
				}
				s.release()
			})
			close(s.shutdownDone)
		}()
	})
}

func (s *subtitleWindow) release() {
	s.releaseOnce.Do(func() {
		s.locale.stop()
		activeSubtitle.Lock()
		if activeSubtitle.window == s {
			activeSubtitle.window = nil
		}
		activeSubtitle.Unlock()
		releaseVoiceTask(subtitleMenuName)
	})
}

// stopActiveSubtitle is called during application exit, before ffaudio.UnInit.
func stopActiveSubtitle() <-chan struct{} {
	activeSubtitle.Lock()
	window := activeSubtitle.window
	activeSubtitle.Unlock()
	if window != nil {
		window.shutdown()
		return window.shutdownDone
	}
	return nil
}
