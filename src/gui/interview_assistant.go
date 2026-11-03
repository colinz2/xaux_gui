package gui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

var activeInterview struct {
	sync.Mutex
	window *interviewAssistant
	setup  fyne.Window
}

type interviewTranscriptRow struct {
	content   *widget.Label
	status    *widget.Label
	generate  *widget.Button
	container *fyne.Container
}

type interviewAnswer struct {
	sequence   int
	timestamp  string
	question   string
	content    string
	errorText  string
	saved      bool
	superseded atomic.Bool
	status     *widget.Label
	answer     *widget.Label
	error      *widget.Label
}

type interviewQuestion struct {
	messages   []interviewChatMessage
	answer     *interviewAnswer
	queuedAt   time.Time
	generation uint64
}

type interviewQuestionCheck struct {
	turnIndex int
	messages  []interviewChatMessage
}

type interviewAssistant struct {
	window       fyne.Window
	session      *Session
	modelConfig  modelConfig
	model        interviewAnswerModel
	reference    interviewContext
	captures     []*asrSoundCap
	turns        []interviewTurn
	rows         []*interviewTranscriptRow
	answers      []*interviewAnswer
	savedEntries []SessionTranscriptEntry

	status           *widget.Label
	duration         *sessionDurationView
	pauseButton      *widget.Button
	stopButton       *widget.Button
	transcript       *fyne.Container
	transcriptScroll *container.Scroll
	emptyTranscript  *widget.Label
	answerList       *fyne.Container
	answerScroll     *container.Scroll
	emptyAnswers     *widget.Label

	ctx              context.Context
	cancel           context.CancelFunc
	questionQueue    chan interviewQuestion
	checkQueue       chan interviewQuestionCheck
	answerMu         sync.Mutex
	activeAnswer     *interviewAnswer
	answerCancel     context.CancelFunc
	answerVersion    atomic.Uint64
	latestAnswerTurn int
	hasAnswerTurn    bool
	debugLog         *log.Logger
	debugLogFile     *os.File
	debugLogOnce     sync.Once
	releaseOnce      sync.Once
	running          atomic.Bool
	stopping         atomic.Bool
	shutdownOnce     sync.Once
	shutdownDone     chan struct{}
	captureWG        sync.WaitGroup
	answerWG         sync.WaitGroup
	runMu            sync.Mutex
	runErr           error
	paused           bool
	transitioning    bool
	stopRequested    bool
}

func runInterviewAssistant() {
	activeInterview.Lock()
	assistant := activeInterview.window
	setup := activeInterview.setup
	activeInterview.Unlock()
	if assistant != nil {
		if assistant.window != nil {
			assistant.window.Show()
			assistant.window.RequestFocus()
		}
		return
	}
	if setup != nil {
		setup.Show()
		setup.RequestFocus()
		return
	}
	if activeTask := activeVoiceTask(); activeTask != "" {
		fyneDialog.ShowInformation(
			"面试",
			"当前正在使用“"+activeTask+"”任务，请先结束后再开始面试。",
			currentMainWin.win,
		)
		return
	}

	showInterviewSetup()
}

func showInterviewSetup() {
	window := fyne.CurrentApp().NewWindow("新建面试会话")
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("留空则使用当前时间")
	jobEntry := widget.NewMultiLineEntry()
	jobEntry.SetPlaceHolder("可粘贴职位描述（可选）")
	jobEntry.SetMinRowsVisible(7)
	jobEntry.Wrapping = fyne.TextWrapBreak
	information := widget.NewLabel("职位描述保存在会话目录，并用于面试实时回答和复盘。")
	information.Wrapping = fyne.TextWrapBreak
	cancel := widget.NewButton("取消", window.Close)
	start := widget.NewButton("开始", func() {
		if err := startInterviewAssistant(nameEntry.Text, jobEntry.Text); err != nil {
			fyneDialog.ShowError(err, window)
			return
		}
		window.Close()
	})
	content := container.NewPadded(container.NewBorder(
		container.NewVBox(widget.NewLabel("会话名称"), nameEntry, widget.NewLabel("职位描述（JD）")),
		container.NewVBox(information, container.NewHBox(layout.NewSpacer(), cancel, start)),
		nil, nil, jobEntry,
	))
	window.SetContent(content)
	window.Resize(fyne.NewSize(640, 430))
	window.CenterOnScreen()
	activeInterview.Lock()
	activeInterview.setup = window
	activeInterview.Unlock()
	window.SetOnClosed(func() {
		activeInterview.Lock()
		if activeInterview.setup == window {
			activeInterview.setup = nil
		}
		activeInterview.Unlock()
	})
	window.Show()
}

func closeInterviewSetup() {
	activeInterview.Lock()
	window := activeInterview.setup
	activeInterview.Unlock()
	if window != nil {
		window.Close()
	}
}

func startInterviewAssistant(requestedName, jobDescription string) error {
	jobDescription, err := validateInterviewContext(jobDescription, "职位描述")
	if err != nil {
		return err
	}
	setting, err := currentAppSetting()
	if err != nil {
		return err
	}
	if !setting.speakerSelected {
		return errors.New("请先在首页选中扬声器设备")
	}
	if strings.TrimSpace(setting.proxyAddr) == "" {
		return errors.New("请先配置并启动 ASR Proxy")
	}
	resume, err := loadInterviewResume(setting.resumeEnabled, setting.resumePath)
	if err != nil {
		return err
	}
	knowledge, err := loadInterviewKnowledge(setting.knowledgeEnabled, setting.knowledgePath)
	if err != nil {
		return err
	}
	if err := setting.reloadModelConfig(); err != nil {
		return fmt.Errorf("读取大模型配置失败：%w", err)
	}
	if err := setting.modelConfig.validate(); err != nil {
		return fmt.Errorf("请先在“开始 > 设置”完善大模型配置：%w", err)
	}
	debugLogger, debugLogFile, err := openInterviewDebugLogger(setting.modelConfigDir)
	if err != nil {
		return err
	}
	model, err := newChatCompletionModel(setting.modelConfig)
	if err != nil {
		closeInterviewDebugLog(debugLogger, debugLogFile)
		return fmt.Errorf("面试模型配置无效：%w", err)
	}
	model.debugLog = debugLogger
	assistant := newInterviewAssistant(model, setting.modelConfig)
	if assistant == nil {
		closeInterviewDebugLog(debugLogger, debugLogFile)
		return fmt.Errorf("当前正在使用“%s”任务，请先结束后再开始面试", activeVoiceTask())
	}
	assistant.reference = interviewContext{jobDescription: jobDescription, resume: resume, knowledge: knowledge}
	assistant.debugLog = debugLogger
	assistant.debugLogFile = debugLogFile
	debugLogger.Printf(
		"session starting model=%q reasoning_effort=%q max_tokens=%d log_file=%q",
		model.model, reasoningEffortForModel(model.model), maxTokensForModel(model.model), debugLogFile.Name(),
	)

	session, err := CreateSession(setting.sessionRootDir, requestedName, SessionKindInterview)
	if err != nil {
		assistant.release()
		return err
	}
	if err := session.SaveJobDescription(jobDescription); err != nil {
		if finishErr := session.Finish(SessionStatusFailed, nil, err); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
		assistant.release()
		return err
	}
	assistant.session = session
	captures, err := newASRSoundCaps(&setting.Setting, session, assistant.setCaptureError)
	if err != nil {
		if finishErr := session.Finish(SessionStatusFailed, nil, err); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
		assistant.release()
		return err
	}
	assistant.captures = captures
	assistant.buildContent()
	assistant.running.Store(true)

	for _, capture := range captures {
		capture.target = assistant
		assistant.captureWG.Add(1)
		go func(capture *asrSoundCap) {
			defer assistant.captureWG.Done()
			if err := capture.Run(); err != nil {
				assistant.setCaptureError(err)
			}
		}(capture)
		if err := <-capture.soundCap.StartDone(); err != nil {
			go assistant.failStartup(localizedError(
				fmt.Sprintf("启动音源 %q 失败", capture.deviceName),
				fmt.Sprintf("start audio source %q failed", capture.deviceName),
				err,
			))
			return nil
		}
	}

	assistant.startAnswerWorker()
	activeInterview.Lock()
	activeInterview.window = assistant
	activeInterview.Unlock()
	return nil
}

func newInterviewAssistant(model interviewAnswerModel, config modelConfig) *interviewAssistant {
	if !acquireVoiceTask(interviewAssistantName) {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &interviewAssistant{
		modelConfig:   config,
		model:         model,
		ctx:           ctx,
		cancel:        cancel,
		questionQueue: make(chan interviewQuestion, 1),
		checkQueue:    make(chan interviewQuestionCheck, 32),
		shutdownDone:  make(chan struct{}),
	}
}

func (s *interviewAssistant) buildContent() {
	info := s.session.Snapshot()

	s.status = widget.NewLabel("正在连接音源…")
	s.status.Importance = widget.SuccessImportance
	s.duration = newSessionDurationView(widget.NewLabel(""), info.CreatedAt)
	s.pauseButton = widget.NewButton("暂停", s.togglePause)
	s.stopButton = widget.NewButtonWithIcon("停止并保存", mytheme.ResEnd, s.shutdown)
	toolbar := container.NewHBox(
		s.status,
		layout.NewSpacer(),
		s.duration.label,
		s.pauseButton,
		s.stopButton,
	)

	s.emptyTranscript = widget.NewLabel("等待面试官提问…")
	s.emptyTranscript.Importance = widget.LowImportance
	s.transcript = container.NewVBox(container.NewCenter(s.emptyTranscript))
	s.transcriptScroll = container.NewVScroll(s.transcript)
	s.emptyAnswers = widget.NewLabel("面试官的问题完成转写后，这里会显示关键词和简短思路。")
	s.emptyAnswers.Wrapping = fyne.TextWrapBreak
	s.emptyAnswers.Importance = widget.LowImportance
	s.answerList = container.NewVBox(container.NewCenter(s.emptyAnswers))
	s.answerScroll = container.NewVScroll(s.answerList)

	transcriptHeader := widget.NewLabelWithStyle("实时对话", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	answerHeader := widget.NewLabelWithStyle("关键词与思路", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	left := container.NewBorder(container.NewPadded(transcriptHeader), nil, nil, nil, s.transcriptScroll)
	right := container.NewBorder(container.NewPadded(answerHeader), nil, nil, nil, s.answerScroll)
	split := container.NewHSplit(left, right)
	split.Offset = 0.43
	content := container.NewBorder(container.NewPadded(toolbar), nil, nil, nil, split)
	s.window = fyne.CurrentApp().NewWindow("面试")
	s.window.SetContent(content)
	s.window.Resize(fyne.NewSize(1000, 680))
	s.window.CenterOnScreen()
	s.window.SetCloseIntercept(s.shutdown)
	s.window.SetOnClosed(s.release)
	s.window.Show()
}

func (s *interviewAssistant) updateASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if capture == nil || response == nil {
		return
	}
	source := "speaker"
	if capture.isMicrophone {
		source = "microphone"
	}
	if response.Type == x.TypeSentenceStart {
		_, interval := capture.acceptASRResult(response)
		s.debugf("asr event source=%s type=%s interval_ms=%d", source, response.Type, interval.Milliseconds())
		fyne.Do(func() {
			capture.resultIndex = -1
			capture.writeListIndex = -1
		})
		return
	}
	if response.Type != x.TypeRecognizing && response.Type != x.TypeSentenceEnd {
		return
	}
	receivedAt := time.Now()
	accepted, interval := capture.acceptASRResult(response)
	s.debugf(
		"asr event source=%s type=%s interim=%t index=%d text_chars=%d interval_ms=%d accepted=%t",
		source, response.Type, response.Result.Interim, response.Result.Index,
		utf8.RuneCountInString(response.Result.Result), interval.Milliseconds(), accepted,
	)
	if !accepted {
		return
	}
	fyne.Do(func() {
		s.debugf("asr ui update source=%s type=%s queue_ms=%d",
			source, response.Type, time.Since(receivedAt).Milliseconds())
		s.applyASRResult(capture, response)
	})
}

func (s *interviewAssistant) applyASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if !s.running.Load() || s.transcript == nil {
		return
	}
	resultIndex := response.Result.Index
	content := response.Result.Result
	isFinal := asrResponseIsFinal(response)
	sameResult := capture.resultIndex == resultIndex &&
		capture.writeListIndex >= 0 && capture.writeListIndex < len(s.turns)
	if content == "" && !sameResult {
		return
	}

	var (
		index      int
		newlyFinal bool
	)
	if sameResult {
		index = capture.writeListIndex
		turn := &s.turns[index]
		newlyFinal = !turn.final && isFinal
		if content != "" {
			turn.content = content
		}
		turn.final = isFinal
		s.updateTranscriptRow(index, turn)
	} else {
		index = len(s.turns)
		turn := &interviewTurn{
			timestamp:    time.Now().Format("2006-01-02 15:04:05"),
			deviceName:   capture.deviceName,
			content:      content,
			isMicrophone: capture.isMicrophone,
			final:        isFinal,
		}
		newlyFinal = turn.final
		capture.writeListIndex = index
		s.turns = append(s.turns, *turn)
		s.rows = append(s.rows, s.newTranscriptRow(turn, index))
		if len(s.rows) == 1 {
			s.transcript.Objects = nil
		} else {
			s.transcript.Objects = append(s.transcript.Objects, widget.NewSeparator())
		}
		s.transcript.Objects = append(s.transcript.Objects, s.rows[index].container)
	}
	capture.resultIndex = resultIndex
	turn := &s.turns[index]
	if newlyFinal {
		s.savedEntries = append(s.savedEntries, SessionTranscriptEntry{
			Timestamp: turn.timestamp,
			Device:    turn.deviceName,
			Content:   turn.content,
		})
		if !turn.isMicrophone && strings.TrimSpace(turn.content) != "" && !s.stopping.Load() {
			s.queueQuestionCheck(index)
		}
	}
	s.transcript.Refresh()
	s.transcriptScroll.ScrollToBottom()
}

func (s *interviewAssistant) newTranscriptRow(turn *interviewTurn, index int) *interviewTranscriptRow {
	icon := widget.NewIcon(mytheme.ResSpeakerIcon)
	if turn.isMicrophone {
		icon.SetResource(mytheme.ResMicrophoneIcon)
	}
	title := widget.NewLabel(turn.deviceName + "  " + turn.timestamp)
	title.TextStyle.Bold = true
	status := widget.NewLabel("")
	status.Importance = widget.LowImportance
	generate := widget.NewButton("生成提示", func() {
		if s.running.Load() && !s.stopping.Load() {
			s.rows[index].generate.Hide()
			s.rows[index].status.SetText("手动生成")
			s.enqueueQuestion(index)
		}
	})
	generate.Hide()
	content := widget.NewLabel(turn.content)
	content.Wrapping = fyne.TextWrapBreak
	content.Selectable = true
	row := container.NewVBox(
		container.NewHBox(icon, title, layout.NewSpacer(), status, generate),
		container.NewPadded(content),
	)
	result := &interviewTranscriptRow{content: content, status: status, generate: generate, container: row}
	s.setTranscriptRowStatus(result, turn)
	return result
}

func (s *interviewAssistant) updateTranscriptRow(index int, turn *interviewTurn) {
	row := s.rows[index]
	row.content.SetText(turn.content)
	s.setTranscriptRowStatus(row, turn)
}

func (s *interviewAssistant) setTranscriptRowStatus(row *interviewTranscriptRow, turn *interviewTurn) {
	if turn.final {
		row.status.SetText("")
		return
	}
	row.status.SetText("识别中")
}

func (s *interviewAssistant) queueQuestionCheck(index int) {
	row := s.rows[index]
	row.status.SetText("判断中")
	check := interviewQuestionCheck{
		turnIndex: index,
		messages:  buildInterviewQuestionMessages(s.turns[:index+1]),
	}
	select {
	case s.checkQueue <- check:
	default:
		row.status.SetText("判断队列已满")
		row.generate.Show()
		s.setStatus("问题判断队列已满，可在对话中手动生成提示", widget.WarningImportance)
		s.debugf("question check rejected reason=queue_full")
	}
}

func (s *interviewAssistant) applyQuestionDecision(index int, decision interviewQuestionDecision, err error) {
	row := s.rows[index]
	switch {
	case err != nil:
		row.status.SetText("判断失败")
		row.generate.Show()
		s.setStatus("问题判断失败："+err.Error(), widget.DangerImportance)
	case decision == interviewQuestionYes:
		if s.hasAnswerTurn && index < s.latestAnswerTurn {
			row.status.SetText("较早问题")
			row.generate.Show()
			return
		}
		row.status.SetText("")
		s.enqueueQuestion(index)
	case decision == interviewQuestionNo:
		row.status.SetText("非提问")
		row.generate.Show()
	default:
		row.status.SetText("无法判断")
		row.generate.Show()
	}
}

func (s *interviewAssistant) startQuestionCheckWorker() {
	s.answerWG.Add(1)
	go func() {
		defer s.answerWG.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case check := <-s.checkQueue:
				ctx, cancel := context.WithTimeout(s.ctx, interviewQuestionTimeout)
				decision, err := classifyInterviewTurn(ctx, s.model, check.messages)
				cancel()
				if s.ctx.Err() != nil {
					return
				}
				if errors.Is(err, context.DeadlineExceeded) {
					err = localizedError(
						fmt.Sprintf("等待问题判断超过 %s", interviewQuestionTimeout),
						fmt.Sprintf("question classification timed out after %s", interviewQuestionTimeout), nil,
					)
				}
				s.debugf("question check completed decision=%d error=%s", decision, diagnosticError(err))
				fyne.DoAndWait(func() {
					if !s.stopping.Load() {
						s.applyQuestionDecision(check.turnIndex, decision, err)
					}
				})
			}
		}
	}()
}

func (s *interviewAssistant) enqueueQuestion(turnIndex int) {
	if turnIndex < 0 || turnIndex >= len(s.turns) {
		return
	}
	turn := s.turns[turnIndex]
	if turn.isMicrophone || !turn.final {
		return
	}
	if !s.hasAnswerTurn || turnIndex > s.latestAnswerTurn {
		s.latestAnswerTurn = turnIndex
		s.hasAnswerTurn = true
	}
	answer := &interviewAnswer{
		sequence:  len(s.answers) + 1,
		timestamp: turn.timestamp,
		question:  turn.content,
		status:    widget.NewLabel("排队中"),
		answer:    widget.NewLabel("等待生成…"),
		error:     widget.NewLabel(""),
	}
	answer.answer.Wrapping = fyne.TextWrapBreak
	answer.answer.Selectable = true
	answer.error.Wrapping = fyne.TextWrapBreak
	answer.error.Importance = widget.DangerImportance
	questionLabel := widget.NewLabel(turn.content)
	questionLabel.Wrapping = fyne.TextWrapBreak
	questionLabel.Selectable = true
	header := container.NewHBox(
		widget.NewLabelWithStyle("提示", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		answer.status,
	)
	card := widget.NewCard(
		"问题",
		turn.timestamp,
		container.NewVBox(questionLabel, widget.NewSeparator(), header, answer.answer, answer.error),
	)
	if len(s.answers) == 0 {
		s.answerList.Objects = nil
	} else {
		s.answerList.Objects = append(s.answerList.Objects, widget.NewSeparator())
	}
	s.answerList.Objects = append(s.answerList.Objects, card)
	s.answers = append(s.answers, answer)
	s.answerList.Refresh()
	s.answerScroll.ScrollToBottom()

	messages := buildInterviewMessages(s.turns[:turnIndex+1], s.modelConfig.SystemPrompt, s.reference)
	question := interviewQuestion{
		messages: messages,
		answer:   answer,
		queuedAt: time.Now(),
	}
	question.generation = s.answerVersion.Add(1)
	s.cancelActiveAnswer()
	s.supersedeQueuedAnswers()
	s.debugf(
		"question queued id=%d context_messages=%d question_chars=%d queue_depth=%d",
		answer.sequence, len(messages), len([]rune(turn.content)), len(s.questionQueue),
	)
	select {
	case s.questionQueue <- question:
	default:
		answer.status.SetText("队列已满")
		answer.error.SetText("待生成的问题过多；请停止并保存后重新开始。")
		s.debugf("question rejected id=%d reason=queue_full", answer.sequence)
	}
}

func (s *interviewAssistant) cancelActiveAnswer() {
	s.answerMu.Lock()
	defer s.answerMu.Unlock()
	if s.activeAnswer != nil && s.answerCancel != nil {
		s.activeAnswer.superseded.Store(true)
		s.answerCancel()
	}
}

func (s *interviewAssistant) supersedeQueuedAnswers() {
	for {
		select {
		case question := <-s.questionQueue:
			s.markAnswerSuperseded(question.answer)
		default:
			return
		}
	}
}

func (s *interviewAssistant) beginAnswer(
	question interviewQuestion,
	cancel context.CancelFunc,
) bool {
	s.answerMu.Lock()
	defer s.answerMu.Unlock()
	if s.ctx.Err() != nil || question.generation != s.answerVersion.Load() {
		return false
	}
	s.activeAnswer = question.answer
	s.answerCancel = cancel
	return true
}

func (s *interviewAssistant) finishAnswer(answer *interviewAnswer) {
	s.answerMu.Lock()
	defer s.answerMu.Unlock()
	if s.activeAnswer == answer {
		s.activeAnswer = nil
		s.answerCancel = nil
	}
}

func (s *interviewAssistant) markAnswerSuperseded(answer *interviewAnswer) {
	if answer == nil {
		return
	}
	answer.superseded.Store(true)
	answer.status.SetText("已由新问题替代")
	answer.status.Importance = widget.WarningImportance
	if answer.content != "" && !answer.saved {
		answer.errorText = "新问题到达，回答未完成"
		answer.error.SetText(answer.errorText)
		s.appendAnswerToSession(answer)
	}
}

func (s *interviewAssistant) startAnswerWorker() {
	s.startQuestionCheckWorker()
	s.answerWG.Add(1)
	go func() {
		defer s.answerWG.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case question := <-s.questionQueue:
				if s.ctx.Err() != nil {
					return
				}
				startedAt := time.Now()
				s.debugf(
					"answer worker started id=%d queue_wait=%s",
					question.answer.sequence, startedAt.Sub(question.queuedAt).Round(time.Millisecond),
				)
				fyne.DoAndWait(func() {
					if !s.stopping.Load() {
						question.answer.status.SetText("请求中")
						question.answer.answer.SetText("正在连接大模型…")
						s.setStatus("正在请求面试建议…", widget.SuccessImportance)
					}
				})
				requestCtx, cancel := context.WithTimeout(s.ctx, interviewAnswerTimeout)
				if !s.beginAnswer(question, cancel) {
					cancel()
					fyne.DoAndWait(func() {
						s.markAnswerSuperseded(question.answer)
					})
					continue
				}
				firstToken := true
				answerChars := 0
				err := s.model.streamAnswer(requestCtx, question.messages, func() {
					fyne.Do(func() {
						if s.stopping.Load() {
							return
						}
						question.answer.status.SetText("思考中")
						question.answer.status.Importance = widget.WarningImportance
						question.answer.answer.SetText("模型正在思考，建议将在正文生成后显示。")
						s.setStatus("模型思考中…", widget.SuccessImportance)
					})
				}, func(token string) {
					isFirstToken := firstToken
					firstToken = false
					answerChars += len([]rune(token))
					fyne.Do(func() {
						if s.stopping.Load() {
							return
						}
						if isFirstToken {
							question.answer.status.SetText("生成中")
							question.answer.status.Importance = widget.SuccessImportance
							s.setStatus("正在生成面试建议…", widget.SuccessImportance)
						}
						question.answer.content += token
						question.answer.answer.SetText(question.answer.content)
						s.answerScroll.ScrollToBottom()
					})
				})
				cancel()
				s.finishAnswer(question.answer)
				if errors.Is(err, context.DeadlineExceeded) {
					err = localizedError(
						fmt.Sprintf("等待大模型响应超过 %s", interviewAnswerTimeout),
						fmt.Sprintf("model response timed out after %s", interviewAnswerTimeout), nil,
					)
				}
				s.debugf(
					"answer worker finished id=%d duration=%s answer_chars=%d error=%v",
					question.answer.sequence, time.Since(startedAt).Round(time.Millisecond), answerChars, diagnosticError(err),
				)
				fyne.DoAndWait(func() {
					if s.stopping.Load() {
						return
					}
					if question.answer.superseded.Load() {
						s.markAnswerSuperseded(question.answer)
						s.setListeningStatus()
						return
					}
					switch {
					case err != nil:
						question.answer.errorText = err.Error()
						question.answer.status.SetText("生成失败")
						question.answer.error.SetText("错误：" + err.Error())
						question.answer.status.Importance = widget.DangerImportance
						s.setStatus("模型请求失败", widget.DangerImportance)
					default:
						question.answer.status.SetText("已完成")
						question.answer.status.Importance = widget.SuccessImportance
						s.setListeningStatus()
					}
					s.appendAnswerToSession(question.answer)
				})
			}
		}
	}()
}

func (s *interviewAssistant) appendAnswerToSession(answer *interviewAnswer) {
	content := "问题：" + answer.question + "\n\n建议回答：\n" + answer.content
	device := "AI 建议"
	if answer.errorText != "" {
		device = "AI 建议（未完成）"
		content += "\n\n状态：" + answer.errorText
	}
	s.savedEntries = append(s.savedEntries, SessionTranscriptEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Device:    device,
		Content:   content,
	})
	answer.saved = true
}

func (s *interviewAssistant) saveIncompleteTurns() {
	for _, turn := range s.turns {
		if turn.final || strings.TrimSpace(turn.content) == "" {
			continue
		}
		device := turn.deviceName + "（未完成）"
		s.savedEntries = append(s.savedEntries, SessionTranscriptEntry{
			Timestamp: turn.timestamp,
			Device:    device,
			Content:   turn.content,
		})
	}
}

func (s *interviewAssistant) setCaptureError(err error) {
	if err == nil {
		return
	}
	s.runMu.Lock()
	if s.runErr == nil {
		s.runErr = err
	}
	s.runMu.Unlock()
	log.Printf("interview assistant audio capture: %v", err)
	s.debugf("audio capture failed error=%v", err)
	fyne.Do(func() {
		if !s.stopping.Load() && s.status != nil {
			s.setStatus("音频识别异常："+err.Error(), widget.DangerImportance)
		}
		if s.pauseButton != nil {
			s.pauseButton.Disable()
		}
	})
}

func (s *interviewAssistant) setStatus(text string, importance widget.Importance) {
	s.status.SetText(text)
	s.status.Importance = importance
	s.status.Refresh()
}

func (s *interviewAssistant) setListeningStatus() {
	s.runMu.Lock()
	runErr := s.runErr
	s.runMu.Unlock()
	switch {
	case runErr != nil:
		s.setStatus("音频识别异常："+runErr.Error(), widget.DangerImportance)
	case s.paused:
		s.setStatus("已暂停", widget.WarningImportance)
	default:
		s.setStatus("正在监听", widget.SuccessImportance)
	}
}

func (s *interviewAssistant) togglePause() {
	if !s.running.Load() || s.stopping.Load() || s.transitioning {
		return
	}
	s.transitioning = true
	s.pauseButton.Disable()
	pause := !s.paused
	if pause {
		s.setStatus("正在暂停…", widget.WarningImportance)
	} else {
		s.setStatus("正在恢复…", widget.SuccessImportance)
	}
	go func() {
		var pauseErr error
		for _, capture := range s.captures {
			if pause {
				pauseErr = errors.Join(pauseErr, capture.soundCap.Pause())
			} else {
				pauseErr = errors.Join(pauseErr, capture.soundCap.Resume())
			}
		}
		fyne.Do(func() {
			s.transitioning = false
			if s.stopping.Load() {
				return
			}
			if pauseErr != nil {
				log.Printf("pause/resume interview capture: %v", pauseErr)
				s.setStatus("操作失败："+pauseErr.Error(), widget.DangerImportance)
				s.pauseButton.Disable()
				if s.stopRequested {
					s.stopRequested = false
					s.shutdown()
				}
				return
			}
			s.paused = pause
			if pause {
				s.pauseButton.SetText("继续")
				s.duration.Pause()
				s.setStatus("已暂停", widget.WarningImportance)
			} else {
				s.pauseButton.SetText("暂停")
				s.duration.Resume()
				s.setStatus("正在监听", widget.SuccessImportance)
			}
			s.pauseButton.Enable()
			if s.stopRequested {
				s.stopRequested = false
				s.shutdown()
			}
		})
	}()
}

func (s *interviewAssistant) shutdown() {
	if s.transitioning {
		s.stopRequested = true
		s.stopButton.Disable()
		return
	}
	s.shutdownOnce.Do(func() {
		s.debugf("session stopping")
		s.stopping.Store(true)
		s.cancel()
		s.setStatus("正在结束并保存…", widget.MediumImportance)
		s.pauseButton.Disable()
		s.stopButton.Disable()
		s.duration.Stop()
		go func() {
			stopASRSoundCaps(s.captures)
			s.captureWG.Wait()
			s.answerWG.Wait()
			var entries []SessionTranscriptEntry
			fyne.DoAndWait(func() {
				for _, answer := range s.answers {
					if answer.saved || answer.status.Text != "生成中" || answer.content == "" {
						continue
					}
					answer.status.SetText("已中断")
					answer.errorText = "结束时生成被中断"
					s.appendAnswerToSession(answer)
				}
				s.saveIncompleteTurns()
				s.running.Store(false)
				entries = append(entries, s.savedEntries...)
			})

			s.runMu.Lock()
			runErr := s.runErr
			s.runMu.Unlock()
			status := SessionStatusStopped
			if runErr != nil {
				status = SessionStatusFailed
			}
			if err := s.session.Finish(status, entries, runErr); err != nil {
				log.Printf("finish interview session: %v", err)
			}
			s.debugf("session finished status=%q", status)
			s.closeDebugLog()
			fyne.DoAndWait(func() {
				if s.window != nil {
					s.window.SetCloseIntercept(nil)
					s.window.Close()
				}
				s.release()
				refreshActiveSessionHistory()
			})
			close(s.shutdownDone)
		}()
	})
}

func (s *interviewAssistant) failStartup(err error) {
	s.debugf("session startup failed error=%s", diagnosticError(err))
	s.stopping.Store(true)
	s.cancel()
	stopASRSoundCaps(s.captures)
	s.captureWG.Wait()
	var entries []SessionTranscriptEntry
	fyne.DoAndWait(func() {
		s.saveIncompleteTurns()
		s.running.Store(false)
		entries = append(entries, s.savedEntries...)
		if s.window != nil {
			s.window.SetCloseIntercept(nil)
			s.window.Close()
		}
		s.release()
		refreshActiveSessionHistory()
	})
	if finishErr := s.session.Finish(SessionStatusFailed, entries, err); finishErr != nil {
		err = errors.Join(err, finishErr)
	}
	fyne.Do(func() {
		if currentMainWin != nil && !currentMainWin.quitting.Load() {
			fyneDialog.ShowError(err, currentMainWin.win)
		}
	})
}

func (s *interviewAssistant) release() {
	s.releaseOnce.Do(func() {
		s.closeDebugLog()
		activeInterview.Lock()
		if activeInterview.window == s {
			activeInterview.window = nil
		}
		activeInterview.Unlock()
		releaseVoiceTask(interviewAssistantName)
	})
}

func (s *interviewAssistant) debugf(format string, args ...any) {
	if s.debugLog != nil {
		s.debugLog.Printf(format, args...)
	}
}

func (s *interviewAssistant) closeDebugLog() {
	s.debugLogOnce.Do(func() {
		closeInterviewDebugLog(s.debugLog, s.debugLogFile)
	})
}

func stopActiveInterview() <-chan struct{} {
	activeInterview.Lock()
	assistant := activeInterview.window
	activeInterview.Unlock()
	if assistant == nil {
		return nil
	}
	assistant.shutdown()
	return assistant.shutdownDone
}
