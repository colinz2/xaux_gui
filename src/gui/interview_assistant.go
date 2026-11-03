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
	generating bool
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
	locale           *localeBindings
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
		showLocalizedInformation(
			tr("menu.interview"),
			trData("interview.active_task", map[string]any{"Task": voiceTaskDisplayName(activeTask)}),
			currentMainWin.win,
		)
		return
	}

	showInterviewSetup()
}

func showInterviewSetup() {
	bindings := &localeBindings{}
	window := fyne.CurrentApp().NewWindow(tr("interview.new_session"))
	bindings.add(func() { window.SetTitle(tr("interview.new_session")) })
	nameEntry := widget.NewEntry()
	bindings.add(func() { nameEntry.SetPlaceHolder(tr("speech.name_placeholder")) })
	jobEntry := widget.NewMultiLineEntry()
	bindings.add(func() { jobEntry.SetPlaceHolder(tr("interview.job_placeholder")) })
	jobEntry.SetMinRowsVisible(7)
	jobEntry.Wrapping = fyne.TextWrapBreak
	information := widget.NewLabel("")
	bindings.setLabel(information, "interview.job_info", nil)
	information.Wrapping = fyne.TextWrapBreak
	cancel := widget.NewButton("", window.Close)
	bindings.setButton(cancel, "common.cancel", nil)
	start := widget.NewButton("", func() {
		if err := startInterviewAssistant(nameEntry.Text, jobEntry.Text); err != nil {
			showLocalizedError(err, window)
			return
		}
		window.Close()
	})
	bindings.setButton(start, "common.start", nil)
	nameLabel, jobLabel := widget.NewLabel(""), widget.NewLabel("")
	bindings.setLabel(nameLabel, "record.session_name", nil)
	bindings.setLabel(jobLabel, "interview.job_label", nil)
	content := container.NewPadded(container.NewBorder(
		container.NewVBox(nameLabel, nameEntry, jobLabel),
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
		bindings.stop()
		activeInterview.Lock()
		if activeInterview.setup == window {
			activeInterview.setup = nil
		}
		activeInterview.Unlock()
	})
	window.Show()
	bindings.start()
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
	jobDescription, err := validateInterviewContext(jobDescription, &localizedText{key: "reference.job"})
	if err != nil {
		return err
	}
	setting, err := currentAppSetting()
	if err != nil {
		return err
	}
	if !setting.speakerSelected {
		return errors.New(tr("interview.speaker_required"))
	}
	if strings.TrimSpace(setting.proxyAddr) == "" {
		return errors.New(tr("interview.proxy_required"))
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
		return localizedMessageError("interview.model_read_failed", nil, "read model configuration failed", err)
	}
	if err := setting.modelConfig.validate(); err != nil {
		return localizedMessageError("interview.model_setup_required", nil, "model configuration is incomplete", err)
	}
	debugLogger, debugLogFile, err := openInterviewDebugLogger(setting.modelConfigDir)
	if err != nil {
		return err
	}
	model, err := newChatCompletionModel(setting.modelConfig)
	if err != nil {
		closeInterviewDebugLog(debugLogger, debugLogFile)
		return localizedMessageError("interview.model_invalid", nil, "invalid interview model configuration", err)
	}
	model.debugLog = debugLogger
	assistant := newInterviewAssistant(model, setting.modelConfig)
	if assistant == nil {
		closeInterviewDebugLog(debugLogger, debugLogFile)
		return errors.New(trData("interview.active_task", map[string]any{"Task": voiceTaskDisplayName(activeVoiceTask())}))
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
			go assistant.failStartup(localizedMessageError(
				"interview.source_start_failed", map[string]any{"Name": fmt.Sprintf("%q", capture.deviceName)},
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
		locale:        &localeBindings{},
	}
}

func (s *interviewAssistant) buildContent() {
	info := s.session.Snapshot()

	s.status = widget.NewLabel("")
	s.locale.setLabel(s.status, "interview.connecting", nil)
	s.status.Importance = widget.SuccessImportance
	s.duration = newSessionDurationView(widget.NewLabel(""), info.CreatedAt)
	s.pauseButton = widget.NewButton("", s.togglePause)
	s.locale.setButton(s.pauseButton, "speech.pause", nil)
	s.stopButton = widget.NewButtonWithIcon("", mytheme.ResEnd, s.shutdown)
	s.locale.setButton(s.stopButton, "speech.stop_save", nil)
	toolbar := container.NewHBox(
		s.status,
		layout.NewSpacer(),
		s.duration.label,
		s.pauseButton,
		s.stopButton,
	)

	s.emptyTranscript = widget.NewLabel("")
	s.locale.setLabel(s.emptyTranscript, "interview.waiting_question", nil)
	s.emptyTranscript.Importance = widget.LowImportance
	s.transcript = container.NewVBox(container.NewCenter(s.emptyTranscript))
	s.transcriptScroll = container.NewVScroll(s.transcript)
	s.emptyAnswers = widget.NewLabel("")
	s.locale.setLabel(s.emptyAnswers, "interview.waiting_answers", nil)
	s.emptyAnswers.Wrapping = fyne.TextWrapBreak
	s.emptyAnswers.Importance = widget.LowImportance
	s.answerList = container.NewVBox(container.NewCenter(s.emptyAnswers))
	s.answerScroll = container.NewVScroll(s.answerList)

	transcriptHeader := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	answerHeader := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	s.locale.setLabel(transcriptHeader, "interview.transcript", nil)
	s.locale.setLabel(answerHeader, "interview.answers", nil)
	left := container.NewBorder(container.NewPadded(transcriptHeader), nil, nil, nil, s.transcriptScroll)
	right := container.NewBorder(container.NewPadded(answerHeader), nil, nil, nil, s.answerScroll)
	split := container.NewHSplit(left, right)
	split.Offset = 0.43
	content := container.NewBorder(container.NewPadded(toolbar), nil, nil, nil, split)
	s.window = fyne.CurrentApp().NewWindow(tr("menu.interview"))
	s.locale.add(func() { s.window.SetTitle(tr("menu.interview")) })
	s.window.SetContent(content)
	s.window.Resize(fyne.NewSize(1000, 680))
	s.window.CenterOnScreen()
	s.window.SetCloseIntercept(s.shutdown)
	s.window.SetOnClosed(s.release)
	s.window.Show()
	s.locale.start()
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
	generate := widget.NewButton("", func() {
		if s.running.Load() && !s.stopping.Load() {
			s.rows[index].generate.Hide()
			s.locale.setLabel(s.rows[index].status, "interview.manual", nil)
			s.enqueueQuestion(index)
		}
	})
	s.locale.setButton(generate, "interview.generate", nil)
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
		s.locale.clearLabel(row.status)
		return
	}
	s.locale.setLabel(row.status, "speech.recognizing", nil)
}

func (s *interviewAssistant) queueQuestionCheck(index int) {
	row := s.rows[index]
	s.locale.setLabel(row.status, "interview.checking", nil)
	check := interviewQuestionCheck{
		turnIndex: index,
		messages:  buildInterviewQuestionMessages(s.turns[:index+1]),
	}
	select {
	case s.checkQueue <- check:
	default:
		s.locale.setLabel(row.status, "interview.check_queue_full", nil)
		row.generate.Show()
		s.setStatus("interview.check_queue_full_help", nil, widget.WarningImportance)
		s.debugf("question check rejected reason=queue_full")
	}
}

func (s *interviewAssistant) applyQuestionDecision(index int, decision interviewQuestionDecision, err error) {
	row := s.rows[index]
	switch {
	case err != nil:
		s.locale.setLabel(row.status, "interview.check_failed", nil)
		row.generate.Show()
		s.setStatus("interview.check_error", map[string]any{"Error": err}, widget.DangerImportance)
	case decision == interviewQuestionYes:
		if s.hasAnswerTurn && index < s.latestAnswerTurn {
			s.locale.setLabel(row.status, "interview.older_question", nil)
			row.generate.Show()
			return
		}
		s.locale.clearLabel(row.status)
		s.enqueueQuestion(index)
	case decision == interviewQuestionNo:
		s.locale.setLabel(row.status, "interview.not_question", nil)
		row.generate.Show()
	default:
		s.locale.setLabel(row.status, "interview.unknown", nil)
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
					err = localizedMessageError(
						"interview.question_timeout", map[string]any{"Duration": interviewQuestionTimeout},
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
		status:    widget.NewLabel(""),
		answer:    widget.NewLabel(""),
		error:     widget.NewLabel(""),
	}
	s.locale.setLabel(answer.status, "interview.queued", nil)
	s.locale.setLabel(answer.answer, "interview.waiting_generation", nil)
	answer.answer.Wrapping = fyne.TextWrapBreak
	answer.answer.Selectable = true
	answer.error.Wrapping = fyne.TextWrapBreak
	answer.error.Importance = widget.DangerImportance
	questionLabel := widget.NewLabel(turn.content)
	questionLabel.Wrapping = fyne.TextWrapBreak
	questionLabel.Selectable = true
	hintLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	s.locale.setLabel(hintLabel, "interview.hint", nil)
	header := container.NewHBox(
		hintLabel,
		layout.NewSpacer(),
		answer.status,
	)
	card := widget.NewCard(
		tr("interview.question"),
		turn.timestamp,
		container.NewVBox(questionLabel, widget.NewSeparator(), header, answer.answer, answer.error),
	)
	s.locale.add(func() { card.SetTitle(tr("interview.question")) })
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
		s.locale.setLabel(answer.status, "interview.queue_full", nil)
		s.locale.setLabel(answer.error, "interview.queue_full_help", nil)
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
	answer.generating = false
	s.locale.setLabel(answer.status, "interview.superseded", nil)
	answer.status.Importance = widget.WarningImportance
	if answer.content != "" && !answer.saved {
		answer.errorText = "新问题到达，回答未完成"
		s.locale.setLabel(answer.error, "interview.superseded_help", nil)
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
						s.locale.setLabel(question.answer.status, "interview.requesting", nil)
						s.locale.setLabel(question.answer.answer, "interview.connecting_model", nil)
						s.setStatus("interview.requesting_advice", nil, widget.SuccessImportance)
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
						s.locale.setLabel(question.answer.status, "interview.thinking", nil)
						question.answer.status.Importance = widget.WarningImportance
						s.locale.setLabel(question.answer.answer, "interview.thinking_help", nil)
						s.setStatus("interview.model_thinking", nil, widget.SuccessImportance)
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
							question.answer.generating = true
							s.locale.setLabel(question.answer.status, "interview.generating", nil)
							s.locale.forgetLabel(question.answer.answer)
							question.answer.status.Importance = widget.SuccessImportance
							s.setStatus("interview.generating_advice", nil, widget.SuccessImportance)
						}
						question.answer.content += token
						question.answer.answer.SetText(question.answer.content)
						s.answerScroll.ScrollToBottom()
					})
				})
				cancel()
				s.finishAnswer(question.answer)
				if errors.Is(err, context.DeadlineExceeded) {
					err = localizedMessageError(
						"interview.answer_timeout", map[string]any{"Duration": interviewAnswerTimeout},
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
						question.answer.generating = false
						question.answer.errorText = err.Error()
						s.locale.setLabel(question.answer.status, "interview.generation_failed", nil)
						s.locale.setLabel(question.answer.error, "common.error", map[string]any{"Error": err})
						question.answer.status.Importance = widget.DangerImportance
						s.setStatus("interview.model_failed", nil, widget.DangerImportance)
					default:
						question.answer.generating = false
						s.locale.setLabel(question.answer.status, "interview.completed", nil)
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

func (s *interviewAssistant) saveGeneratingAnswers() {
	for _, answer := range s.answers {
		if answer.saved || !answer.generating || answer.content == "" {
			continue
		}
		answer.generating = false
		s.locale.setLabel(answer.status, "interview.interrupted", nil)
		answer.errorText = "结束时生成被中断"
		s.appendAnswerToSession(answer)
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
			s.setStatus("interview.capture_error", map[string]any{"Error": err}, widget.DangerImportance)
		}
		if s.pauseButton != nil {
			s.pauseButton.Disable()
		}
	})
}

func (s *interviewAssistant) setStatus(key string, data any, importance widget.Importance) {
	s.locale.setLabel(s.status, key, data)
	s.status.Importance = importance
	s.status.Refresh()
}

func (s *interviewAssistant) setListeningStatus() {
	s.runMu.Lock()
	runErr := s.runErr
	s.runMu.Unlock()
	switch {
	case runErr != nil:
		s.setStatus("interview.capture_error", map[string]any{"Error": runErr}, widget.DangerImportance)
	case s.paused:
		s.setStatus("common.paused", nil, widget.WarningImportance)
	default:
		s.setStatus("speech.listening", nil, widget.SuccessImportance)
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
		s.setStatus("speech.pausing", nil, widget.WarningImportance)
	} else {
		s.setStatus("speech.resuming", nil, widget.SuccessImportance)
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
				s.setStatus("interview.action_failed", map[string]any{"Error": pauseErr}, widget.DangerImportance)
				s.pauseButton.Disable()
				if s.stopRequested {
					s.stopRequested = false
					s.shutdown()
				}
				return
			}
			s.paused = pause
			if pause {
				s.locale.setButton(s.pauseButton, "speech.resume", nil)
				s.duration.Pause()
				s.setStatus("common.paused", nil, widget.WarningImportance)
			} else {
				s.locale.setButton(s.pauseButton, "speech.pause", nil)
				s.duration.Resume()
				s.setStatus("speech.listening", nil, widget.SuccessImportance)
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
		s.setStatus("speech.saving", nil, widget.MediumImportance)
		s.pauseButton.Disable()
		s.stopButton.Disable()
		s.duration.Stop()
		go func() {
			stopASRSoundCaps(s.captures)
			s.captureWG.Wait()
			s.answerWG.Wait()
			var entries []SessionTranscriptEntry
			fyne.DoAndWait(func() {
				s.saveGeneratingAnswers()
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
			showLocalizedError(err, currentMainWin.win)
		}
	})
}

func (s *interviewAssistant) release() {
	s.releaseOnce.Do(func() {
		s.locale.stop()
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
