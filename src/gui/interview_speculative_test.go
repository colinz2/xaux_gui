package gui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

type parallelInterviewModel struct {
	classificationStarted chan struct{}
	answerStarted         chan struct{}
	answerBuffered        chan struct{}
	answerStopped         chan struct{}
	decision              chan struct{}
	finishAnswer          chan struct{}
	classification        string
	classificationErr     error
	answerErr             error
}

func (m *parallelInterviewModel) streamAnswer(ctx context.Context, messages []interviewChatMessage, _ func(), onToken func(string)) error {
	if strings.HasPrefix(messages[0].content, "判断当前面试官发言") {
		close(m.classificationStarted)
		select {
		case <-m.decision:
			if m.classificationErr != nil {
				return m.classificationErr
			}
			onToken(m.classification)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(m.answerStarted)
	defer close(m.answerStopped)
	onToken("先到的回答")
	close(m.answerBuffered)
	select {
	case <-m.finishAnswer:
		onToken("后续回答")
		return m.answerErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitInterviewSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestInterviewSpeculativeAnswerWaitsForClassification(t *testing.T) {
	tests := []struct {
		name                string
		decision            string
		classificationErr   error
		completeBeforeCheck bool
		answerErr           error
		wantAnswer          bool
		wantStatus          string
	}{
		{name: "question while streaming", decision: "QUESTION", wantAnswer: true},
		{name: "question after completion", decision: "QUESTION", completeBeforeCheck: true, wantAnswer: true},
		{name: "question with answer error", decision: "QUESTION", completeBeforeCheck: true, answerErr: errors.New("answer failed"), wantAnswer: true},
		{name: "non-question", decision: "OTHER", wantStatus: "非提问"},
		{name: "uncertain", decision: "UNCERTAIN", wantStatus: "无法判断"},
		{name: "classification error", classificationErr: errors.New("check failed"), wantStatus: "判断失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fynetest.NewApp()
			defer app.Quit()
			ctx, cancel := context.WithCancel(context.Background())
			model := &parallelInterviewModel{
				classificationStarted: make(chan struct{}),
				answerStarted:         make(chan struct{}),
				answerBuffered:        make(chan struct{}),
				answerStopped:         make(chan struct{}),
				decision:              make(chan struct{}),
				finishAnswer:          make(chan struct{}),
				classification:        tt.decision,
				classificationErr:     tt.classificationErr,
				answerErr:             tt.answerErr,
			}
			assistant := &interviewAssistant{
				ctx:           ctx,
				model:         model,
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
			assistant.applyASRResult(speaker, &x.AllResponse{
				Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "请介绍一下自己"},
			})
			assistant.startQuestionCheckWorker()
			defer func() {
				cancel()
				assistant.answerWG.Wait()
			}()

			waitInterviewSignal(t, model.classificationStarted, "classification")
			waitInterviewSignal(t, model.answerStarted, "parallel answer")
			waitInterviewSignal(t, model.answerBuffered, "buffered answer")
			if len(assistant.answers) != 0 || len(assistant.questionQueue) != 0 || len(assistant.savedEntries) != 1 {
				t.Fatal("unconfirmed answer was displayed, queued, or saved")
			}
			if tt.completeBeforeCheck {
				close(model.finishAnswer)
				waitInterviewSignal(t, model.answerStopped, "answer completion")
			}
			close(model.decision)

			if !tt.wantAnswer {
				waitInterviewSignal(t, model.answerStopped, "discarded answer cancellation")
				if len(assistant.answers) != 0 || len(assistant.questionQueue) != 0 ||
					len(assistant.savedEntries) != 1 || assistant.rows[0].status.Text != tt.wantStatus ||
					!assistant.rows[0].generate.Visible() {
					t.Fatal("unconfirmed answer was not discarded or manual override was lost")
				}
				return
			}

			var question interviewQuestion
			select {
			case question = <-assistant.questionQueue:
			case <-time.After(5 * time.Second):
				t.Fatal("confirmed question did not produce an answer")
			}
			if question.speculative == nil || question.answer.content != "" {
				t.Fatal("confirmed question did not retain the buffered answer")
			}
			tokens := make(chan string, 2)
			done := make(chan error, 1)
			go func() {
				done <- question.speculative.streamAnswer(ctx, nil, nil, func(token string) {
					tokens <- token
				})
			}()
			select {
			case token := <-tokens:
				if token != "先到的回答" {
					t.Fatalf("first buffered token = %q", token)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("buffered answer was not released")
			}
			if !tt.completeBeforeCheck {
				close(model.finishAnswer)
			}
			select {
			case token := <-tokens:
				if token != "后续回答" {
					t.Fatalf("second answer token = %q", token)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("subsequent answer was not streamed")
			}
			select {
			case err := <-done:
				if !errors.Is(err, tt.answerErr) {
					t.Fatalf("answer error = %v, want %v", err, tt.answerErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("answer stream did not complete")
			}
			question.speculative.cancel()
		})
	}
}

type interviewFinishedLog struct {
	finished chan struct{}
}

func (l *interviewFinishedLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "answer worker finished") {
		close(l.finished)
	}
	return len(p), nil
}

func TestInterviewSpeculativeAnswerDisplayedAndSaved(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	ctx, cancel := context.WithCancel(context.Background())
	model := &parallelInterviewModel{
		classificationStarted: make(chan struct{}),
		answerStarted:         make(chan struct{}),
		answerBuffered:        make(chan struct{}),
		answerStopped:         make(chan struct{}),
		decision:              make(chan struct{}),
		finishAnswer:          make(chan struct{}),
		classification:        "QUESTION",
	}
	logSignal := &interviewFinishedLog{finished: make(chan struct{})}
	var logs bytes.Buffer
	assistant := &interviewAssistant{
		ctx:           ctx,
		model:         model,
		checkQueue:    make(chan interviewQuestionCheck, 1),
		questionQueue: make(chan interviewQuestion, 1),
		transcript:    container.NewVBox(),
		answerList:    container.NewVBox(),
		status:        widget.NewLabel(""),
		debugLog:      log.New(io.MultiWriter(logSignal, &logs), "", 0),
		traceSession:  42,
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "扬声器", resultIndex: -1, writeListIndex: -1}
	assistant.applyASRResult(speaker, &x.AllResponse{
		Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "请介绍一下自己"},
	})
	assistant.startAnswerWorker()
	defer func() {
		cancel()
		assistant.answerWG.Wait()
	}()
	waitInterviewSignal(t, model.answerBuffered, "buffered answer")
	if len(assistant.answers) != 0 {
		t.Fatal("answer was displayed before classification")
	}
	close(model.finishAnswer)
	waitInterviewSignal(t, model.answerStopped, "answer completion")
	close(model.decision)
	waitInterviewSignal(t, logSignal.finished, "answer worker completion")
	cancel()
	assistant.answerWG.Wait()
	if len(assistant.answers) != 1 || assistant.answers[0].content != "先到的回答后续回答" ||
		assistant.answers[0].answer.Text != "先到的回答后续回答" ||
		!assistant.answers[0].saved || len(assistant.savedEntries) != 2 {
		t.Fatalf("confirmed buffered answer was not displayed and saved: %+v", assistant.answers)
	}
	for _, entry := range []string{
		"asr applied session=42 turn=0", "question check queued session=42 turn=0",
		"question check and speculative answer started session=42 turn=0",
		"speculative answer buffered session=42 turn=0",
		"question check completed session=42 turn=0", "question decision ui session=42 turn=0",
		"question queued session=42 turn=0 id=1", "answer worker started session=42 turn=0 id=1",
		"answer ui chunk applied session=42 turn=0 id=1 chunk=1 first=true",
		"final_to_ui_ms=", "answer worker finished session=42 turn=0 id=1",
	} {
		if !strings.Contains(logs.String(), entry) {
			t.Errorf("missing timing event %q in %s", entry, logs.String())
		}
	}
	if strings.Contains(logs.String(), "请介绍一下自己") || strings.Contains(logs.String(), "先到的回答") {
		t.Fatal("timing log contains transcript or answer text")
	}
}

func TestSupersededQueuedSpeculativeAnswerIsCanceled(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	ctx, cancel := context.WithCancel(context.Background())
	model := &parallelInterviewModel{
		answerStarted:  make(chan struct{}),
		answerBuffered: make(chan struct{}),
		answerStopped:  make(chan struct{}),
		finishAnswer:   make(chan struct{}),
	}
	assistant := &interviewAssistant{
		ctx:           ctx,
		model:         model,
		questionQueue: make(chan interviewQuestion, 1),
	}
	defer func() {
		cancel()
		assistant.answerWG.Wait()
	}()
	speculative := assistant.startSpeculativeAnswer([]interviewChatMessage{{role: "user", content: "问题"}})
	waitInterviewSignal(t, model.answerBuffered, "buffered answer")
	answer := &interviewAnswer{status: widget.NewLabel("")}
	assistant.questionQueue <- interviewQuestion{answer: answer, speculative: speculative}
	assistant.supersedeQueuedAnswers()
	waitInterviewSignal(t, model.answerStopped, "superseded answer cancellation")
	if !answer.superseded.Load() || len(assistant.questionQueue) != 0 {
		t.Fatal("superseded answer was not canceled and removed")
	}
}
