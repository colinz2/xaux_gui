package gui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

type serializedInterviewDriver struct {
	fyne.Driver
	mu sync.Mutex
}

func (d *serializedInterviewDriver) DoFromGoroutine(fn func(), _ bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn()
}

type serializedInterviewApp struct {
	fyne.App
	driver fyne.Driver
}

func (a *serializedInterviewApp) Driver() fyne.Driver { return a.driver }

type interimTestStep struct {
	token string
	done  bool
	err   error
	ack   chan struct{}
}

type interimTestRequest struct {
	kind     string
	messages []interviewChatMessage
	steps    chan interimTestStep
	ended    chan struct{}
}

type interimTestModel struct {
	requests chan *interimTestRequest
}

func (m *interimTestModel) streamAnswer(ctx context.Context, messages []interviewChatMessage, _ func(), onToken func(string)) error {
	req := &interimTestRequest{
		kind: requestTraceFromContext(ctx).kind, messages: messages,
		steps: make(chan interimTestStep), ended: make(chan struct{}),
	}
	select {
	case m.requests <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer close(req.ended)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case step := <-req.steps:
			if step.token != "" {
				onToken(step.token)
			}
			if step.ack != nil {
				close(step.ack)
			}
			if step.done {
				return step.err
			}
		}
	}
}

func receiveInterimRequest(t *testing.T, model *interimTestModel) *interimTestRequest {
	t.Helper()
	select {
	case req := <-model.requests:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for model request")
		return nil
	}
}

func sendInterimStep(t *testing.T, req *interimTestRequest, step interimTestStep) {
	t.Helper()
	step.ack = make(chan struct{})
	select {
	case req.steps <- step:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out sending model response")
	}
	waitInterviewSignal(t, step.ack, "model response")
}

func waitInterimUI(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		fyne.DoAndWait(func() { ok = condition() })
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for UI state")
}

func newInterimTestAssistant(t *testing.T) (*interviewAssistant, *interimTestModel, *asrSoundCap) {
	t.Helper()
	app := fynetest.NewApp()
	wrapped := &serializedInterviewApp{App: app, driver: &serializedInterviewDriver{Driver: app.Driver()}}
	fyne.SetCurrentApp(wrapped)
	ctx, cancel := context.WithCancel(context.Background())
	model := &interimTestModel{requests: make(chan *interimTestRequest, 16)}
	assistant := &interviewAssistant{
		ctx: ctx, cancel: cancel, model: model, interimEnabled: true,
		checkQueue: make(chan interviewQuestionCheck, 4), questionQueue: make(chan interviewQuestion, 1),
		transcript: container.NewVBox(), answerList: container.NewVBox(), status: widget.NewLabel(""),
		emptyAnswers: widget.NewLabel("waiting"),
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.answerScroll = container.NewVScroll(assistant.answerList)
	assistant.running.Store(true)
	assistant.startAnswerWorker()
	t.Cleanup(func() {
		assistant.stopping.Store(true)
		cancel()
		assistant.answerWG.Wait()
		fyne.SetCurrentApp(app)
		app.Quit()
	})
	return assistant, model, &asrSoundCap{deviceName: "speaker", resultIndex: -1, writeListIndex: -1}
}

func interimUpdate(assistant *interviewAssistant, speaker *asrSoundCap, index int, text, kind string) {
	fyne.DoAndWait(func() {
		assistant.applyASRResult(speaker, &x.AllResponse{
			Type: kind, Result: x.RecognizeResult{Index: index, Result: text},
		})
	})
}

func TestInterimPredictionRequiresCountAndUnicodeLength(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	for i := 1; i < interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, "足够长度的中文识别文本用于测试"+strings.Repeat("a", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("long text triggered before the count threshold: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "短"+strings.Repeat("a", 13)+"  ", x.TypeRecognizing)
	select {
	case req := <-model.requests:
		t.Fatalf("short text triggered at the count threshold: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "短"+strings.Repeat("a", 14), x.TypeRecognizing)
	check := receiveInterimRequest(t, model)
	if check.kind != "interim_classification" {
		t.Fatalf("unexpected request kind %q", check.kind)
	}
	sendInterimStep(t, check, interimTestStep{token: "OTHER", done: true})
	waitInterimUI(t, func() bool { return !assistant.interimStates[0].checking })
	interimUpdate(assistant, speaker, 0, "短"+strings.Repeat("a", 15), x.TypeRecognizing)
	select {
	case req := <-model.requests:
		t.Fatalf("check did not skip the update after the delayed threshold: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "短"+strings.Repeat("a", 16), x.TypeRecognizing)
	next := receiveInterimRequest(t, model)
	if !strings.Contains(next.messages[1].content, "短"+strings.Repeat("a", 16)) {
		t.Fatal("check after the skipped update did not use the latest transcript")
	}
	sendInterimStep(t, next, interimTestStep{token: "OTHER", done: true})
}

func TestInterimShortUtteranceStillGetsFinalRequests(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	for i := 1; i <= 10; i++ {
		interimUpdate(assistant, speaker, 0, "为什么"+strings.Repeat("a", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("short interim text triggered %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "为什么？", x.TypeSentenceEnd)
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil || !strings.Contains(answer.messages[len(answer.messages)-1].content, "为什么？") {
		t.Fatal("short final transcript did not trigger normal final requests")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "short final answer cancellation")
}

func TestInterimChecksSkipAnUpdateAndCoalesce(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	base := "足够长度的中文识别文本用于测试"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	first := receiveInterimRequest(t, model)
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 9), x.TypeRecognizing)
	sendInterimStep(t, first, interimTestStep{token: "OTHER", done: true})
	waitInterimUI(t, func() bool {
		return !assistant.interimStates[0].checking && assistant.rows[0].content.Text == base+strings.Repeat("a", 9)
	})
	select {
	case req := <-model.requests:
		t.Fatalf("interim check did not skip update 9: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 10), x.TypeRecognizing)
	second := receiveInterimRequest(t, model)
	if !strings.Contains(second.messages[1].content, base+strings.Repeat("a", 10)) {
		t.Fatal("second check did not use update 10")
	}
	for i := 11; i <= 13; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("overlapping interim check was started: %s", req.kind)
	default:
	}
	sendInterimStep(t, second, interimTestStep{token: "OTHER", done: true})
	third := receiveInterimRequest(t, model)
	if !strings.Contains(third.messages[1].content, base+strings.Repeat("a", 13)) {
		t.Fatal("coalesced check did not use the latest update")
	}
	sendInterimStep(t, third, interimTestStep{token: "OTHER", done: true})
	waitInterimUI(t, func() bool { return !assistant.interimStates[0].checking })
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 14), x.TypeRecognizing)
	select {
	case req := <-model.requests:
		t.Fatalf("interim check did not skip update after coalescing: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 15), x.TypeRecognizing)
	fourth := receiveInterimRequest(t, model)
	if !strings.Contains(fourth.messages[1].content, base+strings.Repeat("a", 15)) {
		t.Fatal("next check did not use update 15")
	}
	sendInterimStep(t, fourth, interimTestStep{token: "OTHER", done: true})
}

func TestInterimPredictionThresholdCoalescingAndFreshFinalAnswer(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	base := "请解释 Redis"
	for i := 1; i < interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("request before threshold: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 8), x.TypeRecognizing)
	first := receiveInterimRequest(t, model)
	if first.kind != "interim_classification" || !strings.Contains(first.messages[1].content, base+strings.Repeat("a", 8)) {
		t.Fatalf("unexpected interim check: %+v", first)
	}
	for i := 9; i <= 10; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("interim check was not coalesced: %s", req.kind)
	default:
	}
	sendInterimStep(t, first, interimTestStep{token: "QUESTION", done: true})
	var check, preview *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		switch req.kind {
		case "interim_classification":
			check = req
		case "interim_answer":
			preview = req
		default:
			t.Fatalf("unexpected request kind %q", req.kind)
		}
	}
	if check == nil || preview == nil || !strings.Contains(check.messages[1].content, base+strings.Repeat("a", 10)) ||
		!strings.Contains(preview.messages[len(preview.messages)-1].content, base+strings.Repeat("a", 10)) {
		t.Fatal("coalesced check or preview did not use the latest transcript")
	}
	sendInterimStep(t, preview, interimTestStep{token: "预测回答"})
	var displayed *interviewInterimPreview
	fyne.DoAndWait(func() {
		state := assistant.interimStates[0]
		if state == nil || state.preview == nil || !state.preview.visible ||
			state.preview.answer.Text != "预测回答" || len(assistant.answers) != 0 || len(assistant.savedEntries) != 0 {
			t.Fatal("provisional answer was not displayed separately without saving")
		}
		displayed = state.preview
	})
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 10), x.TypeSentenceEnd)
	waitInterviewSignal(t, check.ended, "canceled interim check")
	waitInterviewSignal(t, preview.ended, "canceled interim answer")
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		switch req.kind {
		case "classification":
			final = req
		case "speculative_answer":
			answer = req
		}
	}
	if final == nil || answer == nil || !strings.Contains(answer.messages[len(answer.messages)-1].content, base+strings.Repeat("a", 10)) {
		t.Fatal("matching transcript did not trigger a fresh final answer")
	}
	sendInterimStep(t, answer, interimTestStep{token: "最终"})
	fyne.DoAndWait(func() {
		if !assistant.hasVisibleInterimPreview() || len(assistant.answers) != 0 {
			t.Fatal("final answer appeared before final classification")
		}
	})
	sendInterimStep(t, final, interimTestStep{token: "QUESTION", done: true})
	sendInterimStep(t, answer, interimTestStep{token: "回答", done: true})
	waitInterimUI(t, func() bool {
		return len(assistant.answers) == 1 && assistant.answers[0].content == "最终回答" && assistant.answers[0].saved
	})
	fyne.DoAndWait(func() {
		position := slices.Index(assistant.answerList.Objects, fyne.CanvasObject(displayed.container))
		if position < 0 || position+2 >= len(assistant.answerList.Objects) {
			t.Fatal("prediction disappeared after final answer")
		}
		card, ok := assistant.answerList.Objects[position+2].(*widget.Card)
		if !ok {
			t.Fatal("final answer card was not placed immediately after the prediction")
		}
		content, ok := card.Content.(*fyne.Container)
		if !ok || len(content.Objects) < 4 || content.Objects[3] != assistant.answers[0].answer ||
			displayed.answer.Text != "预测回答" || assistant.answers[0].answer.Text != "最终回答" ||
			!strings.Contains(displayed.status.Text, "预测") {
			t.Fatal("prediction and final answer were not kept separately in order")
		}
	})
}

func TestInterimFinalAnswerFollowsItsPredictionAcrossTurns(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	base := "请解释缓存系统原理"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	firstPreviewRequest := receiveInterimRequest(t, model)
	sendInterimStep(t, firstPreviewRequest, interimTestStep{token: "预测一"})
	var firstPreview *interviewInterimPreview
	fyne.DoAndWait(func() { firstPreview = assistant.interimStates[0].preview })
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 8), x.TypeSentenceEnd)
	waitInterviewSignal(t, firstPreviewRequest.ended, "first preview cancellation")
	var firstFinal, firstAnswer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			firstFinal = req
		} else if req.kind == "speculative_answer" {
			firstAnswer = req
		}
	}
	if firstFinal == nil || firstAnswer == nil {
		t.Fatal("first final requests were not started")
	}

	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 1, base+strings.Repeat("b", i), x.TypeRecognizing)
	}
	check = receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	secondPreviewRequest := receiveInterimRequest(t, model)
	sendInterimStep(t, secondPreviewRequest, interimTestStep{token: "预测二"})
	var secondPreview *interviewInterimPreview
	fyne.DoAndWait(func() { secondPreview = assistant.interimStates[1].preview })

	sendInterimStep(t, firstFinal, interimTestStep{token: "QUESTION", done: true})
	sendInterimStep(t, firstAnswer, interimTestStep{token: "最终一", done: true})
	waitInterimUI(t, func() bool {
		return len(assistant.answers) == 1 && assistant.answers[0].saved
	})
	fyne.DoAndWait(func() {
		first := slices.Index(assistant.answerList.Objects, fyne.CanvasObject(firstPreview.container))
		second := slices.Index(assistant.answerList.Objects, fyne.CanvasObject(secondPreview.container))
		if first < 0 || second <= first+2 {
			t.Fatal("first final answer was not inserted before the next prediction")
		}
		card, ok := assistant.answerList.Objects[first+2].(*widget.Card)
		if !ok {
			t.Fatal("first final answer does not follow its prediction")
		}
		content, ok := card.Content.(*fyne.Container)
		if !ok || content.Objects[3] != assistant.answers[0].answer {
			t.Fatal("first final answer is attached to the wrong prediction")
		}
	})

	interimUpdate(assistant, speaker, 1, base+strings.Repeat("b", 8), x.TypeSentenceEnd)
	waitInterviewSignal(t, secondPreviewRequest.ended, "second preview cancellation")
	var secondFinal, secondAnswer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			secondFinal = req
		} else if req.kind == "speculative_answer" {
			secondAnswer = req
		}
	}
	if secondFinal == nil || secondAnswer == nil {
		t.Fatal("second final requests were not started")
	}
	sendInterimStep(t, secondFinal, interimTestStep{token: "QUESTION", done: true})
	sendInterimStep(t, secondAnswer, interimTestStep{token: "最终二", done: true})
	waitInterimUI(t, func() bool {
		return len(assistant.answers) == 2 && assistant.answers[1].saved
	})
	fyne.DoAndWait(func() {
		first := slices.Index(assistant.answerList.Objects, fyne.CanvasObject(firstPreview.container))
		second := slices.Index(assistant.answerList.Objects, fyne.CanvasObject(secondPreview.container))
		if first < 0 || second <= first+2 || second+2 >= len(assistant.answerList.Objects) ||
			firstPreview.answer.Text != "预测一" || secondPreview.answer.Text != "预测二" {
			t.Fatal("a prediction was removed or reordered after the next question")
		}
		card, ok := assistant.answerList.Objects[second+2].(*widget.Card)
		if !ok {
			t.Fatal("second final answer does not follow its prediction")
		}
		content, ok := card.Content.(*fyne.Container)
		if !ok || content.Objects[3] != assistant.answers[1].answer {
			t.Fatal("second final answer is attached to the wrong prediction")
		}
	})
}

func TestInterimPredictionChangedFinalAndNonQuestionAreDiscarded(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	base := "请解释缓存原理"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	if preview.kind != "interim_answer" {
		t.Fatalf("preview request kind = %q", preview.kind)
	}
	sendInterimStep(t, preview, interimTestStep{token: "不应保存"})
	interimUpdate(assistant, speaker, 0, base+strings.Repeat("a", 8)+"补充", x.TypeSentenceEnd)
	waitInterviewSignal(t, preview.ended, "outdated preview cancellation")
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		} else {
			t.Fatalf("unexpected final request kind %q", req.kind)
		}
	}
	if final == nil || answer == nil || !strings.Contains(answer.messages[len(answer.messages)-1].content, "补充") {
		t.Fatal("changed final transcript did not trigger a fresh answer")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "non-question answer cancellation")
	fyne.DoAndWait(func() {
		if len(assistant.answers) != 0 || len(assistant.savedEntries) != 1 || assistant.hasVisibleInterimPreview() ||
			assistant.rows[0].status.Text != "非提问" {
			t.Fatal("non-question retained a provisional answer or lost its transcript")
		}
	})
}

func TestInterimPredictionDuplicateUpdatesAndErrors(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	assistant.debugLog = nil
	for i := 0; i < 10; i++ {
		assistant.updateASRResult(speaker, &x.AllResponse{
			Type: x.TypeRecognizing, Result: x.RecognizeResult{Index: 0, Result: "重复内容超过十五个字符用于测试"},
		})
	}
	select {
	case req := <-model.requests:
		t.Fatalf("duplicate ASR updates reached threshold: %s", req.kind)
	default:
	}
	for i := 1; i < interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, fmt.Sprintf("重复内容超过十五个字符用于测试%d", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{err: errors.New("prediction unavailable"), done: true})
	waitInterimUI(t, func() bool { return strings.Contains(assistant.status.Text, "prediction unavailable") })
	interimUpdate(assistant, speaker, 0, "最终陈述", x.TypeSentenceEnd)
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("final classification was not started after interim failure")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "final speculative cancellation")
}

func TestInterimPredictionMatchingNonQuestionDiscardsPreview(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	text := "缓存问题是什么"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	sendInterimStep(t, preview, interimTestStep{token: "预测"})
	interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", 8), x.TypeSentenceEnd)
	waitInterviewSignal(t, preview.ended, "interim preview cancellation")
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("final requests were not started")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "non-question final answer cancellation")
	fyne.DoAndWait(func() {
		if assistant.hasVisibleInterimPreview() || len(assistant.answers) != 0 ||
			len(assistant.savedEntries) != 1 || assistant.rows[0].status.Text != "非提问" {
			t.Fatal("final non-question did not discard provisional content")
		}
	})
}

func TestInterimPredictionDiscardsPreviewOnUncertainOrFailedFinalCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		step interimTestStep
	}{
		{name: "uncertain", step: interimTestStep{token: "UNCERTAIN", done: true}},
		{name: "failed", step: interimTestStep{err: errors.New("final check failed"), done: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assistant, model, speaker := newInterimTestAssistant(t)
			text := "请解释缓存原理"
			for i := 1; i <= interviewInterimThreshold; i++ {
				interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", i), x.TypeRecognizing)
			}
			check := receiveInterimRequest(t, model)
			sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
			preview := receiveInterimRequest(t, model)
			sendInterimStep(t, preview, interimTestStep{token: "不应保存"})
			interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", 8), x.TypeSentenceEnd)
			waitInterviewSignal(t, preview.ended, "interim answer cancellation")
			var final, answer *interimTestRequest
			for i := 0; i < 2; i++ {
				req := receiveInterimRequest(t, model)
				if req.kind == "classification" {
					final = req
				} else if req.kind == "speculative_answer" {
					answer = req
				}
			}
			if final == nil || answer == nil {
				t.Fatal("final requests were not started")
			}
			sendInterimStep(t, final, tc.step)
			waitInterviewSignal(t, answer.ended, "unconfirmed final answer cancellation")
			fyne.DoAndWait(func() {
				if assistant.hasVisibleInterimPreview() || len(assistant.answers) != 0 ||
					len(assistant.savedEntries) != 1 || !assistant.rows[0].generate.Visible() {
					t.Fatal("unconfirmed question retained a preview or hid manual generation")
				}
			})
		})
	}
}

func TestInterimPredictionFailedPreviewRetriesAfterFinal(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	text := "请解释缓存原理"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	sendInterimStep(t, preview, interimTestStep{err: errors.New("preview failed"), done: true})
	waitInterimUI(t, func() bool {
		state := assistant.interimStates[0]
		return state != nil && state.preview != nil && state.preview.visible &&
			strings.Contains(state.preview.error.Text, "preview failed")
	})
	interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", 8), x.TypeSentenceEnd)
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("failed preview was reused instead of starting a fresh answer")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "final speculative cancellation")
}

func TestInterimPredictionCanBeDisabled(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	fyne.DoAndWait(func() { assistant.interimEnabled = false })
	for i := 1; i <= 12; i++ {
		interimUpdate(assistant, speaker, 0, fmt.Sprintf("正在说%d", i), x.TypeRecognizing)
	}
	select {
	case req := <-model.requests:
		t.Fatalf("disabled interim prediction started %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "最终陈述", x.TypeSentenceEnd)
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("disabled interim prediction did not preserve final-only behavior")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "final speculative cancellation")
}

func TestInterimPredictionChangedFinalGeneratesFreshAnswer(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	text := "请解释缓存原理"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	sendInterimStep(t, preview, interimTestStep{token: "过期预测"})
	var displayed *interviewInterimPreview
	fyne.DoAndWait(func() { displayed = assistant.interimStates[0].preview })
	interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", 8)+"新内容", x.TypeSentenceEnd)
	waitInterviewSignal(t, preview.ended, "outdated preview cancellation")
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil || !strings.Contains(answer.messages[len(answer.messages)-1].content, "新内容") {
		t.Fatal("final answer did not use the changed transcript")
	}
	fyne.DoAndWait(func() {
		if !assistant.hasVisibleInterimPreview() {
			t.Fatal("preview was removed before the final decision")
		}
	})
	sendInterimStep(t, final, interimTestStep{token: "QUESTION", done: true})
	waitInterimUI(t, func() bool {
		return len(assistant.answers) == 1 && displayed.answer.Text == "过期预测" &&
			strings.Contains(displayed.status.Text, "预测")
	})
	sendInterimStep(t, answer, interimTestStep{token: "最终回答", done: true})
	waitInterimUI(t, func() bool {
		return len(assistant.answers) == 1 && assistant.answers[0].saved &&
			assistant.answers[0].content == "最终回答" &&
			slices.Contains(assistant.answerList.Objects, fyne.CanvasObject(displayed.container))
	})
	fyne.DoAndWait(func() {
		for _, entry := range assistant.savedEntries {
			if strings.Contains(entry.Content, "过期预测") {
				t.Fatal("outdated provisional answer was saved")
			}
		}
	})
}

func TestInterimPredictionRemainsMarkedIfFinalAnswerFails(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	text := "请解释缓存原理"
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	sendInterimStep(t, preview, interimTestStep{token: "仅供预测"})
	var displayed *interviewInterimPreview
	fyne.DoAndWait(func() { displayed = assistant.interimStates[0].preview })
	interimUpdate(assistant, speaker, 0, text+strings.Repeat("a", 8)+"补充", x.TypeSentenceEnd)
	waitInterviewSignal(t, preview.ended, "preview cancellation")
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("final requests were not started")
	}
	sendInterimStep(t, final, interimTestStep{token: "QUESTION", done: true})
	waitInterimUI(t, func() bool { return len(assistant.answers) == 1 })
	sendInterimStep(t, answer, interimTestStep{err: errors.New("final request failed"), done: true})
	waitInterimUI(t, func() bool {
		a := assistant.answers[0]
		return a.saved && displayed.answer.Text == "仅供预测" &&
			strings.Contains(displayed.status.Text, "预测") && strings.Contains(a.error.Text, "final request failed") &&
			slices.Contains(assistant.answerList.Objects, fyne.CanvasObject(displayed.container))
	})
	fyne.DoAndWait(func() {
		for _, entry := range assistant.savedEntries {
			if strings.Contains(entry.Content, "仅供预测") {
				t.Fatal("preview was saved as a final answer")
			}
		}
	})
}

func TestInterimRecognitionRevisionRemovesPreview(t *testing.T) {
	assistant, model, speaker := newInterimTestAssistant(t)
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, "旧内容需要说明"+strings.Repeat("a", i), x.TypeRecognizing)
	}
	check := receiveInterimRequest(t, model)
	sendInterimStep(t, check, interimTestStep{token: "QUESTION", done: true})
	preview := receiveInterimRequest(t, model)
	sendInterimStep(t, preview, interimTestStep{token: "预测"})
	interimUpdate(assistant, speaker, 0, "修正后的内容需要满足足够字符长度", x.TypeRecognizing)
	waitInterviewSignal(t, preview.ended, "revised preview cancellation")
	fyne.DoAndWait(func() {
		if assistant.hasVisibleInterimPreview() {
			t.Fatal("recognition revision retained an obsolete preview")
		}
	})
	select {
	case req := <-model.requests:
		t.Fatalf("revised transcript was checked without skipping an update: %s", req.kind)
	default:
	}
	interimUpdate(assistant, speaker, 0, "修正后的内容需要满足足够字符长度并补充", x.TypeRecognizing)
	next := receiveInterimRequest(t, model)
	if next.kind != "interim_classification" ||
		!strings.Contains(next.messages[1].content, "修正后的内容") {
		t.Fatal("revised interim transcript was not checked")
	}
	sendInterimStep(t, next, interimTestStep{token: "OTHER", done: true})
	interimUpdate(assistant, speaker, 0, "修正后的内容", x.TypeSentenceEnd)
	var final, answer *interimTestRequest
	for i := 0; i < 2; i++ {
		req := receiveInterimRequest(t, model)
		if req.kind == "classification" {
			final = req
		} else if req.kind == "speculative_answer" {
			answer = req
		}
	}
	if final == nil || answer == nil {
		t.Fatal("final transcript did not trigger normal classification")
	}
	sendInterimStep(t, final, interimTestStep{token: "OTHER", done: true})
	waitInterviewSignal(t, answer.ended, "final speculative cancellation")
}

type burstInterimModel struct{}

func (burstInterimModel) streamAnswer(ctx context.Context, _ []interviewChatMessage, _ func(), onToken func(string)) error {
	if requestTraceFromContext(ctx).kind == "interim_classification" {
		onToken("QUESTION")
		return nil
	}
	for i := 0; i < 2050; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		onToken("a")
	}
	return nil
}

func TestInterimPreviewDoesNotBufferFinalAnswer(t *testing.T) {
	assistant, _, speaker := newInterimTestAssistant(t)
	assistant.model = burstInterimModel{}
	for i := 1; i <= interviewInterimThreshold; i++ {
		interimUpdate(assistant, speaker, 0, "足够长度的测试文本"+strings.Repeat("a", i), x.TypeRecognizing)
	}
	waitInterimUI(t, func() bool {
		state := assistant.interimStates[0]
		return state != nil && state.preview != nil && len(state.preview.content) == 2050 &&
			state.preview.statusKey == "interview.interim_waiting_final"
	})
}
