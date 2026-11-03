package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

type sequenceQuestionCheckModel struct {
	calls   int
	blocked chan struct{}
}

func (m *sequenceQuestionCheckModel) streamAnswer(ctx context.Context, messages []interviewChatMessage, _ func(), onToken func(string)) error {
	if !strings.HasPrefix(messages[0].content, "判断当前面试官发言") {
		<-ctx.Done()
		return ctx.Err()
	}
	m.calls++
	switch m.calls {
	case 1:
		onToken("OTHER")
	case 2:
		onToken("QUESTION")
	default:
		close(m.blocked)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func TestInterviewAssistantListeningStatusReflectsPauseAndCaptureError(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	assistant := &interviewAssistant{
		status: widget.NewLabel(""),
		paused: true,
	}
	assistant.setListeningStatus()
	if assistant.status.Text != "已暂停" {
		t.Fatalf("status while paused = %q, want 已暂停", assistant.status.Text)
	}

	assistant.paused = false
	assistant.runMu.Lock()
	assistant.runErr = errors.New("capture stopped")
	assistant.runMu.Unlock()
	assistant.setListeningStatus()
	if assistant.status.Text != "音频识别异常：capture stopped" {
		t.Fatalf("status after capture error = %q", assistant.status.Text)
	}
}

func TestInterviewAssistantAnswersFinalSpeakerTurnsWithConversationContext(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	directory := t.TempDir()
	writeKnowledgeFile(t, directory, "answer.md", "并发时关注锁竞争")
	knowledge, err := loadInterviewKnowledge(true, directory)
	if err != nil {
		t.Fatal(err)
	}
	assistant := &interviewAssistant{
		modelConfig:   modelConfig{SystemPrompt: "自定义系统提示词"},
		reference:     interviewContext{jobDescription: "Go 后端职位", resume: "项目使用消息队列", knowledge: knowledge},
		questionQueue: make(chan interviewQuestion, 4),
		checkQueue:    make(chan interviewQuestionCheck, 4),
		transcript:    container.NewVBox(),
		answerList:    container.NewVBox(),
		status:        widget.NewLabel(""),
	}

	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}
	microphone := &asrSoundCap{
		deviceName:     "麦克风",
		isMicrophone:   true,
		resultIndex:    -1,
		writeListIndex: -1,
	}

	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeRecognizing,
		Result: x.RecognizeResult{
			Index: 0, Result: "请介绍一下自己", Interim: true,
		},
	})
	if len(assistant.questionQueue) != 0 {
		t.Fatal("interim speaker transcript triggered a model request")
	}
	assistant.applyASRResult(speaker, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 0, Result: "请介绍一下自己"},
	})
	if len(assistant.checkQueue) != 1 || len(assistant.questionQueue) != 0 || len(assistant.answers) != 0 {
		t.Fatal("final speaker transcript should be checked before creating an answer")
	}
	firstCheck := <-assistant.checkQueue
	assistant.applyQuestionDecision(firstCheck.turnIndex, interviewQuestionYes, nil)
	if len(assistant.questionQueue) != 1 {
		t.Fatalf("confirmed question triggered %d answer requests, want 1", len(assistant.questionQueue))
	}
	activeCtx, cancelActive := context.WithCancel(context.Background())
	assistant.answerMu.Lock()
	assistant.activeAnswer = assistant.answers[0]
	assistant.answerCancel = cancelActive
	assistant.answerMu.Unlock()
	assistant.applyASRResult(microphone, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 0, Result: "我有五年 Go 开发经验。"},
	})
	if len(assistant.questionQueue) != 1 {
		t.Fatal("microphone final transcript should only update context")
	}
	assistant.applyASRResult(speaker, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 1, Result: "请举一个并发项目的例子"},
	})
	if len(assistant.checkQueue) != 1 {
		t.Fatal("new speaker transcript was not queued for classification")
	}
	secondCheck := <-assistant.checkQueue
	assistant.applyQuestionDecision(secondCheck.turnIndex, interviewQuestionYes, nil)
	if len(assistant.questionQueue) != 1 {
		t.Fatalf("latest speaker final left %d queued model requests, want only 1", len(assistant.questionQueue))
	}
	second := <-assistant.questionQueue
	if assistant.answers[0].status.Text != "已由新问题替代" {
		t.Fatalf("stale queued answer status = %q", assistant.answers[0].status.Text)
	}
	if !errors.Is(activeCtx.Err(), context.Canceled) {
		t.Fatalf("old active model request was not canceled: %v", activeCtx.Err())
	}
	if len(second.messages) != 4 ||
		second.messages[0].role != "system" ||
		!strings.HasPrefix(second.messages[0].content, "自定义系统提示词\n\n") ||
		second.messages[1].role != "user" ||
		!strings.Contains(second.messages[1].content, "Go 后端职位") ||
		!strings.Contains(second.messages[1].content, "项目使用消息队列") ||
		!strings.Contains(second.messages[1].content, "并发时关注锁竞争") ||
		second.messages[2].role != "user" ||
		!strings.Contains(second.messages[2].content, "候选人：我有五年 Go 开发经验。") ||
		second.messages[3].role != "user" ||
		second.messages[3].content != "当前面试官问题：请举一个并发项目的例子" {
		t.Fatalf("second model request has unexpected conversation context: %+v", second.messages)
	}
	if len(assistant.savedEntries) != 3 {
		t.Fatalf("saved %d transcript turns, want 3", len(assistant.savedEntries))
	}
	answer := &interviewAnswer{
		question: "请举一个并发项目的例子",
		content:  "我会先说明项目背景和结果。",
	}
	assistant.appendAnswerToSession(answer)
	if !answer.saved || len(assistant.savedEntries) != 4 ||
		assistant.savedEntries[3].Device != "AI 建议" ||
		!strings.Contains(assistant.savedEntries[3].Content, answer.content) {
		t.Fatalf("AI suggestion was not added to the session transcript: %+v", assistant.savedEntries)
	}
}

func TestInterviewAssistantNonQuestionAndFailedCheckDoNotInterruptAnswer(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	activeCtx, cancelActive := context.WithCancel(context.Background())
	defer cancelActive()
	assistant := &interviewAssistant{
		checkQueue:    make(chan interviewQuestionCheck, 2),
		questionQueue: make(chan interviewQuestion, 1),
		transcript:    container.NewVBox(),
		answerList:    container.NewVBox(),
		status:        widget.NewLabel(""),
		activeAnswer:  &interviewAnswer{status: widget.NewLabel("生成中")},
		answerCancel:  cancelActive,
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}
	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "好的，我了解了。"},
	})
	check := <-assistant.checkQueue
	assistant.applyQuestionDecision(check.turnIndex, interviewQuestionNo, nil)
	if len(assistant.answers) != 0 || len(assistant.questionQueue) != 0 || activeCtx.Err() != nil {
		t.Fatal("non-question created an answer or interrupted the active answer")
	}
	if assistant.rows[0].status.Text != "非提问" || !assistant.rows[0].generate.Visible() ||
		len(assistant.savedEntries) != 1 {
		t.Fatal("non-question must remain in the transcript with a manual override")
	}

	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 1, Result: "接下来呢"},
	})
	check = <-assistant.checkQueue
	assistant.applyQuestionDecision(check.turnIndex, interviewQuestionUncertain, errors.New("服务不可用"))
	if assistant.rows[1].status.Text != "判断失败" ||
		!strings.Contains(assistant.status.Text, "服务不可用") ||
		!assistant.rows[1].generate.Visible() || activeCtx.Err() != nil {
		t.Fatal("classification error was not surfaced or interrupted the active answer")
	}
	assistant.rows[1].generate.OnTapped()
	if len(assistant.answers) != 1 || len(assistant.questionQueue) != 1 ||
		!errors.Is(activeCtx.Err(), context.Canceled) {
		t.Fatal("manual override did not generate a suggestion and supersede the active answer")
	}
}

func TestInterviewAssistantUncertainAndFullCheckQueueAllowManualOverride(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	assistant := &interviewAssistant{
		checkQueue:    make(chan interviewQuestionCheck, 1),
		questionQueue: make(chan interviewQuestion, 1),
		transcript:    container.NewVBox(),
		answerList:    container.NewVBox(),
		status:        widget.NewLabel(""),
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}
	for i, text := range []string{"这部分", "另一个话题"} {
		assistant.applyASRResult(speaker, &x.AllResponse{
			Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: i, Result: text},
		})
	}
	if assistant.rows[1].status.Text != "判断队列已满" || !assistant.rows[1].generate.Visible() {
		t.Fatal("full classification queue did not expose a manual override")
	}
	check := <-assistant.checkQueue
	assistant.applyQuestionDecision(check.turnIndex, interviewQuestionUncertain, nil)
	if assistant.rows[0].status.Text != "无法判断" || !assistant.rows[0].generate.Visible() ||
		len(assistant.answers) != 0 {
		t.Fatal("uncertain result should not create an answer and should allow manual override")
	}
	assistant.rows[1].generate.OnTapped()
	assistant.applyQuestionDecision(0, interviewQuestionYes, nil)
	if len(assistant.answers) != 1 || assistant.rows[0].status.Text != "较早问题" ||
		!assistant.rows[0].generate.Visible() {
		t.Fatal("an older delayed classification must not replace a newer manually requested answer")
	}
}

func TestInterviewQuestionWorkerProcessesTurnsInOrderAndStops(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	ctx, cancel := context.WithCancel(context.Background())
	model := &sequenceQuestionCheckModel{blocked: make(chan struct{})}
	assistant := &interviewAssistant{
		ctx:           ctx,
		model:         model,
		checkQueue:    make(chan interviewQuestionCheck, 3),
		questionQueue: make(chan interviewQuestion, 1),
		transcript:    container.NewVBox(),
		answerList:    container.NewVBox(),
		status:        widget.NewLabel(""),
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}
	for i, text := range []string{"好的", "请介绍一下自己", "接下来呢"} {
		assistant.applyASRResult(speaker, &x.AllResponse{
			Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: i, Result: text},
		})
	}
	assistant.startQuestionCheckWorker()
	defer func() {
		cancel()
		assistant.answerWG.Wait()
	}()
	select {
	case <-model.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("classification worker did not process queued turns")
	}
	cancel()
	assistant.answerWG.Wait()
	if assistant.rows[0].status.Text != "非提问" || len(assistant.answers) != 1 ||
		assistant.answers[0].question != "请介绍一下自己" || len(assistant.questionQueue) != 1 ||
		len(assistant.savedEntries) != 3 {
		t.Fatal("worker should preserve all transcripts and create an answer only for the confirmed question")
	}
}

func TestInterviewAssistantKeepsFinalAndIncompleteTurnsWhenStopping(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	transcript := container.NewVBox()
	assistant := &interviewAssistant{
		questionQueue:    make(chan interviewQuestion, 1),
		transcript:       transcript,
		transcriptScroll: container.NewVScroll(transcript),
	}
	assistant.running.Store(true)
	assistant.stopping.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}

	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeRecognizing,
		Result: x.RecognizeResult{
			Index: 0, Result: "最后一句尚未完整转写", Interim: true,
		},
	})
	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeSentenceEnd,
		Result: x.RecognizeResult{
			Index: 0, Interim: true,
		},
	})
	if len(assistant.savedEntries) != 1 ||
		assistant.savedEntries[0].Content != "最后一句尚未完整转写" ||
		!assistant.turns[0].final {
		t.Fatalf("final ASR result was not preserved while stopping: turns=%+v entries=%+v",
			assistant.turns, assistant.savedEntries)
	}
	if len(assistant.questionQueue) != 0 {
		t.Fatal("stopping session queued a new model request")
	}

	microphone := &asrSoundCap{
		deviceName:     "麦克风",
		isMicrophone:   true,
		resultIndex:    -1,
		writeListIndex: -1,
	}
	assistant.applyASRResult(microphone, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "候选人正在回答", Interim: true},
	})
	assistant.saveIncompleteTurns()
	if len(assistant.savedEntries) != 2 ||
		assistant.savedEntries[1].Device != "麦克风（未完成）" ||
		assistant.savedEntries[1].Content != "候选人正在回答" {
		t.Fatalf("unfinished turn was not saved for review: %+v", assistant.savedEntries)
	}
}

func TestInterviewAssistantSupersedesActiveAndQueuedAnswers(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	activeCtx, cancelActive := context.WithCancel(context.Background())
	activeAnswer := &interviewAnswer{status: widget.NewLabel("生成中")}
	queuedAnswer := &interviewAnswer{status: widget.NewLabel("排队中")}
	assistant := &interviewAssistant{
		ctx:           parentCtx,
		questionQueue: make(chan interviewQuestion, 1),
		activeAnswer:  activeAnswer,
		answerCancel:  cancelActive,
	}
	assistant.questionQueue <- interviewQuestion{answer: queuedAnswer}

	assistant.answerVersion.Add(1)
	assistant.cancelActiveAnswer()
	assistant.supersedeQueuedAnswers()

	if !activeAnswer.superseded.Load() || !queuedAnswer.superseded.Load() {
		t.Fatal("older active and queued answers were not marked superseded")
	}
	if !errors.Is(activeCtx.Err(), context.Canceled) {
		t.Fatalf("active answer was not canceled: %v", activeCtx.Err())
	}
	if queuedAnswer.status.Text != "已由新问题替代" || len(assistant.questionQueue) != 0 {
		t.Fatalf("queued answer was not replaced: status=%q queue=%d",
			queuedAnswer.status.Text, len(assistant.questionQueue))
	}

	_, cancelStale := context.WithCancel(context.Background())
	defer cancelStale()
	if assistant.beginAnswer(interviewQuestion{generation: 0}, cancelStale) {
		t.Fatal("stale question was allowed to start")
	}
}

func TestInterviewAnswerIsSavedInInterviewSessionTranscript(t *testing.T) {
	root := t.TempDir()
	session, err := CreateSession(root, "面试会话", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	assistant := &interviewAssistant{}
	assistant.appendAnswerToSession(&interviewAnswer{
		question: "如何处理并发？",
		content:  "使用 context 管理任务生命周期。",
	})
	if err := session.Finish(SessionStatusStopped, assistant.savedEntries, nil); err != nil {
		t.Fatal(err)
	}
	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Kind != SessionKindInterview {
		t.Fatalf("unexpected saved sessions: %+v", sessions)
	}
	transcript, err := os.ReadFile(filepath.Join(sessions[0].Directory, sessions[0].TranscriptFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "[AI 建议]") ||
		!strings.Contains(string(transcript), "context 管理任务生命周期") {
		t.Fatalf("saved transcript does not contain the AI answer: %s", transcript)
	}
	if got := sessionKindText(sessions[0].Kind); got != "面试" {
		t.Fatalf("session kind label = %q", got)
	}
}
