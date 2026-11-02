package sound_cap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"xaux_gui/pkg/ffaudio"

	"github.com/realzhangm/xaux/pkg/x"
)

// CaptureMode selects the source without exposing ffaudio flags to the GUI.
type CaptureMode int

const (
	CaptureMicrophone CaptureMode = iota + 1
	CaptureSpeaker
)

type State int32

const (
	StateCreated State = iota
	StateStarting
	StateRunning
	StatePaused
	StateResuming
	StateStopped
	StateError
)

var (
	ErrInvalidCaptureMode  = errors.New("invalid capture mode")
	ErrInvalidState        = errors.New("invalid sound capture state")
	ErrASRProxyUnavailable = errors.New("ASR proxy unavailable")
)

type Config struct {
	ProxyAddr      string // Empty disables ASR and selects recording-only mode.
	DeviceName     string // Empty selects the system default for the chosen mode.
	Mode           CaptureMode
	RecordFilePath string

	// Output format used for both ASR and the optional WAV recording. Zero
	// values select the required 16 kHz/mono/int16 format; other values are
	// accepted only when they explicitly match that format.
	SampleRate    int
	Channels      int
	BitsPerSample int

	BufferLengthMsec int
	OnError          func(error)
	OnStateChange    func(State)
}

// SoundCap adapts one ffaudio stream to the existing ASR recording flow.
// Simultaneous microphone+speaker capture uses two SoundCap instances so each
// source retains its own ASR session and recording, as in the original GUI.
type SoundCap struct {
	Config

	ctx    context.Context
	cancel context.CancelFunc

	asrClient *x.Client
	converter *ffaudio.PCMConverter
	recording *wavWriter

	audioMu sync.RWMutex
	audio   *ffaudio.FFAudio

	// ioMu serializes conversion, ASR access and WAV finalization with Close.
	ioMu sync.Mutex

	startDoneChan chan error
	startOnce     sync.Once
	closeOnce     sync.Once
	closed        int32
	state         int32
}

func NewSoundCap(ctx context.Context, config *Config, asrCb x.AsrResultCallBack) (*SoundCap, error) {
	if config == nil {
		return nil, fmt.Errorf("nil sound capture config")
	}
	if config.Mode != CaptureMicrophone && config.Mode != CaptureSpeaker {
		return nil, fmt.Errorf("%w: %d", ErrInvalidCaptureMode, config.Mode)
	}

	sc := &SoundCap{
		Config:        *config,
		startDoneChan: make(chan error, 1),
	}
	if sc.SampleRate == 0 {
		sc.SampleRate = 16000
	}
	if sc.Channels == 0 {
		sc.Channels = 1
	}
	if sc.BitsPerSample == 0 {
		sc.BitsPerSample = 16
	}
	if sc.BufferLengthMsec == 0 {
		sc.BufferLengthMsec = 64
	}
	if sc.SampleRate != 16000 || sc.Channels != 1 || sc.BitsPerSample != 16 {
		return nil, fmt.Errorf("ASR output must be 16 kHz/mono/int16, got rate=%d channels=%d bits=%d",
			sc.SampleRate, sc.Channels, sc.BitsPerSample)
	}
	sc.ctx, sc.cancel = context.WithCancel(ctx)
	atomic.StoreInt32(&sc.state, int32(StateCreated))

	if sc.ProxyAddr != "" {
		var err error
		if sc.asrClient, err = x.NewClient(sc.ProxyAddr, asrCb); err != nil {
			sc.cancel()
			return nil, fmt.Errorf("%w: connect ASR proxy: %w", ErrASRProxyUnavailable, err)
		}
		if err = sc.asrClient.Start(x.StartConfig{
			SampleRate:    int32(sc.SampleRate),
			BitsPerSample: int32(sc.BitsPerSample),
		}); err != nil {
			sc.asrClient.Close()
			sc.cancel()
			return nil, fmt.Errorf("%w: start ASR session: %w", ErrASRProxyUnavailable, err)
		}
	}
	return sc, nil
}

func (s *SoundCap) StartDone() <-chan error {
	return s.startDoneChan
}

func (s *SoundCap) notifyStart(err error) {
	s.startOnce.Do(func() {
		s.startDoneChan <- err
	})
}

func (s *SoundCap) State() State {
	return State(atomic.LoadInt32(&s.state))
}

func (s *SoundCap) setState(state State) {
	old := State(atomic.SwapInt32(&s.state, int32(state)))
	if old != state && s.OnStateChange != nil {
		s.OnStateChange(state)
	}
}

func (s *SoundCap) transitionState(from, to State) bool {
	if !atomic.CompareAndSwapInt32(&s.state, int32(from), int32(to)) {
		return false
	}
	if s.OnStateChange != nil {
		s.OnStateChange(to)
	}
	return true
}

func (s *SoundCap) acceptsAudio() bool {
	state := s.State()
	return state == StateRunning || state == StateResuming
}

func (s *SoundCap) reportError(err error) {
	if err == nil {
		return
	}
	if s.OnError != nil {
		s.OnError(err)
		return
	}
	log.Printf("sound capture: %v", err)
}

func (s *SoundCap) getAudio() *ffaudio.FFAudio {
	s.audioMu.RLock()
	defer s.audioMu.RUnlock()
	return s.audio
}

// Write implements io.Writer for ffaudio. Input uses the device's native PCM
// format; conversion happens before data reaches ASR or the WAV file.
func (s *SoundCap) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if s.isClose() || !s.acceptsAudio() {
		return len(p), nil
	}

	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	if s.isClose() || !s.acceptsAudio() {
		return len(p), nil
	}
	if s.converter == nil {
		return 0, fmt.Errorf("PCM converter is not initialized")
	}
	converted, err := s.converter.Convert(p)
	if err != nil {
		return 0, fmt.Errorf("convert PCM: %w", err)
	}
	if len(converted) == 0 {
		return len(p), nil
	}
	if s.asrClient != nil {
		if err := s.asrClient.Send(converted); err != nil {
			return 0, fmt.Errorf("send PCM to ASR: %w", err)
		}
	}
	if s.recording != nil {
		if _, err := s.recording.Write(converted); err != nil {
			return 0, fmt.Errorf("write recording: %w", err)
		}
	}
	return len(p), nil
}

func (s *SoundCap) Run() (runErr error) {
	if !atomic.CompareAndSwapInt32(&s.state, int32(StateCreated), int32(StateStarting)) {
		err := fmt.Errorf("%w: Run from %d", ErrInvalidState, s.State())
		s.notifyStart(err)
		return err
	}
	if s.OnStateChange != nil {
		s.OnStateChange(StateStarting)
	}

	defer func() {
		if runErr != nil && !s.isClose() {
			s.setState(StateError)
			s.reportError(runErr)
		}
		s.close()
	}()

	mode := int32(ffaudio.ModeCapture)
	if s.Mode == CaptureSpeaker {
		mode = ffaudio.ModeLoopBack
	}
	audio := ffaudio.NewFFAudio()
	audio.Writer = s
	audio.OnError = s.reportError
	if err := audio.Open(&ffaudio.Config{
		AppName:          "xaux_gui",
		DeviceName:       s.DeviceName,
		BufferLengthMsec: s.BufferLengthMsec,
	}, mode); err != nil {
		s.notifyStart(err)
		return err
	}

	converter, err := ffaudio.NewPCMConverter(
		ffaudio.PCMFormat{
			SampleRate: audio.SampleRate,
			Channels:   audio.Channels,
			Format:     audio.Format,
		},
		ffaudio.PCMFormat{
			SampleRate: s.SampleRate,
			Channels:   s.Channels,
			Format:     ffaudio.FormatInt16,
		},
	)
	if err != nil {
		_ = audio.Close()
		s.notifyStart(err)
		return err
	}

	var recording *wavWriter
	if s.RecordFilePath != "" {
		recording, err = newWAVWriter(s.RecordFilePath, s.SampleRate, s.Channels, s.BitsPerSample)
		if err != nil {
			_ = audio.Close()
			s.notifyStart(err)
			return err
		}
	}

	s.ioMu.Lock()
	s.converter = converter
	s.recording = recording
	s.ioMu.Unlock()
	s.audioMu.Lock()
	s.audio = audio
	s.audioMu.Unlock()

	if s.isClose() {
		return s.abortStartup(audio)
	}
	if err := audio.Start(); err != nil {
		s.notifyStart(err)
		return err
	}
	if s.isClose() || !s.transitionState(StateStarting, StateRunning) {
		return s.abortStartup(audio)
	}
	s.notifyStart(nil)

	runErr = audio.ListenAndRun(s.ctx)
	return runErr
}

func (s *SoundCap) abortStartup(audio *ffaudio.FFAudio) error {
	_ = audio.Close()
	s.ioMu.Lock()
	s.closeRecordingLocked()
	s.ioMu.Unlock()
	err := fmt.Errorf("%w: capture closed during startup", ErrInvalidState)
	s.notifyStart(err)
	return err
}

// Pause stops device consumption before ending the current ASR utterance.
func (s *SoundCap) Pause() error {
	if s.isClose() {
		return fmt.Errorf("%w: Pause after Close", ErrInvalidState)
	}
	if !s.transitionState(StateRunning, StatePaused) {
		if s.State() == StatePaused {
			return nil
		}
		return fmt.Errorf("%w: Pause from %d", ErrInvalidState, s.State())
	}
	audio := s.getAudio()
	if audio == nil {
		err := fmt.Errorf("%w: audio stream is unavailable", ErrInvalidState)
		s.setState(StateError)
		s.close()
		return err
	}
	if err := audio.Pause(); err != nil {
		s.setState(StateError)
		s.reportError(err)
		s.close()
		return err
	}
	if s.isClose() {
		return fmt.Errorf("%w: Pause after Close", ErrInvalidState)
	}

	s.ioMu.Lock()
	var err error
	if s.asrClient != nil {
		err = s.asrClient.End()
	}
	s.ioMu.Unlock()
	if err != nil {
		if s.isClose() {
			return err
		}
		s.setState(StateError)
		err = fmt.Errorf("pause ASR session: %w", err)
		s.reportError(err)
		s.close()
		return err
	}
	return nil
}

// Resume starts a new ASR utterance before restarting device consumption.
func (s *SoundCap) Resume() error {
	if s.isClose() || !s.transitionState(StatePaused, StateResuming) {
		return fmt.Errorf("%w: Resume from %d", ErrInvalidState, s.State())
	}
	audio := s.getAudio()
	if audio == nil {
		err := fmt.Errorf("%w: audio stream is unavailable", ErrInvalidState)
		s.setState(StateError)
		s.close()
		return err
	}

	s.ioMu.Lock()
	var err error
	if s.asrClient != nil {
		err = s.asrClient.Start(x.StartConfig{
			SampleRate:    int32(s.SampleRate),
			BitsPerSample: int32(s.BitsPerSample),
		})
	}
	s.ioMu.Unlock()
	if err != nil {
		if s.isClose() {
			return err
		}
		s.setState(StateError)
		s.reportError(fmt.Errorf("resume ASR session: %w", err))
		s.close()
		return err
	}
	if s.isClose() {
		return fmt.Errorf("%w: Resume after Close", ErrInvalidState)
	}

	if err := audio.Resume(); err != nil {
		if !s.isClose() {
			s.setState(StateError)
			s.reportError(err)
			s.close()
		}
		return err
	}
	if s.isClose() || !s.transitionState(StateResuming, StateRunning) {
		return fmt.Errorf("%w: Resume after Close", ErrInvalidState)
	}
	return nil
}

func (s *SoundCap) closeRecordingLocked() {
	if s.recording == nil {
		return
	}
	if err := s.recording.Close(); err != nil {
		s.reportError(err)
	}
	s.recording = nil
}

func (s *SoundCap) close() {
	s.closeOnce.Do(func() {
		atomic.StoreInt32(&s.closed, 1)
		s.cancel()
		if audio := s.getAudio(); audio != nil {
			if err := audio.Close(); err != nil {
				s.reportError(err)
			}
		}

		s.ioMu.Lock()
		s.closeRecordingLocked()
		if s.asrClient != nil {
			_ = s.asrClient.End()
			s.asrClient.Close()
		}
		s.ioMu.Unlock()
		if s.State() != StateError {
			s.setState(StateStopped)
		}
	})
}

func (s *SoundCap) isClose() bool {
	return atomic.LoadInt32(&s.closed) == 1
}

func (s *SoundCap) Close() {
	s.close()
}

// DumpRecordAudio is retained for API compatibility. Recording is now streamed
// directly to disk; Close normally finalizes it, and this method is idempotent.
func (s *SoundCap) DumpRecordAudio() {
	s.ioMu.Lock()
	s.closeRecordingLocked()
	s.ioMu.Unlock()
}
