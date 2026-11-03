package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSessionUniqueNamesAndPersistence(t *testing.T) {
	root := t.TempDir()
	first, err := CreateSession(root, "recording", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateSession(root, "recording", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot().Name != "recording" || second.Snapshot().Name != "recording-2" {
		t.Fatalf("unexpected unique names: %q, %q", first.Snapshot().Name, second.Snapshot().Name)
	}
	if first.Directory() == second.Directory() {
		t.Fatal("sessions share a directory")
	}

	if err := first.AddRecording("microphone.wav"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Directory(), "microphone.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(SessionStatusStopped, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Finish(SessionStatusFailed, nil, errors.New("capture failed")); err != nil {
		t.Fatal(err)
	}

	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("loaded %d sessions, want 2", len(sessions))
	}
	byName := make(map[string]SessionInfo, len(sessions))
	for _, info := range sessions {
		byName[info.Name] = info
	}
	stopped := byName[first.Snapshot().Name]
	if stopped.Status != SessionStatusStopped || len(stopped.Recordings) != 1 || stopped.Recordings[0] != "microphone.wav" {
		t.Fatalf("recording metadata was not persisted: %+v", stopped)
	}
	failed := byName[second.Snapshot().Name]
	if failed.Status != SessionStatusFailed || failed.Error != "capture failed" {
		t.Fatalf("failure metadata was not persisted: %+v", failed)
	}
}

func TestSessionTranscriptAndDefaultName(t *testing.T) {
	root := t.TempDir()
	session, err := CreateSession(root, ` ../invalid:name `, SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	name := session.Snapshot().Name
	if name == "" || strings.ContainsAny(name, `/\\:`) {
		t.Fatalf("unsafe session name %q", name)
	}
	entries := []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "麦克风", Content: "第一段"},
		{Timestamp: "2026-01-02 03:04:08", Device: "扬声器", Content: "第二段"},
	}
	if err := session.Finish(SessionStatusStopped, entries, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(session.Directory(), sessionTranscriptName))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"麦克风", "第一段", "扬声器", "第二段"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("transcript does not contain %q: %s", expected, text)
		}
	}
	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].TranscriptFile != sessionTranscriptName {
		t.Fatalf("transcript metadata was not loaded: %+v", sessions)
	}
}

func TestInterviewReviewPersistsWithSessionManifest(t *testing.T) {
	root := t.TempDir()
	session, err := CreateSession(root, "面试复盘", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "扬声器", Content: "请介绍一下项目。"},
		{Timestamp: "2026-01-02 03:04:10", Device: "麦克风", Content: "项目使用 Go 开发。"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	info := session.Snapshot()
	finishedAt := info.UpdatedAt
	if err := saveInterviewReview(&info, "  ## 下一步练习\n- 补充项目结果  "); err != nil {
		t.Fatal(err)
	}
	if !info.UpdatedAt.Equal(finishedAt) {
		t.Fatal("saving a review changed the interview session end time")
	}

	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ReviewFile != sessionInterviewReviewName {
		t.Fatalf("review file was not recorded in the manifest: %+v", sessions)
	}
	review, err := os.ReadFile(filepath.Join(sessions[0].Directory, sessions[0].ReviewFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(review) != "## 下一步练习\n- 补充项目结果\n" {
		t.Fatalf("unexpected saved review: %q", review)
	}
}

func TestSessionHistoryWindow(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	session, err := CreateSession(root, "历史测试", SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AddRecording("microphone.wav"); err != nil {
		t.Fatal(err)
	}
	if err := session.AddRecording("speaker.wav"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.Directory(), "microphone.wav"), make([]byte, wavHeaderSize+2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.Directory(), "speaker.wav"), make([]byte, wavHeaderSize), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "麦克风", Content: "历史转写内容"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var openedPath string
	win := &sessionHistoryWin{
		setting: &appSetting{Setting: Setting{sessionRootDir: root}},
		recordingOpener: func(path string) error {
			openedPath = path
			return nil
		},
	}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.window.Resize(fyne.NewSize(1100, 760))
	win.buildContent()
	win.searchSessions()
	win.window.Show()
	if len(win.sessions) != 1 {
		t.Fatalf("history contains %d sessions, want 1", len(win.sessions))
	}
	win.list.Select(0)
	if win.name.Text != "历史测试" || len(win.recordings.Objects) != 2 {
		t.Fatalf("history details were not populated: %q, %d recordings", win.name.Text, len(win.recordings.Objects))
	}
	if win.deleteButton.Disabled() {
		t.Fatal("delete button should be enabled for the selected session")
	}
	for i, row := range win.recordings.Objects {
		button, ok := row.(*fyne.Container).Objects[1].(*widget.Button)
		if !ok || button.Icon == nil || button.Text != "播放" {
			t.Fatalf("recording row has no icon playback button: %#v", row)
		}
		if button.Disabled() != (i == 1) {
			t.Fatalf("recording row %d disabled=%v, want %v", i, button.Disabled(), i == 1)
		}
		if i == 0 {
			fynetest.Tap(button)
			if openedPath != filepath.Join(session.Directory(), "microphone.wav") {
				t.Fatalf("playback opened %q, want the selected recording", openedPath)
			}
		}
	}
	if label := win.recordings.Objects[1].(*fyne.Container).Objects[0].(*widget.Label); !strings.Contains(label.Text, "无音频数据") {
		t.Fatalf("empty recording is not labeled: %q", label.Text)
	}
	if !win.directory.Selectable || !strings.Contains(win.directory.Text, session.Directory()) {
		t.Fatalf("session directory is not available for selection and copying: selectable=%v text=%q",
			win.directory.Selectable, win.directory.Text)
	}
	if !strings.Contains(win.transcript.Text, "历史转写内容") {
		t.Fatalf("history transcript was not loaded: %q", win.transcript.Text)
	}
	initialSize := win.transcript.Size()
	if initialSize.Height <= win.transcript.MinSize().Height {
		t.Fatalf("transcript area did not expand beyond its minimum height: size=%v min=%v",
			initialSize, win.transcript.MinSize())
	}
	win.window.Resize(fyne.NewSize(1400, 1000))
	expandedSize := win.transcript.Size()
	if expandedSize.Width <= initialSize.Width || expandedSize.Height <= initialSize.Height {
		t.Fatalf("transcript area did not grow with window: initial=%v expanded=%v", initialSize, expandedSize)
	}
	if !strings.Contains(win.metadata.Text, "持续时长：00:00:00") {
		t.Fatalf("history duration was not displayed: %q", win.metadata.Text)
	}
}

func TestSessionHistoryDisplaysSavedInterviewReview(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	session, err := CreateSession(root, "复盘显示", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "扬声器", Content: "如何处理并发？"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	info := session.Snapshot()
	if err := saveInterviewReview(&info, "## 待改进\n- 补充并发控制方案"); err != nil {
		t.Fatal(err)
	}

	win := &sessionHistoryWin{setting: &appSetting{Setting: Setting{
		sessionRootDir: root,
		modelConfigDir: root,
	}}}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.buildContent()
	win.showSession(info)
	if len(win.reviewActions.Objects) != 2 ||
		win.reviewActions.Objects[0] != win.reviewButton ||
		win.reviewActions.Objects[1] != win.deleteButton {
		t.Fatal("review status must not share the action row with the review and delete buttons")
	}
	if !strings.Contains(win.review.Text, "补充并发控制方案") {
		t.Fatalf("saved review was not shown: %q", win.review.Text)
	}
	if !win.review.Selectable {
		t.Fatal("saved review text must be selectable and copyable")
	}
	if win.reviewEmpty.Visible() {
		t.Fatal("review placeholder must be hidden when saved review content is shown")
	}
	if win.reviewButton.Disabled() || win.reviewButton.Text != "重新生成复盘" {
		t.Fatalf("review action is not available: disabled=%v text=%q",
			win.reviewButton.Disabled(), win.reviewButton.Text)
	}
	logData, err := os.ReadFile(filepath.Join(root, interviewDebugLogFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "enabled=true reason=\"saved_review_loaded\"") {
		t.Fatalf("review action availability was not logged: %s", logData)
	}
}

func TestSessionHistoryChangesAnalysisByTaskType(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	win := &sessionHistoryWin{setting: &appSetting{Setting: Setting{sessionRootDir: root}}}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.buildContent()

	transcription, err := CreateSession(root, "转写", SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	if err := transcription.Finish(SessionStatusStopped, []SessionTranscriptEntry{
		{Content: "讨论项目进度"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	info := transcription.Snapshot()
	finishedAt := info.UpdatedAt
	if err := saveTranscriptionSummary(&info, "  项目进展顺利  "); err != nil {
		t.Fatal(err)
	}
	if !info.UpdatedAt.Equal(finishedAt) {
		t.Fatal("saving summary changed session end time")
	}
	sessions, err := LoadSessions(root)
	if err != nil || len(sessions) != 1 || sessions[0].SummaryFile != sessionSummaryName {
		t.Fatalf("summary manifest not persisted: sessions=%+v err=%v", sessions, err)
	}
	win.showSession(sessions[0])
	if !strings.Contains(win.metadata.Text, "任务类型：转写") ||
		!win.historyTabs.Visible() || win.transcriptTab.Text != "转写" ||
		win.reviewTab.Text != "AI 总结" || win.reviewHeader.Text != "AI 总结" ||
		win.reviewButton.Text != "重新生成总结" || win.review.Text != "项目进展顺利\n" ||
		!win.review.Selectable {
		t.Fatalf("transcription details incorrect: metadata=%q tab=%q button=%q text=%q",
			win.metadata.Text, win.reviewTab.Text, win.reviewButton.Text, win.review.Text)
	}

	recording, err := CreateSession(root, "录音", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	if err := recording.Finish(SessionStatusStopped, nil, nil); err != nil {
		t.Fatal(err)
	}
	win.showSession(recording.Snapshot())
	if !strings.Contains(win.metadata.Text, "任务类型：录音") || win.historyTabs.Visible() || win.reviewButton.Visible() {
		t.Fatal("recording must not display transcription or AI analysis")
	}
	if err := saveSessionAnalysisResult(&info, ""); err == nil {
		t.Fatal("empty summary must not be saved")
	}
	recordingInfo := recording.Snapshot()
	if err := saveSessionAnalysisResult(&recordingInfo, "不支持"); err == nil {
		t.Fatal("recording must not accept AI analysis")
	}

	interview, err := CreateSession(root, "面试", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	win.showSession(interview.Snapshot())
	if !strings.Contains(win.metadata.Text, "任务类型：面试") ||
		!win.historyTabs.Visible() || win.transcriptTab.Text != "面试对话" ||
		win.reviewTab.Text != "AI 复盘" || win.reviewButton.Text != "一键生成复盘" ||
		!win.reviewButton.Disabled() {
		t.Fatalf("running interview details incorrect: metadata=%q tab=%q button=%q",
			win.metadata.Text, win.reviewTab.Text, win.reviewButton.Text)
	}
	win.clearDetails()
	if win.historyTabs.Visible() || win.reviewButton.Visible() {
		t.Fatal("clearing selection should hide analysis")
	}

	running, err := CreateSession(root, "进行中", SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	win.showSession(running.Snapshot())
	if !win.reviewButton.Disabled() || win.reviewButton.Text != "一键生成总结" ||
		!strings.Contains(win.reviewStatus.Text, "结束会话后") {
		t.Fatal("running transcription should not allow summary generation")
	}
	if err := running.Finish(SessionStatusStopped, nil, nil); err != nil {
		t.Fatal(err)
	}
	win.showSession(running.Snapshot())
	if !win.reviewButton.Disabled() || !strings.Contains(win.reviewStatus.Text, "没有可用于总结") {
		t.Fatal("missing transcript should not allow summary generation")
	}
	info.SummaryFile = "missing-summary.txt"
	win.showSession(info)
	if !strings.Contains(win.reviewStatus.Text, "读取总结失败") || win.reviewButton.Disabled() {
		t.Fatal("missing saved summary should report an error and allow regeneration")
	}
}

func TestTranscriptionSummaryInitializesConfigDirectory(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	if err := saveModelConfig(app.Storage().RootURI().Path(), modelConfig{
		BaseURL: "https://example.test/v1",
		Model:   "test-model",
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	session, err := CreateSession(root, "转写", SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{{Content: "内容"}}, nil); err != nil {
		t.Fatal(err)
	}
	setting := &appSetting{Setting: Setting{sessionRootDir: root}}
	win := &sessionHistoryWin{setting: setting}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.buildContent()
	win.showSession(session.Snapshot())
	win.generateSessionAnalysis()
	if setting.modelConfigDir != app.Storage().RootURI().Path() ||
		!strings.Contains(win.reviewStatus.Text, "请填写 API Key") {
		t.Fatalf("summary preflight did not load configuration: dir=%q status=%q",
			setting.modelConfigDir, win.reviewStatus.Text)
	}
}

func TestSessionHistorySearchesNameAndTranscript(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	sessions := []struct {
		name       string
		transcript string
	}{
		{name: "项目周会", transcript: "讨论了本周的进度"},
		{name: "客户访谈", transcript: "今天的天气怎么样"},
	}
	for _, test := range sessions {
		session, err := CreateSession(root, test.name, SessionKindTranscription)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{
			{Timestamp: "2026-01-02 03:04:05", Device: "麦克风", Content: test.transcript},
		}, nil); err != nil {
			t.Fatal(err)
		}
	}

	win := &sessionHistoryWin{setting: &appSetting{Setting: Setting{sessionRootDir: root}}}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.buildContent()
	win.searchEntry.SetText("客户")
	win.searchEntry.OnChanged("客户")
	if len(win.sessions) != 1 || win.sessions[0].Name != "客户访谈" {
		t.Fatalf("name search matched %+v, want only 客户访谈", win.sessions)
	}

	win.searchEntry.SetText("天气")
	win.searchEntry.OnChanged("天气")
	if len(win.sessions) != 1 || win.sessions[0].Name != "客户访谈" {
		t.Fatalf("transcript search for 天气 matched %+v, want only 客户访谈", win.sessions)
	}
	if win.selected == nil || win.selected.Name != "客户访谈" || !strings.Contains(win.transcript.Text, "今天的天气怎么样") {
		t.Fatalf("first search result was not loaded into the details panel: selected=%+v transcript=%q",
			win.selected, win.transcript.Text)
	}

	win.searchEntry.SetText("不存在的关键词")
	win.searchEntry.OnChanged("不存在的关键词")
	if len(win.sessions) != 0 || !strings.Contains(win.hint.Text, "没有找到匹配") {
		t.Fatalf("unmatched query returned %+v with hint %q", win.sessions, win.hint.Text)
	}

	win.searchEntry.SetText("")
	win.searchEntry.OnChanged("")
	if len(win.sessions) != len(sessions) {
		t.Fatalf("clearing search returned %d sessions, want %d", len(win.sessions), len(sessions))
	}
}

func TestDeleteSessionDirectoryRemovesOnlySelectedSession(t *testing.T) {
	root := t.TempDir()
	selected, err := CreateSession(root, "待删除", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := CreateSession(root, "保留", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(selected.Directory(), "audio.wav")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := deleteSessionDirectory(root, selected.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(selected.Directory()); !os.IsNotExist(err) {
		t.Fatalf("selected session directory still exists: err=%v", err)
	}
	if _, err := os.Stat(keep.Directory()); err != nil {
		t.Fatalf("unselected session was affected: %v", err)
	}
}

func TestDeleteSessionDirectoryRejectsUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	session, err := CreateSession(root, "安全检查", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	info := session.Snapshot()

	tests := []struct {
		name string
		info SessionInfo
	}{
		{name: "outside root", info: SessionInfo{Name: "outside", Directory: t.TempDir()}},
		{name: "manifest mismatch", info: SessionInfo{Name: "wrong-name", Directory: info.Directory}},
		{name: "root directory", info: SessionInfo{Name: filepath.Base(root), Directory: root}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := deleteSessionDirectory(root, test.info); err == nil {
				t.Fatal("unsafe session deletion unexpectedly succeeded")
			}
		})
	}
	if _, err := os.Stat(info.Directory); err != nil {
		t.Fatalf("rejected deletion removed valid session: %v", err)
	}
}

func TestSessionHistoryListItemLayout(t *testing.T) {
	item := sessionHistoryListItem(SessionInfo{
		Name:      "一个非常长的会话名称，应该截断而不能挤乱右侧日期和状态",
		CreatedAt: time.Date(2026, 11, 2, 20, 0, 0, 0, time.Local),
		Status:    SessionStatusStopped,
	})
	name := item.Objects[0].(*widget.Label)
	metadata := item.Objects[1].(*fyne.Container)
	date := metadata.Objects[0].(*widget.Label)
	status := metadata.Objects[1].(*widget.Label)

	if name.Wrapping != fyne.TextWrapOff || name.Truncation != fyne.TextTruncateEllipsis {
		t.Fatalf("long session name is not configured for single-line ellipsis: wrapping=%v truncation=%v",
			name.Wrapping, name.Truncation)
	}
	if date.Text != "2026-11-02 20:00:00" || date.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("date is not right-aligned: %+v", date)
	}
	if status.Text != "已停止" || status.Alignment != fyne.TextAlignTrailing {
		t.Fatalf("status is not right-aligned: %+v", status)
	}
}

func TestSessionDefaultNameAndConcurrentUniqueness(t *testing.T) {
	if got := normalizeSessionName("", time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)); got != "20260102-030405" {
		t.Fatalf("unexpected default name %q", got)
	}

	root := t.TempDir()
	const count = 8
	names := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := CreateSession(root, "same", SessionKindRecording)
			if err != nil {
				errs <- err
				return
			}
			names <- session.Snapshot().Name
		}()
	}
	wg.Wait()
	close(names)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	unique := make(map[string]struct{}, count)
	for name := range names {
		if _, exists := unique[name]; exists {
			t.Errorf("duplicate session name %q", name)
		}
		unique[name] = struct{}{}
	}
	if len(unique) != count {
		t.Fatalf("created %d unique sessions, want %d: %s", len(unique), count, fmt.Sprint(unique))
	}
}
