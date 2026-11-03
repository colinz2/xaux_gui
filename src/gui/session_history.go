package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const wavHeaderSize = 44

type sessionHistoryWin struct {
	window          fyne.Window
	setting         *appSetting
	sessions        []SessionInfo
	list            *widget.List
	searchEntry     *widget.Entry
	hint            *widget.Label
	name            *widget.Label
	metadata        *widget.Label
	directory       *widget.Label
	detailError     *widget.Label
	recordings      *fyne.Container
	recordingOpener func(string) error
	transcript      *widget.Entry
	jobDescription  *widget.Label
	review          *widget.Label
	reviewScroll    *container.Scroll
	reviewEmpty     *widget.Label
	reviewHeader    *widget.Label
	historyTabs     *container.AppTabs
	transcriptTab   *container.TabItem
	reviewTab       *container.TabItem
	jobTab          *container.TabItem
	details         *fyne.Container
	deleteButton    *widget.Button
	reviewButton    *widget.Button
	reviewActions   *fyne.Container
	reviewStatus    *widget.Label
	selected        *SessionInfo
	reviewCancel    context.CancelFunc
	reviewing       bool
	reviewRunID     atomic.Uint64
	locale          *localeBindings
	detailsLocale   *localeBindings
}

var sessionHistoryWindows struct {
	sync.Mutex
	active *sessionHistoryWin
}

func runSessionHistory() {
	sessionHistoryWindows.Lock()
	if active := sessionHistoryWindows.active; active != nil {
		sessionHistoryWindows.Unlock()
		active.window.Show()
		active.window.RequestFocus()
		return
	}
	sessionHistoryWindows.Unlock()

	setting, err := currentAppSetting()
	if err != nil {
		showLocalizedError(err, currentMainWin.win)
		return
	}
	history := &sessionHistoryWin{setting: setting, locale: &localeBindings{}}
	history.window = fyne.CurrentApp().NewWindow(tr("menu.session_history"))
	history.locale.add(func() { history.window.SetTitle(tr("menu.session_history")) })
	history.buildContent()
	history.window.Resize(fyne.NewSize(1100, 700))
	history.window.CenterOnScreen()
	history.window.SetOnClosed(func() {
		history.locale.stop()
		history.detailsLocale.stop()
		history.cancelInterviewReview()
		history.reviewRunID.Add(1)
		sessionHistoryWindows.Lock()
		if sessionHistoryWindows.active == history {
			sessionHistoryWindows.active = nil
		}
		sessionHistoryWindows.Unlock()
	})

	sessionHistoryWindows.Lock()
	if active := sessionHistoryWindows.active; active != nil {
		sessionHistoryWindows.Unlock()
		history.window.Close()
		active.window.Show()
		active.window.RequestFocus()
		return
	}
	sessionHistoryWindows.active = history
	sessionHistoryWindows.Unlock()

	history.searchSessions()
	history.window.Show()
	history.locale.start()
}

func refreshActiveSessionHistory() {
	fyne.Do(func() {
		sessionHistoryWindows.Lock()
		history := sessionHistoryWindows.active
		sessionHistoryWindows.Unlock()
		if history != nil && !history.reviewing {
			history.searchSessions()
		}
	})
}

func closeActiveSessionHistory() {
	sessionHistoryWindows.Lock()
	history := sessionHistoryWindows.active
	sessionHistoryWindows.Unlock()
	if history == nil {
		return
	}
	history.cancelInterviewReview()
	history.reviewRunID.Add(1)
	history.window.SetCloseIntercept(nil)
	history.window.Close()
}

func (w *sessionHistoryWin) buildContent() {
	w.hint = widget.NewLabel("")
	w.hint.Wrapping = fyne.TextWrapBreak
	w.list = widget.NewList(
		func() int { return len(w.sessions) },
		func() fyne.CanvasObject { return sessionHistoryListItem(SessionInfo{}) },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			updateSessionHistoryListItem(object.(*fyne.Container), w.sessions[id])
		},
	)
	w.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(w.sessions) {
			w.showSession(w.sessions[id])
		}
	}

	w.searchEntry = widget.NewEntry()
	w.locale.add(func() { w.searchEntry.SetPlaceHolder(tr("history.search_placeholder")) })
	w.searchEntry.OnChanged = func(string) {
		w.searchSessions()
	}
	w.searchEntry.OnSubmitted = func(string) {
		w.searchSessions()
	}
	searchButton := widget.NewButtonWithIcon("", theme.SearchIcon(), w.searchSessions)
	w.locale.setButton(searchButton, "history.search", nil)
	left := container.NewBorder(
		container.NewBorder(nil, nil, nil, searchButton, w.searchEntry),
		w.hint,
		nil, nil,
		w.list,
	)

	w.name = widget.NewLabel("")
	w.locale.setLabel(w.name, "history.select", nil)
	w.name.TextStyle.Bold = true
	w.name.Wrapping = fyne.TextWrapBreak
	w.deleteButton = widget.NewButtonWithIcon("", theme.DeleteIcon(), w.confirmDeleteSession)
	w.locale.setButton(w.deleteButton, "history.delete", nil)
	w.deleteButton.Disable()
	w.reviewButton = widget.NewButton("", w.generateSessionAnalysis)
	w.reviewButton.Disable()
	w.reviewButton.Hide()
	w.reviewStatus = widget.NewLabel("")
	w.reviewStatus.Wrapping = fyne.TextWrapBreak
	w.reviewStatus.Hide()
	w.reviewActions = container.NewHBox(w.reviewButton, w.deleteButton)
	titleRow := container.NewBorder(nil, nil, nil, w.reviewActions, w.name)
	w.metadata = widget.NewLabel("")
	w.metadata.Wrapping = fyne.TextWrapBreak
	w.directory = widget.NewLabel("")
	w.directory.Wrapping = fyne.TextWrapBreak
	w.directory.Selectable = true
	w.detailError = widget.NewLabel("")
	w.detailError.Wrapping = fyne.TextWrapBreak
	w.recordings = container.NewVBox()
	w.transcript = widget.NewMultiLineEntry()
	w.transcript.Wrapping = fyne.TextWrapBreak
	w.transcript.SetMinRowsVisible(12)
	w.locale.add(func() { w.transcript.SetPlaceHolder(tr("history.transcript_empty")) })
	w.transcript.Disable()
	w.jobDescription = widget.NewLabel("")
	w.jobDescription.Wrapping = fyne.TextWrapBreak
	w.jobDescription.Selectable = true
	w.review = widget.NewLabel("")
	w.review.Wrapping = fyne.TextWrapBreak
	w.review.Selectable = true
	w.reviewScroll = container.NewVScroll(w.review)
	w.reviewEmpty = widget.NewLabel("")
	w.reviewEmpty.Wrapping = fyne.TextWrapBreak
	w.reviewEmpty.Importance = widget.LowImportance
	reviewContent := container.NewStack(w.reviewScroll, w.reviewEmpty)
	recordingsHeader := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	transcriptHeader := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.locale.setLabel(recordingsHeader, "history.recordings", nil)
	w.locale.setLabel(transcriptHeader, "history.transcript_results", nil)
	w.reviewHeader = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.transcriptTab = container.NewTabItem("", container.NewBorder(transcriptHeader, nil, nil, nil, w.transcript))
	w.reviewTab = container.NewTabItem("", container.NewBorder(w.reviewHeader, nil, nil, nil, reviewContent))
	w.jobTab = container.NewTabItem("", container.NewVScroll(w.jobDescription))
	w.historyTabs = container.NewAppTabs(w.transcriptTab, w.reviewTab)
	w.locale.add(func() {
		w.refreshTabLabels()
		w.list.Refresh()
	})
	w.historyTabs.Hide()
	detailsHeader := container.NewVBox(
		titleRow,
		w.reviewStatus,
		w.metadata,
		w.directory,
		w.detailError,
		widget.NewSeparator(),
		recordingsHeader,
		w.recordings,
		widget.NewSeparator(),
	)

	w.details = container.NewBorder(detailsHeader, nil, nil, nil, w.historyTabs)
	split := container.NewHSplit(left, w.details)
	split.Offset = 0.42
	if w.window != nil {
		w.window.SetContent(split)
	}
}

func sessionHistoryListItem(info SessionInfo) *fyne.Container {
	name := widget.NewLabel("")
	name.Wrapping = fyne.TextWrapOff
	name.Truncation = fyne.TextTruncateEllipsis
	date := widget.NewLabel("")
	date.Alignment = fyne.TextAlignTrailing
	status := widget.NewLabel("")
	status.Alignment = fyne.TextAlignTrailing
	metadata := container.NewHBox(date, status)
	row := container.NewBorder(nil, nil, nil, metadata, name)
	updateSessionHistoryListItem(row, info)
	return row
}

func updateSessionHistoryListItem(row *fyne.Container, info SessionInfo) {
	name := row.Objects[0].(*widget.Label)
	metadata := row.Objects[1].(*fyne.Container)
	date := metadata.Objects[0].(*widget.Label)
	status := metadata.Objects[1].(*widget.Label)

	name.SetText(info.Name)
	date.SetText(info.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	status.SetText(sessionStatusText(info.Status))
}

func (w *sessionHistoryWin) searchSessions() {
	sessions, err := LoadSessions(w.setting.sessionRootDir)
	w.list.UnselectAll()
	w.clearDetails()
	if err != nil {
		w.sessions = nil
		w.locale.setLabel(w.hint, "history.load_failed", map[string]any{"Error": err})
		w.list.Refresh()
		return
	}

	query := strings.TrimSpace(w.searchEntry.Text)
	var firstReadErr error
	failedReads := 0
	if query != "" {
		matches := make([]SessionInfo, 0, len(sessions))
		needle := strings.ToLower(query)
		for _, info := range sessions {
			if strings.Contains(strings.ToLower(info.Name), needle) {
				matches = append(matches, info)
				continue
			}
			if info.TranscriptFile == "" {
				continue
			}
			path := filepath.Join(info.Directory, filepath.Base(info.TranscriptFile))
			data, err := os.ReadFile(path)
			if err != nil {
				readErr := localizedMessageError("history.read_session_transcript_failed",
					map[string]any{"Name": fmt.Sprintf("%q", info.Name)},
					fmt.Sprintf("read transcript for session %q failed", info.Name), err)
				log.Printf("read transcript for session %q: %v", info.Name, err)
				failedReads++
				if firstReadErr == nil {
					firstReadErr = readErr
				}
				continue
			}
			if strings.Contains(strings.ToLower(string(data)), needle) {
				matches = append(matches, info)
			}
		}
		sessions = matches
	}
	w.sessions = sessions

	var summaryKey string
	var summaryData map[string]any
	switch {
	case query == "" && len(sessions) == 0:
		summaryKey = "history.no_sessions"
	case query == "":
		summaryKey = "history.count"
		summaryData = map[string]any{"Count": len(sessions), "Path": w.setting.sessionRootDir}
	case len(sessions) == 0:
		summaryKey = "history.no_matches"
		summaryData = map[string]any{"Query": query}
	default:
		summaryKey = "history.matches"
		summaryData = map[string]any{"Count": len(sessions), "Query": query}
	}
	if firstReadErr != nil {
		w.locale.setLabel(w.hint, "history.read_failures", func() any {
			return map[string]any{"Summary": trData(summaryKey, summaryData), "Count": failedReads, "Error": firstReadErr.Error()}
		})
	} else {
		w.locale.setLabel(w.hint, summaryKey, summaryData)
	}
	w.list.Refresh()
	if len(w.sessions) > 0 {
		w.list.Select(0)
	}
}

func (w *sessionHistoryWin) clearDetails() {
	w.cancelInterviewReview()
	w.reviewRunID.Add(1)
	w.selected = nil
	w.detailsLocale.stop()
	w.detailsLocale = nil
	w.locale.setLabel(w.name, "history.select", nil)
	w.deleteButton.Disable()
	w.reviewButton.Hide()
	w.reviewButton.Disable()
	w.setReviewStatus("")
	w.metadata.SetText("")
	w.directory.SetText("")
	w.detailError.SetText("")
	w.recordings.Objects = nil
	w.recordings.Refresh()
	w.transcript.SetText("")
	w.jobDescription.SetText("")
	w.setReviewText("")
	w.historyTabs.Hide()
	w.details.Refresh()
}

func (w *sessionHistoryWin) showSession(info SessionInfo) {
	w.cancelInterviewReview()
	w.reviewRunID.Add(1)
	w.detailsLocale.stop()
	w.detailsLocale = nil
	if w.locale != nil {
		w.detailsLocale = &localeBindings{}
		defer w.detailsLocale.start()
	}
	defer w.details.Refresh()

	selected := info
	w.selected = &selected
	w.configureHistoryTabs(info)
	w.deleteButton.Enable()
	w.locale.forgetLabel(w.name)
	w.name.SetText(info.Name)
	endedAt := info.UpdatedAt
	if info.Status == SessionStatusRunning {
		endedAt = time.Now()
	}
	duration := formatSessionDuration(endedAt.Sub(info.CreatedAt))
	w.detailsLocale.setLabel(w.metadata, "history.metadata", func() any {
		return map[string]any{
			"Created":  info.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			"Duration": duration, "Kind": sessionKindText(info.Kind), "Status": sessionStatusText(info.Status),
		}
	})
	w.detailsLocale.setLabel(w.directory, "history.directory", map[string]any{"Path": info.Directory})
	var transcriptErr, jobErr error

	var rows []fyne.CanvasObject
	if info.MixedRecording != "" {
		fileName := filepath.Base(info.MixedRecording)
		path := filepath.Join(info.Directory, fileName)
		label := widget.NewLabel(fileName)
		label.Wrapping = fyne.TextWrapBreak
		play := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
			if err := w.playRecording(path); err != nil {
				showLocalizedError(err, w.window)
			}
		})
		w.detailsLocale.setButton(play, "history.play", nil)
		if fileInfo, err := os.Stat(path); err == nil && fileInfo.Size() <= wavHeaderSize {
			w.detailsLocale.setLabel(label, "history.no_audio", map[string]any{"Name": fileName})
			play.Disable()
		}
		rows = append(rows, container.NewBorder(nil, nil, label, play))
	}
	if len(rows) == 0 {
		empty := widget.NewLabel("")
		w.detailsLocale.setLabel(empty, "history.no_mixed_recording", nil)
		rows = append(rows, empty)
	}
	w.recordings.Objects = rows
	w.recordings.Refresh()

	w.transcript.SetText("")
	w.jobDescription.SetText("")
	if info.TranscriptFile != "" {
		data, err := os.ReadFile(filepath.Join(info.Directory, filepath.Base(info.TranscriptFile)))
		if err != nil {
			transcriptErr = err
		} else {
			w.transcript.SetText(string(data))
		}
	}
	if info.Kind == SessionKindInterview && info.JobDescriptionFile != "" {
		job, err := readInterviewReference(filepath.Join(info.Directory, filepath.Base(info.JobDescriptionFile)), &localizedText{key: "reference.job"})
		if err != nil {
			jobErr = err
		} else {
			w.jobDescription.SetText(job)
		}
	}
	w.detailsLocale.add(func() {
		var messages []string
		if info.Error != "" {
			messages = append(messages, trData("history.task_error", map[string]any{"Error": info.Error}))
		}
		if transcriptErr != nil {
			messages = append(messages, trData("history.read_transcript_failed", map[string]any{"Error": transcriptErr}))
		}
		if jobErr != nil {
			messages = append(messages, jobErr.Error())
		}
		w.detailError.SetText(strings.Join(messages, "\n"))
	})
	w.showSessionAnalysis(info)
}

func openSessionRecording(path string, openFile func(string) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return localizedMessageError("history.recording_access_error", nil, "cannot access recording", err)
	}
	if !info.Mode().IsRegular() {
		return localizedMessageError("history.recording_not_file", nil, "recording path is not a regular file", nil)
	}
	if info.Size() <= wavHeaderSize {
		return localizedMessageError("history.recording_empty", nil, "recording contains no audio data", nil)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return localizedMessageError("history.recording_path_error", nil, "cannot resolve recording path", err)
	}
	if err := openFile(absolutePath); err != nil {
		return localizedMessageError("history.recording_open_error", nil, "cannot open recording in default system player", err)
	}
	return nil
}

func (w *sessionHistoryWin) playRecording(path string) error {
	opener := w.recordingOpener
	if opener == nil {
		opener = openRecordingFile
	}
	return openSessionRecording(path, opener)
}

func (w *sessionHistoryWin) configureHistoryTabs(info SessionInfo) {
	kind := info.Kind
	if kind != SessionKindInterview && kind != SessionKindTranscription {
		w.historyTabs.Hide()
		return
	}
	w.refreshTabLabels()
	items := []*container.TabItem{w.transcriptTab, w.reviewTab}
	if kind == SessionKindInterview && info.JobDescriptionFile != "" {
		items = append(items, w.jobTab)
	}
	w.historyTabs.SetItems(items)
	w.historyTabs.Select(w.transcriptTab)
	w.historyTabs.Show()
}

func (w *sessionHistoryWin) refreshTabLabels() {
	if w.selected != nil && w.selected.Kind == SessionKindTranscription {
		w.transcriptTab.Text = tr("menu.transcription")
		w.reviewTab.Text = tr("history.ai_summary")
		w.reviewHeader.SetText(tr("history.ai_summary"))
		w.reviewEmpty.SetText(tr("history.summary_empty"))
	} else {
		w.transcriptTab.Text = tr("history.interview_transcript")
		w.reviewTab.Text = tr("history.ai_review")
		w.reviewHeader.SetText(tr("history.ai_interview_review"))
		w.reviewEmpty.SetText(tr("history.review_empty"))
	}
	w.jobTab.Text = tr("history.job")
	w.historyTabs.Refresh()
}

func analysisLabel(kind SessionKind) string {
	if kind == SessionKindTranscription {
		return tr("history.summary")
	}
	return tr("history.review")
}

func (w *sessionHistoryWin) showSessionAnalysis(info SessionInfo) {
	w.setReviewText("")
	w.setReviewStatus("")
	w.reviewButton.Hide()
	w.reviewButton.Disable()
	if info.Kind != SessionKindInterview && info.Kind != SessionKindTranscription {
		w.logInterviewReviewActionState(info, false, "unsupported_kind")
		return
	}
	file := info.ReviewFile
	if info.Kind == SessionKindTranscription {
		file = info.SummaryFile
	}
	w.reviewButton.Show()
	w.locale.setButton(w.reviewButton, "history.generate", analysisData(info.Kind, nil))
	if info.Status == SessionStatusRunning {
		w.setLocalizedReviewStatus("history.after_session", analysisData(info.Kind, nil))
		w.logInterviewReviewActionState(info, false, "session_running")
		return
	}
	if info.TranscriptFile == "" {
		w.setLocalizedReviewStatus("history.no_transcript", analysisData(info.Kind, nil))
		w.logInterviewReviewActionState(info, false, "transcript_missing")
		return
	}

	w.reviewButton.Enable()
	if file == "" {
		w.setLocalizedReviewStatus("history.not_generated", analysisData(info.Kind, nil))
		w.logInterviewReviewActionState(info, true, "review_not_generated")
		return
	}
	data, err := os.ReadFile(filepath.Join(info.Directory, filepath.Base(file)))
	if err != nil {
		w.setLocalizedReviewStatus("history.read_analysis_failed", analysisData(info.Kind, err))
		w.logInterviewReviewActionState(info, true, "saved_review_read_failed")
		return
	}
	w.setReviewText(string(data))
	w.locale.setButton(w.reviewButton, "history.regenerate", analysisData(info.Kind, nil))
	w.setLocalizedReviewStatus("history.saved", analysisData(info.Kind, nil))
	w.logInterviewReviewActionState(info, true, "saved_review_loaded")
}

func (w *sessionHistoryWin) logInterviewReviewActionState(info SessionInfo, enabled bool, reason string) {
	if w.setting == nil || strings.TrimSpace(w.setting.modelConfigDir) == "" {
		return
	}
	logger, file, err := openInterviewDebugLogger(w.setting.modelConfigDir)
	if err != nil {
		log.Printf("log interview review action state: %s", diagnosticError(err))
		return
	}
	logger.Printf(
		"review action state session=%q kind=%q status=%q transcript_available=%t review_available=%t enabled=%t reason=%q",
		filepath.Base(info.Directory), info.Kind, info.Status,
		info.TranscriptFile != "", info.ReviewFile != "" || info.SummaryFile != "", enabled, reason,
	)
	closeInterviewDebugLog(logger, file)
}

func (w *sessionHistoryWin) setReviewStatus(text string) {
	if text == "" {
		w.locale.clearLabel(w.reviewStatus)
		w.reviewStatus.Hide()
	} else {
		w.locale.forgetLabel(w.reviewStatus)
		w.reviewStatus.SetText(text)
		w.reviewStatus.Show()
	}
	if w.details != nil {
		w.details.Refresh()
	}
}

func (w *sessionHistoryWin) setLocalizedReviewStatus(key string, data any) {
	w.locale.setLabel(w.reviewStatus, key, data)
	w.reviewStatus.Show()
	if w.details != nil {
		w.details.Refresh()
	}
}

func analysisData(kind SessionKind, err error) func() any {
	return func() any {
		data := map[string]any{"Type": analysisLabel(kind)}
		if err != nil {
			data["Error"] = err.Error()
		}
		return data
	}
}

func (w *sessionHistoryWin) setReviewText(text string) {
	w.review.SetText(text)
	if strings.TrimSpace(text) == "" {
		w.reviewEmpty.Show()
	} else {
		w.reviewEmpty.Hide()
		w.reviewScroll.ScrollToBottom()
	}
}

func (w *sessionHistoryWin) generateSessionAnalysis() {
	if w.reviewing || w.selected == nil {
		return
	}
	info := *w.selected
	if (info.Kind != SessionKindInterview && info.Kind != SessionKindTranscription) ||
		info.Status == SessionStatusRunning || info.TranscriptFile == "" {
		return
	}
	if w.setting.modelConfigDir == "" {
		w.setting.modelConfigDir = fyne.CurrentApp().Storage().RootURI().Path()
	}
	logger, logFile, err := openInterviewDebugLogger(w.setting.modelConfigDir)
	if err != nil {
		w.setLocalizedReviewStatus("history.log_failed", map[string]any{"Error": err})
		return
	}
	keepLogOpen := true
	defer func() {
		if keepLogOpen {
			closeInterviewDebugLog(logger, logFile)
		}
	}()
	logger.Printf(
		"review requested session=%q status=%q transcript_file=%q",
		filepath.Base(info.Directory), info.Status, filepath.Base(info.TranscriptFile),
	)
	failPreflight := func(stage string, err error) {
		logger.Printf("review preflight failed stage=%s error=%s", stage, diagnosticError(err))
		w.setLocalizedReviewStatus("history.preflight_failed", analysisData(info.Kind, err))
	}
	transcriptPath := filepath.Join(info.Directory, filepath.Base(info.TranscriptFile))
	transcript, err := os.ReadFile(transcriptPath)
	if err != nil {
		failPreflight("read_transcript", err)
		return
	}
	if strings.TrimSpace(string(transcript)) == "" {
		failPreflight("empty_transcript", localizedMessageError("history.empty_transcript_error", nil, "transcript is empty", nil))
		return
	}
	transcriptChars := len([]rune(string(transcript)))
	logger.Printf("review transcript loaded chars=%d", transcriptChars)
	if err := w.setting.reloadModelConfig(); err != nil {
		failPreflight("load_model_config", err)
		return
	}
	config := w.setting.modelConfig
	if err := config.validate(); err != nil {
		failPreflight("validate_model_config", err)
		return
	}
	model, err := newChatCompletionModel(config)
	if err != nil {
		failPreflight("initialize_model", err)
		return
	}
	model.debugLog = logger
	messages := buildSessionAnalysisMessages(info.Kind, string(transcript))
	if info.Kind == SessionKindInterview && info.JobDescriptionFile != "" {
		job, err := readInterviewReference(filepath.Join(info.Directory, filepath.Base(info.JobDescriptionFile)), &localizedText{key: "reference.job"})
		if err != nil {
			failPreflight("read_job_description", err)
			return
		}
		messages = buildInterviewReviewMessagesWithJob(string(transcript), job)
	}
	logger.Printf("review generation started model=%q transcript_chars=%d", model.model, transcriptChars)
	ctx, cancel := context.WithTimeout(context.Background(), interviewReviewTimeout)
	runID := w.reviewRunID.Add(1)
	w.reviewCancel = cancel
	w.reviewing = true
	w.reviewButton.Disable()
	w.setLocalizedReviewStatus("history.analyzing", nil)
	w.setReviewText("")
	w.historyTabs.Select(w.historyTabs.Items[1])
	w.details.Refresh()
	logger.Printf("review request prepared messages=%d input_chars=%d", len(messages), len([]rune(messages[1].content)))
	keepLogOpen = false
	go func() {
		startedAt := time.Now()
		var review strings.Builder
		err := streamAndSaveSessionAnalysis(ctx, model, &info, messages, func() bool {
			return runID == w.reviewRunID.Load()
		}, func() {
			fyne.Do(func() {
				if runID == w.reviewRunID.Load() {
					w.setLocalizedReviewStatus("history.model_analyzing", nil)
				}
			})
		}, func(token string) {
			review.WriteString(token)
			content := review.String()
			fyne.Do(func() {
				if runID == w.reviewRunID.Load() {
					w.setReviewText(content)
				}
			})
		})
		content := strings.TrimSpace(review.String())
		logger.Printf(
			"review finished duration=%s answer_chars=%d error=%v",
			time.Since(startedAt).Round(time.Millisecond), len([]rune(content)), diagnosticError(err),
		)
		cancel()
		closeInterviewDebugLog(logger, logFile)
		fyne.Do(func() {
			if runID != w.reviewRunID.Load() {
				return
			}
			w.reviewCancel = nil
			w.reviewing = false
			w.reviewButton.Enable()
			if err != nil {
				w.setLocalizedReviewStatus("history.generate_failed", analysisData(info.Kind, err))
				return
			}
			w.selected = &info
			for index := range w.sessions {
				if w.sessions[index].Directory == info.Directory {
					w.sessions[index] = info
					break
				}
			}
			w.list.Refresh()
			w.locale.setButton(w.reviewButton, "history.regenerate", analysisData(info.Kind, nil))
			w.setLocalizedReviewStatus("history.generation_saved", analysisData(info.Kind, nil))
		})
	}()
}

func streamAndSaveSessionAnalysis(
	ctx context.Context,
	model interviewAnswerModel,
	info *SessionInfo,
	messages []interviewChatMessage,
	isCurrent func() bool,
	onThinking func(),
	onToken func(string),
) error {
	var content strings.Builder
	err := model.streamAnswer(ctx, messages, onThinking, func(token string) {
		content.WriteString(token)
		onToken(token)
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isCurrent() {
		return context.Canceled
	}
	return saveSessionAnalysisResult(info, content.String())
}

func (w *sessionHistoryWin) cancelInterviewReview() {
	if w.reviewCancel != nil {
		w.reviewCancel()
		w.reviewCancel = nil
	}
	w.reviewing = false
}

func (w *sessionHistoryWin) confirmDeleteSession() {
	if w.selected == nil {
		return
	}
	info := *w.selected
	showLocalizedConfirm(
		tr("history.delete"),
		trData("history.confirm_delete", map[string]any{"Name": info.Name}),
		func(confirm bool) {
			if !confirm {
				return
			}
			if err := deleteSessionDirectory(w.setting.sessionRootDir, info); err != nil {
				showLocalizedError(err, w.window)
				return
			}
			w.searchSessions()
			w.locale.setLabel(w.hint, "history.deleted", map[string]any{"Name": info.Name})
		},
		w.window,
	)
}

func deleteSessionDirectory(rootDir string, info SessionInfo) error {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve session root: %w", err)
	}
	target, err := filepath.Abs(info.Directory)
	if err != nil {
		return fmt.Errorf("resolve session directory: %w", err)
	}
	if filepath.Dir(target) != root || filepath.Base(target) != info.Name || target == root {
		return fmt.Errorf("refusing to delete session outside its root directory")
	}

	entryInfo, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect session directory: %w", err)
	}
	if !entryInfo.IsDir() {
		return fmt.Errorf("session path is not a directory")
	}

	data, err := os.ReadFile(filepath.Join(target, sessionManifestName))
	if err != nil {
		return fmt.Errorf("read session manifest before deletion: %w", err)
	}
	var manifest SessionInfo
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode session manifest before deletion: %w", err)
	}
	if manifest.Name != info.Name {
		return fmt.Errorf("session manifest name does not match selected session")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete session directory: %w", err)
	}
	return nil
}
