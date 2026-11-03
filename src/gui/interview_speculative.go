package gui

import (
	"context"
	"time"
)

type interviewAnswerEvent struct {
	thinking bool
	token    string
}

type interviewSpeculativeAnswer struct {
	cancel context.CancelFunc
	events chan interviewAnswerEvent
	err    error
}

func (s *interviewAssistant) startSpeculativeAnswer(messages []interviewChatMessage) *interviewSpeculativeAnswer {
	return s.startSpeculativeAnswerForTurn(messages, -1)
}

func (s *interviewAssistant) startSpeculativeAnswerForTurn(messages []interviewChatMessage, turnIndex int) *interviewSpeculativeAnswer {
	ctx, cancel := context.WithTimeout(s.ctx, interviewAnswerTimeout)
	ctx = withInterviewRequestTrace(ctx, interviewRequestTrace{session: s.traceSession, turn: turnIndex, kind: "speculative_answer"})
	startedAt := time.Now()
	answer := &interviewSpeculativeAnswer{
		cancel: cancel,
		events: make(chan interviewAnswerEvent, 128),
	}
	s.answerWG.Add(1)
	go func() {
		defer s.answerWG.Done()
		defer close(answer.events)
		s.debugf("speculative answer started session=%d turn=%d", s.traceSession, turnIndex)
		chunks := 0
		send := func(event interviewAnswerEvent) (time.Duration, bool) {
			started := time.Now()
			select {
			case answer.events <- event:
				return time.Since(started), true
			case <-ctx.Done():
				return time.Since(started), false
			}
		}
		answer.err = s.model.streamAnswer(ctx, messages, func() {
			send(interviewAnswerEvent{thinking: true})
		}, func(token string) {
			chunks++
			depth := len(answer.events)
			wait, accepted := send(interviewAnswerEvent{token: token})
			s.debugf("speculative answer buffered session=%d turn=%d chunk=%d chars=%d elapsed_ms=%d buffer_depth_before=%d buffer_depth_after=%d buffer_wait_ms=%d accepted=%t",
				s.traceSession, turnIndex, chunks, len([]rune(token)), time.Since(startedAt).Milliseconds(), depth, len(answer.events), wait.Milliseconds(), accepted)
		})
		if answer.err == nil {
			answer.err = ctx.Err()
		}
		s.debugf("speculative answer finished session=%d turn=%d chunks=%d duration_ms=%d error=%s",
			s.traceSession, turnIndex, chunks, time.Since(startedAt).Milliseconds(), diagnosticError(answer.err))
	}()
	return answer
}

func (a *interviewSpeculativeAnswer) streamAnswer(
	ctx context.Context,
	_ []interviewChatMessage,
	onThinking func(),
	onToken func(string),
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-a.events:
			if !ok {
				return a.err
			}
			if event.thinking {
				if onThinking != nil {
					onThinking()
				}
			} else if onToken != nil {
				onToken(event.token)
			}
		}
	}
}
