package gui

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"xaux_gui/pkg/sound_cap"
)

const (
	sessionManifestName        = "session.json"
	sessionTranscriptName      = "transcript.txt"
	sessionInterviewReviewName = "interview_review.txt"
	sessionSummaryName         = "summary.txt"
	sessionJobDescriptionName  = "job_description.txt"
	sessionMixedRecordingName  = "mixed.wav"
)

type SessionStatus string

const (
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusStopped   SessionStatus = "stopped"
	SessionStatusFailed    SessionStatus = "failed"
)

type SessionKind string

const (
	SessionKindRecording     SessionKind = "recording"
	SessionKindTranscription SessionKind = "transcription"
	SessionKindInterview     SessionKind = "interview"
)

type SessionInfo struct {
	Name               string        `json:"name"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	Status             SessionStatus `json:"status"`
	Kind               SessionKind   `json:"kind"`
	Recordings         []string      `json:"recordings,omitempty"`
	MixedRecording     string        `json:"mixed_recording,omitempty"`
	TranscriptFile     string        `json:"transcript_file,omitempty"`
	ReviewFile         string        `json:"review_file,omitempty"`
	SummaryFile        string        `json:"summary_file,omitempty"`
	JobDescriptionFile string        `json:"job_description_file,omitempty"`
	Error              string        `json:"error,omitempty"`
	Directory          string        `json:"-"`
}

type SessionTranscriptEntry struct {
	Timestamp string
	Device    string
	Content   string
}

type Session struct {
	mu                sync.Mutex
	info              SessionInfo
	recordingSegments map[string][]sound_cap.RecordingSegment
}

func CreateSession(rootDir, requestedName string, kind SessionKind) (*Session, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, errors.New("session root directory is empty")
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session root directory: %w", err)
	}

	now := time.Now()
	baseName := normalizeSessionName(requestedName, now)
	for sequence := 1; ; sequence++ {
		name := baseName
		if sequence > 1 {
			name = fmt.Sprintf("%s-%d", baseName, sequence)
		}
		directory := filepath.Join(rootDir, name)
		err := os.Mkdir(directory, 0o755)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create session directory: %w", err)
		}

		session := &Session{info: SessionInfo{
			Name:      name,
			CreatedAt: now,
			UpdatedAt: now,
			Status:    SessionStatusRunning,
			Kind:      kind,
			Directory: directory,
		}}
		if err := session.saveManifestLocked(); err != nil {
			_ = os.Remove(directory)
			return nil, err
		}
		return session, nil
	}
}

func normalizeSessionName(requestedName string, now time.Time) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(requestedName))
	name = strings.Trim(name, " .")
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	if name == "" {
		name = now.Format("20060102-150405")
	}
	reservedName := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch reservedName {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	return name
}

func (s *Session) Snapshot() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.info
	info.Recordings = append([]string(nil), s.info.Recordings...)
	return info
}

func (s *Session) Directory() string {
	return s.info.Directory
}

func (s *Session) AddRecording(fileName string) error {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." {
		return errors.New("recording file name is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Status != SessionStatusRunning {
		return errors.New("cannot add a recording to a finished session")
	}
	for _, existing := range s.info.Recordings {
		if existing == fileName {
			return nil
		}
	}
	s.info.Recordings = append(s.info.Recordings, fileName)
	s.info.UpdatedAt = time.Now()
	return s.saveManifestLocked()
}

func (s *Session) recordSegment(fileName string, segment sound_cap.RecordingSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordingSegments == nil {
		s.recordingSegments = make(map[string][]sound_cap.RecordingSegment)
	}
	s.recordingSegments[fileName] = append(s.recordingSegments[fileName], segment)
}

func (s *Session) SaveJobDescription(description string) error {
	description, err := validateInterviewContext(description, &localizedText{key: "reference.job"})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Kind != SessionKindInterview || s.info.Status != SessionStatusRunning {
		return localizedMessageError("session.job_requires_interview", nil, "job description requires a running interview", nil)
	}
	if description == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(s.info.Directory, sessionJobDescriptionName), []byte(description), 0o600); err != nil {
		return localizedMessageError("session.job_save_failed", nil, "save job description failed", err)
	}
	updated := s.info
	updated.JobDescriptionFile = sessionJobDescriptionName
	if err := writeSessionManifest(updated); err != nil {
		return err
	}
	s.info = updated
	return nil
}

func (s *Session) Finish(status SessionStatus, transcript []SessionTranscriptEntry, taskErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Status != SessionStatusRunning {
		return nil
	}
	if status == "" || status == SessionStatusRunning {
		status = SessionStatusCompleted
	}
	if taskErr != nil {
		status = SessionStatusFailed
		s.info.Error = taskErr.Error()
	}

	var resultErr error
	if len(s.info.Recordings) > 0 {
		inputs := make([]sound_cap.RecordingInput, 0, len(s.info.Recordings))
		for _, fileName := range s.info.Recordings {
			inputs = append(inputs, sound_cap.RecordingInput{
				Path:     filepath.Join(s.info.Directory, fileName),
				Segments: s.recordingSegments[fileName],
			})
		}
		if err := sound_cap.MixRecordings(filepath.Join(s.info.Directory, sessionMixedRecordingName), inputs); err != nil {
			resultErr = fmt.Errorf("mix session recordings: %w", err)
			status = SessionStatusFailed
			s.info.Error = errors.Join(taskErr, resultErr).Error()
		} else {
			s.info.MixedRecording = sessionMixedRecordingName
		}
	}
	if transcript != nil {
		if err := writeSessionTranscript(filepath.Join(s.info.Directory, sessionTranscriptName), transcript); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("save session transcript: %w", err))
			status = SessionStatusFailed
			if s.info.Error == "" {
				s.info.Error = err.Error()
			}
		} else {
			s.info.TranscriptFile = sessionTranscriptName
		}
	}
	s.info.Status = status
	s.info.UpdatedAt = time.Now()
	if err := s.saveManifestLocked(); err != nil {
		resultErr = errors.Join(resultErr, err)
	}
	return resultErr
}

func (s *Session) saveManifestLocked() error {
	return writeSessionManifest(s.info)
}

func writeSessionManifest(info SessionInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session manifest: %w", err)
	}
	data = append(data, '\n')
	manifestPath := filepath.Join(info.Directory, sessionManifestName)
	temporaryPath := manifestPath + ".tmp"
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		return fmt.Errorf("write session manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, manifestPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace session manifest: %w", err)
	}
	return nil
}

func saveInterviewReview(info *SessionInfo, review string) error {
	if info == nil || info.Kind != SessionKindInterview {
		return localizedMessageError("session.review_requires_interview", nil, "interview review requires an interview session", nil)
	}
	return saveSessionAnalysis(info, review, sessionInterviewReviewName)
}

func saveTranscriptionSummary(info *SessionInfo, summary string) error {
	if info == nil || info.Kind != SessionKindTranscription {
		return localizedMessageError("session.summary_requires_transcription", nil, "transcription summary requires a transcription session", nil)
	}
	return saveSessionAnalysis(info, summary, sessionSummaryName)
}

func saveSessionAnalysisResult(info *SessionInfo, content string) error {
	if info == nil {
		return localizedMessageError("session.empty", nil, "session is nil", nil)
	}
	switch info.Kind {
	case SessionKindInterview:
		return saveInterviewReview(info, content)
	case SessionKindTranscription:
		return saveTranscriptionSummary(info, content)
	default:
		return localizedMessageError("session.analysis_unsupported", nil, "session type does not support AI analysis", nil)
	}
}

func saveSessionAnalysis(info *SessionInfo, content, fileName string) error {
	if info.Status == SessionStatusRunning {
		return localizedMessageError("session.analysis_requires_finished", analysisData(info.Kind, nil), "session must finish before generating analysis", nil)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return localizedMessageError("session.analysis_empty", analysisData(info.Kind, nil), "analysis content is empty", nil)
	}
	if strings.TrimSpace(info.Directory) == "" {
		return localizedMessageError("session.directory_empty", nil, "session directory is empty", nil)
	}
	path := filepath.Join(info.Directory, fileName)
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		return localizedMessageError("session.analysis_save_failed", analysisData(info.Kind, nil), "save session analysis failed", err)
	}

	updated := *info
	if info.Kind == SessionKindInterview {
		updated.ReviewFile = fileName
	} else {
		updated.SummaryFile = fileName
	}
	if err := writeSessionManifest(updated); err != nil {
		return err
	}
	*info = updated
	return nil
}

func writeSessionTranscript(path string, entries []SessionTranscriptEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		if _, err = fmt.Fprintf(writer, "[%s] [%s]\n%s\n\n", entry.Timestamp, entry.Device, entry.Content); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err = writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func LoadSessions(rootDir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(rootDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session root directory: %w", err)
	}

	sessions := make([]SessionInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(rootDir, entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, sessionManifestName))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read session %q: %w", entry.Name(), err)
		}
		var info SessionInfo
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, fmt.Errorf("decode session %q: %w", entry.Name(), err)
		}
		info.Directory = directory
		sessions = append(sessions, info)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].Name > sessions[j].Name
		}
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})
	return sessions, nil
}

func sessionStatusText(status SessionStatus) string {
	switch status {
	case SessionStatusRunning:
		return tr("history.status_running")
	case SessionStatusCompleted:
		return tr("history.status_completed")
	case SessionStatusStopped:
		return tr("common.stopped")
	case SessionStatusFailed:
		return tr("history.status_failed")
	default:
		return string(status)
	}
}

func sessionKindText(kind SessionKind) string {
	switch kind {
	case SessionKindRecording:
		return tr("menu.record")
	case SessionKindTranscription:
		return tr("menu.transcription")
	case SessionKindInterview:
		return tr("menu.interview")
	default:
		return string(kind)
	}
}
