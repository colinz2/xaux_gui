package gui

import (
	"context"
	"sync/atomic"
	"time"
)

type interviewTraceKey struct{}

type interviewRequestTrace struct {
	session uint64
	turn    int
	kind    string
}

var interviewSessionSequence atomic.Uint64
var interviewRequestSequence atomic.Uint64

func withInterviewRequestTrace(ctx context.Context, trace interviewRequestTrace) context.Context {
	return context.WithValue(ctx, interviewTraceKey{}, trace)
}

func requestTraceFromContext(ctx context.Context) interviewRequestTrace {
	if trace, ok := ctx.Value(interviewTraceKey{}).(interviewRequestTrace); ok {
		return trace
	}
	return interviewRequestTrace{turn: -1, kind: "other"}
}

func elapsedMillis(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() {
		return -1
	}
	return end.Sub(start).Milliseconds()
}
