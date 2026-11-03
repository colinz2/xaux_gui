package gui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const interviewInterimPreference = "interview_interim_prediction"
const interviewInterimThreshold = 8
const interviewInterimMinChars = 15

type interviewInterimState struct {
	updates             int
	lastRequested       string
	lastRequestedUpdate int
	checking            bool
	cancelCheck         context.CancelFunc
	closed              bool
	preview             *interviewInterimPreview
}

type interviewInterimPreview struct {
	text       string
	cancel     context.CancelFunc
	container  *fyne.Container
	card       *widget.Card
	answer     *widget.Label
	status     *widget.Label
	error      *widget.Label
	statusKey  string
	errorValue error
	content    string
	visible    bool
	discarded  bool
}

func (s *interviewAssistant) observeInterim(index int) {
	if s.ctx == nil || s.ctx.Err() != nil || strings.TrimSpace(s.turns[index].content) == "" {
		return
	}
	if s.interimStates == nil {
		s.interimStates = make(map[int]*interviewInterimState)
	}
	state := s.interimStates[index]
	if state == nil {
		state = &interviewInterimState{}
		s.interimStates[index] = state
	}
	if state.closed {
		return
	}
	state.updates++
	current := s.turns[index].content
	s.debugf("interim update session=%d turn=%d updates=%d chars=%d checking=%t",
		s.traceSession, index, state.updates, len([]rune(current)), state.checking)
	if state.preview != nil && !strings.HasPrefix(current, state.preview.text) {
		s.debugf("interim preview invalidated session=%d turn=%d reason=recognition_revision", s.traceSession, index)
		s.removeInterimPreview(index, true)
	}
	if state.updates >= interviewInterimThreshold && utf8.RuneCountInString(strings.TrimSpace(current)) >= interviewInterimMinChars {
		s.startInterimCheck(index, state)
	}
}

func (s *interviewAssistant) interimTurns(index int) []interviewTurn {
	turns := append([]interviewTurn(nil), s.turns[:index+1]...)
	turns[index].final = true
	return turns
}

func (s *interviewAssistant) startInterimCheck(index int, state *interviewInterimState) {
	if state.closed || state.checking || s.stopping.Load() || s.ctx.Err() != nil {
		return
	}
	text := s.turns[index].content
	if state.updates < interviewInterimThreshold ||
		(state.lastRequestedUpdate > 0 && state.updates-state.lastRequestedUpdate < 2) ||
		text == state.lastRequested || utf8.RuneCountInString(strings.TrimSpace(text)) < interviewInterimMinChars {
		return
	}
	messages := buildInterviewQuestionMessages(s.interimTurns(index))
	messages[0].content += " 当前发言是尚未结束的语音识别中间结果。不要猜测或补全尚未说出的内容；" +
		"仅当已说出的内容明确提出需要回答的要求时输出 QUESTION，否则按规则输出 OTHER 或 UNCERTAIN。"
	ctx, cancel := context.WithTimeout(s.ctx, interviewQuestionTimeout)
	ctx = withInterviewRequestTrace(ctx, interviewRequestTrace{session: s.traceSession, turn: index, kind: "interim_classification"})
	state.checking = true
	state.lastRequested = text
	state.lastRequestedUpdate = state.updates
	state.cancelCheck = cancel
	startedAt := time.Now()
	s.debugf("interim check started session=%d turn=%d updates=%d chars=%d", s.traceSession, index, state.updates, len([]rune(text)))
	s.answerWG.Add(1)
	go func() {
		defer s.answerWG.Done()
		decision, err := classifyInterviewTurn(ctx, s.model, messages)
		cancel()
		s.debugf("interim check completed session=%d turn=%d decision=%d duration_ms=%d error=%s",
			s.traceSession, index, decision, time.Since(startedAt).Milliseconds(), diagnosticError(err))
		fyne.Do(func() {
			if s.stopping.Load() || state.closed || s.interimStates[index] != state {
				return
			}
			state.checking = false
			state.cancelCheck = nil
			current := s.turns[index].content
			s.debugf("interim check applied session=%d turn=%d updates=%d decision=%d current_chars=%d prefix_matches=%t",
				s.traceSession, index, state.updates, decision, len([]rune(current)), strings.HasPrefix(current, text))
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					if current == text && state.preview != nil {
						s.removeInterimPreview(index, true)
					}
					s.locale.setLabel(s.rows[index].status, "interview.interim_check_failed", nil)
					if s.status != nil {
						s.setStatus("interview.interim_check_error", map[string]any{"Error": err}, widget.WarningImportance)
					}
				}
			} else if decision == interviewQuestionYes && strings.HasPrefix(current, text) {
				if state.preview == nil && (!s.hasAnswerTurn || index >= s.latestAnswerTurn) {
					s.startInterimPreview(index, state)
				}
			} else if decision != interviewQuestionYes && current == text && state.preview != nil {
				s.removeInterimPreview(index, true)
			}
			if current != text {
				s.startInterimCheck(index, state)
			}
		})
	}()
}

func (s *interviewAssistant) startInterimPreview(index int, state *interviewInterimState) {
	turn := s.turns[index]
	messages := buildInterviewMessages(s.interimTurns(index), s.modelConfig.SystemPrompt, s.reference)
	answer := widget.NewLabel("")
	answer.Wrapping = fyne.TextWrapBreak
	answer.Selectable = true
	status := widget.NewLabel("")
	status.SetText(tr("interview.interim_predicting"))
	status.Importance = widget.WarningImportance
	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapBreak
	errorLabel.Importance = widget.DangerImportance
	question := widget.NewLabel(turn.content)
	question.Wrapping = fyne.TextWrapBreak
	card := widget.NewCard(tr("interview.interim_title"), turn.timestamp,
		container.NewVBox(status, question, widget.NewSeparator(), answer, errorLabel))
	preview := &interviewInterimPreview{
		text: turn.content, card: card, answer: answer, status: status, error: errorLabel,
		statusKey: "interview.interim_predicting",
		container: container.NewVBox(widget.NewSeparator(), card),
	}
	state.preview = preview
	s.debugf("interim answer started session=%d turn=%d updates=%d chars=%d", s.traceSession, index, state.updates, len([]rune(turn.content)))
	ctx, cancel := context.WithTimeout(s.ctx, interviewAnswerTimeout)
	ctx = withInterviewRequestTrace(ctx, interviewRequestTrace{session: s.traceSession, turn: index, kind: "interim_answer"})
	preview.cancel = cancel
	s.answerWG.Add(1)
	go func() {
		defer s.answerWG.Done()
		startedAt := time.Now()
		chunks := 0
		err := s.model.streamAnswer(ctx, messages, nil, func(token string) {
			chunks++
			emittedAt := time.Now()
			fyne.Do(func() {
				if s.stopping.Load() || state.closed || state.preview != preview || preview.discarded {
					return
				}
				first := !preview.visible
				if first {
					s.showInterimPreview(preview)
				}
				preview.content += token
				preview.answer.SetText(preview.content)
				s.answerScroll.ScrollToBottom()
				now := time.Now()
				s.debugf("interim answer ui session=%d turn=%d first=%t chars=%d ui_queue_ms=%d first_interim_to_ui_ms=%d",
					s.traceSession, index, first, len([]rune(preview.content)), elapsedMillis(emittedAt, now),
					elapsedMillis(s.turns[index].firstInterimAt, now))
			})
		})
		if err == nil {
			err = ctx.Err()
		}
		cancel()
		s.debugf("interim answer finished session=%d turn=%d chunks=%d duration_ms=%d error=%s",
			s.traceSession, index, chunks, time.Since(startedAt).Milliseconds(), diagnosticError(err))
		fyne.Do(func() {
			if s.stopping.Load() || state.closed || state.preview != preview || preview.discarded {
				return
			}
			if err != nil {
				if !preview.visible {
					s.showInterimPreview(preview)
				}
				preview.statusKey = "interview.interim_answer_failed"
				preview.errorValue = err
				s.refreshInterimPreviewLocale(preview)
			} else if preview.visible {
				preview.statusKey = "interview.interim_waiting_final"
				s.refreshInterimPreviewLocale(preview)
			}
		})
	}()
}

func (s *interviewAssistant) refreshInterimPreviewLocale(preview *interviewInterimPreview) {
	preview.card.SetTitle(tr("interview.interim_title"))
	preview.status.SetText(tr(preview.statusKey))
	if preview.errorValue != nil {
		preview.error.SetText(trData("common.error", map[string]any{"Error": preview.errorValue}))
	}
}

func (s *interviewAssistant) refreshInterimLocale() {
	for _, state := range s.interimStates {
		if state.preview != nil && !state.preview.discarded {
			s.refreshInterimPreviewLocale(state.preview)
		}
	}
}

func (s *interviewAssistant) hasVisibleInterimPreview() bool {
	for _, state := range s.interimStates {
		if state.preview != nil && state.preview.visible && !state.preview.discarded {
			return true
		}
	}
	return false
}

func (s *interviewAssistant) showInterimPreview(preview *interviewInterimPreview) {
	if len(s.answers) == 0 && !s.hasVisibleInterimPreview() {
		s.answerList.Objects = nil
	}
	preview.visible = true
	s.answerList.Objects = append(s.answerList.Objects, preview.container)
	s.answerList.Refresh()
}

func (s *interviewAssistant) removeInterimPreview(index int, cancel bool) {
	state := s.interimStates[index]
	if state == nil || state.preview == nil {
		return
	}
	preview := state.preview
	preview.discarded = true
	state.preview = nil
	if cancel && preview.cancel != nil {
		preview.cancel()
	}
	if preview.visible {
		s.answerList.Remove(preview.container)
		if len(s.answers) == 0 && !s.hasVisibleInterimPreview() && s.emptyAnswers != nil {
			s.answerList.Objects = []fyne.CanvasObject{container.NewCenter(s.emptyAnswers)}
		}
		s.answerList.Refresh()
	}
	if state.closed {
		delete(s.interimStates, index)
	}
}

func (s *interviewAssistant) discardInterimState(index int) {
	state := s.interimStates[index]
	if state == nil {
		return
	}
	state.closed = true
	if state.cancelCheck != nil {
		state.cancelCheck()
	}
	s.removeInterimPreview(index, true)
	delete(s.interimStates, index)
}

func (s *interviewAssistant) finalizeInterim(index int) *interviewInterimPreview {
	state := s.interimStates[index]
	if state == nil {
		return nil
	}
	state.closed = true
	if state.cancelCheck != nil {
		state.cancelCheck()
	}
	preview := state.preview
	if preview != nil && preview.cancel != nil {
		preview.cancel()
	}
	if preview == nil || !preview.visible || preview.content == "" {
		s.discardInterimState(index)
		return nil
	}
	preview.statusKey = "interview.interim_waiting_confirmation"
	s.refreshInterimPreviewLocale(preview)
	s.debugf("interim answer retained pending final decision session=%d turn=%d updates=%d", s.traceSession, index, state.updates)
	return preview
}

func (s *interviewAssistant) retainInterimPreview(index int, preview *interviewInterimPreview) bool {
	state := s.interimStates[index]
	if state == nil || state.preview != preview || preview.discarded || !preview.visible {
		return false
	}
	preview.discarded = true
	state.preview = nil
	delete(s.interimStates, index)
	if preview.cancel != nil {
		preview.cancel()
	}
	s.locale.setLabel(preview.status, "interview.interim_retained", nil)
	if preview.errorValue != nil {
		s.locale.setLabel(preview.error, "common.error", map[string]any{"Error": preview.errorValue})
	}
	s.locale.add(func() { preview.card.SetTitle(tr("interview.interim_title")) })
	return true
}
