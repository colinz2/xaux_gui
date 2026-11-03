package gui

import (
	"context"
	"encoding/json"
	"errors"
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
	"fyne.io/fyne/v2/dialog"
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
		dialog.ShowError(err, currentMainWin.win)
		return
	}
	history := &sessionHistoryWin{setting: setting}
	history.window = fyne.CurrentApp().NewWindow("会话历史")
	history.buildContent()
	history.window.Resize(fyne.NewSize(1100, 700))
	history.window.CenterOnScreen()
	history.window.SetOnClosed(func() {
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
	w.searchEntry.SetPlaceHolder("搜索会话名称或转写内容")
	w.searchEntry.OnChanged = func(string) {
		w.searchSessions()
	}
	w.searchEntry.OnSubmitted = func(string) {
		w.searchSessions()
	}
	searchButton := widget.NewButtonWithIcon("搜索", theme.SearchIcon(), w.searchSessions)
	left := container.NewBorder(
		container.NewBorder(nil, nil, nil, searchButton, w.searchEntry),
		w.hint,
		nil, nil,
		w.list,
	)

	w.name = widget.NewLabel("选择一个会话")
	w.name.TextStyle.Bold = true
	w.name.Wrapping = fyne.TextWrapBreak
	w.deleteButton = widget.NewButtonWithIcon("删除会话", theme.DeleteIcon(), w.confirmDeleteSession)
	w.deleteButton.Disable()
	w.reviewButton = widget.NewButton("一键生成复盘", w.generateSessionAnalysis)
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
	w.transcript.SetPlaceHolder("无转写内容")
	w.transcript.Disable()
	w.jobDescription = widget.NewLabel("")
	w.jobDescription.Wrapping = fyne.TextWrapBreak
	w.jobDescription.Selectable = true
	w.review = widget.NewLabel("")
	w.review.Wrapping = fyne.TextWrapBreak
	w.review.Selectable = true
	w.reviewScroll = container.NewVScroll(w.review)
	w.reviewEmpty = widget.NewLabel("面试会话结束后，可以生成 AI 复盘。")
	w.reviewEmpty.Wrapping = fyne.TextWrapBreak
	w.reviewEmpty.Importance = widget.LowImportance
	reviewContent := container.NewStack(w.reviewScroll, w.reviewEmpty)
	recordingsHeader := widget.NewLabelWithStyle("录音文件", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	transcriptHeader := widget.NewLabelWithStyle("转写结果", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.reviewHeader = widget.NewLabelWithStyle("AI 复盘", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.transcriptTab = container.NewTabItem("转写", container.NewBorder(transcriptHeader, nil, nil, nil, w.transcript))
	w.reviewTab = container.NewTabItem("AI 复盘", container.NewBorder(w.reviewHeader, nil, nil, nil, reviewContent))
	w.jobTab = container.NewTabItem("职位描述", container.NewVScroll(w.jobDescription))
	w.historyTabs = container.NewAppTabs(w.transcriptTab, w.reviewTab)
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
		w.hint.SetText("加载失败：" + err.Error())
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
				readErr := fmt.Errorf("读取会话 %q 的转写失败：%w", info.Name, err)
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

	switch {
	case query == "" && len(sessions) == 0:
		w.hint.SetText("暂无会话")
	case query == "":
		w.hint.SetText(fmt.Sprintf("共 %d 个会话；主目录：%s", len(sessions), w.setting.sessionRootDir))
	case len(sessions) == 0:
		w.hint.SetText(fmt.Sprintf("没有找到匹配“%s”的会话", query))
	default:
		w.hint.SetText(fmt.Sprintf("找到 %d 个匹配“%s”的会话", len(sessions), query))
	}
	if firstReadErr != nil {
		w.hint.SetText(fmt.Sprintf("%s；%d 个转写文件读取失败（%v）",
			w.hint.Text, failedReads, firstReadErr))
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
	w.name.SetText("选择一个会话")
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
	defer w.details.Refresh()

	selected := info
	w.selected = &selected
	w.configureHistoryTabs(info)
	w.deleteButton.Enable()
	w.name.SetText(info.Name)
	endedAt := info.UpdatedAt
	if info.Status == SessionStatusRunning {
		endedAt = time.Now()
	}
	duration := formatSessionDuration(endedAt.Sub(info.CreatedAt))
	w.metadata.SetText(fmt.Sprintf(
		"创建时间：%s\n持续时长：%s\n任务类型：%s    状态：%s",
		info.CreatedAt.Local().Format("2006-01-02 15:04:05"),
		duration,
		sessionKindText(info.Kind),
		sessionStatusText(info.Status),
	))
	w.directory.SetText("保存目录：" + info.Directory)
	w.detailError.SetText("")
	if info.Error != "" {
		w.detailError.SetText("任务错误：" + info.Error)
	}

	rows := make([]fyne.CanvasObject, 0, len(info.Recordings))
	for _, recording := range info.Recordings {
		fileName := filepath.Base(recording)
		path := filepath.Join(info.Directory, fileName)
		label := widget.NewLabel(fileName)
		label.Wrapping = fyne.TextWrapBreak
		play := widget.NewButtonWithIcon("播放", theme.MediaPlayIcon(), func() {
			if err := w.playRecording(path); err != nil {
				dialog.ShowError(err, w.window)
			}
		})
		if fileInfo, err := os.Stat(path); err == nil && fileInfo.Size() <= wavHeaderSize {
			label.SetText(fileName + "（无音频数据）")
			play.Disable()
		}
		rows = append(rows, container.NewBorder(nil, nil, label, play))
	}
	if len(rows) == 0 {
		rows = append(rows, widget.NewLabel("无录音文件"))
	}
	w.recordings.Objects = rows
	w.recordings.Refresh()

	w.transcript.SetText("")
	w.jobDescription.SetText("")
	if info.TranscriptFile != "" {
		data, err := os.ReadFile(filepath.Join(info.Directory, filepath.Base(info.TranscriptFile)))
		if err != nil {
			w.detailError.SetText("读取转写失败：" + err.Error())
		} else {
			w.transcript.SetText(string(data))
		}
	}
	if info.Kind == SessionKindInterview && info.JobDescriptionFile != "" {
		job, err := readInterviewReference(filepath.Join(info.Directory, filepath.Base(info.JobDescriptionFile)), "职位描述")
		if err != nil {
			w.detailError.SetText(strings.TrimSpace(w.detailError.Text + "\n" + err.Error()))
		} else {
			w.jobDescription.SetText(job)
		}
	}
	w.showSessionAnalysis(info)
}

func openSessionRecording(path string, openFile func(string) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("无法访问录音文件：%w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("录音路径不是普通文件")
	}
	if info.Size() <= wavHeaderSize {
		return errors.New("录音文件没有音频数据")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("无法解析录音路径：%w", err)
	}
	if err := openFile(absolutePath); err != nil {
		return fmt.Errorf("无法打开录音，请检查系统默认播放器：%w", err)
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
	if kind == SessionKindInterview {
		w.transcriptTab.Text = "面试对话"
		w.reviewTab.Text = "AI 复盘"
		w.reviewHeader.SetText("AI 面试复盘")
		w.reviewEmpty.SetText("面试会话结束后，可以生成 AI 复盘。")
	} else {
		w.transcriptTab.Text = "转写"
		w.reviewTab.Text = "AI 总结"
		w.reviewHeader.SetText("AI 总结")
		w.reviewEmpty.SetText("转写会话结束后，可以生成 AI 总结。")
	}
	items := []*container.TabItem{w.transcriptTab, w.reviewTab}
	if kind == SessionKindInterview && info.JobDescriptionFile != "" {
		items = append(items, w.jobTab)
	}
	w.historyTabs.SetItems(items)
	w.historyTabs.Select(w.transcriptTab)
	w.historyTabs.Show()
}

func analysisLabel(kind SessionKind) string {
	if kind == SessionKindTranscription {
		return "总结"
	}
	return "复盘"
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
	label := analysisLabel(info.Kind)
	file := info.ReviewFile
	if info.Kind == SessionKindTranscription {
		file = info.SummaryFile
	}
	w.reviewButton.Show()
	w.reviewButton.SetText("一键生成" + label)
	if info.Status == SessionStatusRunning {
		w.setReviewStatus("结束会话后可生成" + label)
		w.logInterviewReviewActionState(info, false, "session_running")
		return
	}
	if info.TranscriptFile == "" {
		w.setReviewStatus("没有可用于" + label + "的转写记录")
		w.logInterviewReviewActionState(info, false, "transcript_missing")
		return
	}

	w.reviewButton.Enable()
	if file == "" {
		w.setReviewStatus("尚未生成" + label)
		w.logInterviewReviewActionState(info, true, "review_not_generated")
		return
	}
	data, err := os.ReadFile(filepath.Join(info.Directory, filepath.Base(file)))
	if err != nil {
		w.setReviewStatus("读取" + label + "失败：" + err.Error())
		w.logInterviewReviewActionState(info, true, "saved_review_read_failed")
		return
	}
	w.setReviewText(string(data))
	w.reviewButton.SetText("重新生成" + label)
	w.setReviewStatus("已保存" + label)
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
	w.reviewStatus.SetText(text)
	if text == "" {
		w.reviewStatus.Hide()
	} else {
		w.reviewStatus.Show()
	}
	if w.details != nil {
		w.details.Refresh()
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
	label := analysisLabel(info.Kind)
	if w.setting.modelConfigDir == "" {
		w.setting.modelConfigDir = fyne.CurrentApp().Storage().RootURI().Path()
	}
	logger, logFile, err := openInterviewDebugLogger(w.setting.modelConfigDir)
	if err != nil {
		w.setReviewStatus("打开诊断日志失败：" + err.Error())
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
		w.setReviewStatus(label + "准备失败：" + err.Error())
	}
	transcriptPath := filepath.Join(info.Directory, filepath.Base(info.TranscriptFile))
	transcript, err := os.ReadFile(transcriptPath)
	if err != nil {
		failPreflight("read_transcript", err)
		return
	}
	if strings.TrimSpace(string(transcript)) == "" {
		failPreflight("empty_transcript", localizedError("转写记录为空", "transcript is empty", nil))
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
		job, err := readInterviewReference(filepath.Join(info.Directory, filepath.Base(info.JobDescriptionFile)), "职位描述")
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
	w.setReviewStatus("正在分析转写记录…")
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
					w.setReviewStatus("模型正在分析转写记录…")
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
				w.setReviewStatus(label + "生成失败：" + err.Error())
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
			w.reviewButton.SetText("重新生成" + label)
			w.setReviewStatus(label + "已保存")
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
	dialog.ShowConfirm(
		"删除会话",
		fmt.Sprintf("确定删除会话“%s”及其录音、转写、总结或复盘文件吗？此操作不可恢复。", info.Name),
		func(confirm bool) {
			if !confirm {
				return
			}
			if err := deleteSessionDirectory(w.setting.sessionRootDir, info); err != nil {
				dialog.ShowError(err, w.window)
				return
			}
			w.searchSessions()
			w.hint.SetText(fmt.Sprintf("已删除会话：%s", info.Name))
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
