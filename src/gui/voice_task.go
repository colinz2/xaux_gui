package gui

import "sync"

type voiceTaskGate struct {
	sync.Mutex
	active string
}

func (g *voiceTaskGate) acquire(task string) bool {
	g.Lock()
	defer g.Unlock()

	if g.active != "" {
		return false
	}
	g.active = task
	return true
}

func (g *voiceTaskGate) release(task string) {
	g.Lock()
	defer g.Unlock()

	if g.active == task {
		g.active = ""
	}
}

func (g *voiceTaskGate) activeTask() string {
	g.Lock()
	defer g.Unlock()
	return g.active
}

var voiceTasks voiceTaskGate

func acquireVoiceTask(task string) bool {
	return voiceTasks.acquire(task)
}

func releaseVoiceTask(task string) {
	voiceTasks.release(task)
}

func activeVoiceTask() string {
	return voiceTasks.activeTask()
}

func voiceTaskDisplayName(task string) string {
	switch task {
	case recordMenuName:
		return tr("menu.record")
	case realTimeTransMenuName:
		return tr("menu.transcription")
	case interviewAssistantName:
		return tr("menu.interview")
	case subtitleMenuName:
		return tr("menu.subtitle")
	default:
		return task
	}
}
