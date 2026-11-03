package gui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

func TestInterviewModelTimingLogsCorrelateRequestsWithoutContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"DO_NOT_LOG_RESPONSE\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	model, err := newChatCompletionModel(modelConfig{BaseURL: server.URL + "/v1", APIKey: "DO_NOT_LOG_KEY", Model: "demo-model"})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	model.debugLog = log.New(&logs, "", 0)
	for _, kind := range []string{"classification", "speculative_answer"} {
		ctx := withInterviewRequestTrace(context.Background(), interviewRequestTrace{session: 12, turn: 7, kind: kind})
		if err := model.streamAnswer(ctx, []interviewChatMessage{{role: "user", content: "DO_NOT_LOG_PROMPT"}}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	output := logs.String()
	for _, kind := range []string{"classification", "speculative_answer"} {
		pattern := fmt.Sprintf(`request_id=([0-9]+) session=12 turn=7 kind=%s request started`, kind)
		if !regexp.MustCompile(pattern).MatchString(output) {
			t.Errorf("missing correlated request start for %s: %s", kind, output)
		}
		for _, event := range []string{"http connection", "http request_written", "http first_response_byte", "stream connected", "first answer token", "answer chunk=1", "stream completed"} {
			if !strings.Contains(output, "kind="+kind+" "+event) {
				t.Errorf("missing %s event for %s", event, kind)
			}
		}
	}
	ids := regexp.MustCompile(`request_id=([0-9]+) session=12 turn=7 kind=[a-z_]+ request started`).FindAllStringSubmatch(output, -1)
	if len(ids) != 2 || ids[0][1] == ids[1][1] {
		t.Fatalf("request IDs are not unique: %v", ids)
	}
	for _, secret := range []string{"DO_NOT_LOG_RESPONSE", "DO_NOT_LOG_PROMPT", "DO_NOT_LOG_KEY"} {
		if strings.Contains(output, secret) {
			t.Errorf("diagnostic log contains %q", secret)
		}
	}
}

func TestInterviewASRTimingLogsInterimAndFinalEvents(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	var logs bytes.Buffer
	assistant := &interviewAssistant{
		traceSession: 42,
		debugLog:     log.New(&logs, "", 0),
		checkQueue:   make(chan interviewQuestionCheck, 1),
		transcript:   container.NewVBox(),
		status:       widget.NewLabel(""),
	}
	assistant.transcriptScroll = container.NewVScroll(assistant.transcript)
	assistant.running.Store(true)
	speaker := &asrSoundCap{deviceName: "speaker", resultIndex: -1, writeListIndex: -1}
	start := time.Now().Add(-3 * time.Second)
	for _, event := range []struct {
		at   time.Duration
		kind string
	}{
		{100 * time.Millisecond, x.TypeRecognizing},
		{800 * time.Millisecond, x.TypeRecognizing},
		{1700 * time.Millisecond, x.TypeSentenceEnd},
	} {
		assistant.applyASRResultAt(speaker, &x.AllResponse{
			Type: event.kind, Result: x.RecognizeResult{Index: 5, Result: "PRIVATE_TRANSCRIPT"},
		}, start.Add(event.at))
	}
	output := logs.String()
	for _, event := range []string{
		"asr applied session=42 turn=0 source_microphone=false index=5 final=true newly_final=true",
		"first_interim_to_final_ms=1600", "last_interim_to_final_ms=900",
		"question check queued session=42 turn=0", "final_to_queue_ms=",
	} {
		if !strings.Contains(output, event) {
			t.Errorf("missing %q in %s", event, output)
		}
	}
	if strings.Contains(output, "PRIVATE_TRANSCRIPT") {
		t.Fatal("ASR timing log contains transcript text")
	}
}
