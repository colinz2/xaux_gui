package gui

import "testing"

func TestVoiceTaskGateAllowsOnlyOneActiveTask(t *testing.T) {
	var gate voiceTaskGate
	if !gate.acquire(realTimeTransMenuName) {
		t.Fatal("failed to acquire the first voice task")
	}
	if gate.acquire(interviewAssistantName) {
		t.Fatal("acquired a second task while transcription was active")
	}
	if got := gate.activeTask(); got != realTimeTransMenuName {
		t.Fatalf("active task = %q, want %q", got, realTimeTransMenuName)
	}

	gate.release(interviewAssistantName)
	if got := gate.activeTask(); got != realTimeTransMenuName {
		t.Fatalf("releasing a different task changed active task to %q", got)
	}
	gate.release(realTimeTransMenuName)
	if !gate.acquire(recordMenuName) {
		t.Fatal("failed to acquire a task after the active task was released")
	}
}

func resetVoiceTasksForTest() {
	voiceTasks.Lock()
	voiceTasks.active = ""
	voiceTasks.Unlock()
}
