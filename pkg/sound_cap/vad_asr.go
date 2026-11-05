package sound_cap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"xaux_gui/pkg/vad"

	"github.com/realzhangm/xaux/pkg/x"
)

const (
	vadFrameBytes   = vad.FrameSamples * 2
	vadPreRollBytes = 25 * vadFrameBytes // 400 ms
	vadStartFrames  = 6                  // 96 ms of consecutive speech
	vadEndFrames    = 125                // 2 seconds of consecutive silence
	vadQueueBytes   = 16000 * 2 * 10
)

type vadASRClient interface {
	Send([]byte) error
	End() error
	Close()
}

type vadASRMessage struct {
	audio []byte
	end   chan error
}

// Only this worker owns the detector and network session. Device callbacks
// enqueue bounded PCM copies; cloud connection/finalization never block capture.
type vadASR struct {
	detector vad.Detector
	connect  func() (vadASRClient, error)
	report   func(error)
	messages chan vadASRMessage
	done     chan struct{}
	queued   atomic.Int64
	errMu    sync.Mutex
	err      error

	client  vadASRClient
	pending []byte
	preRoll []byte
	voiced  int
	silent  int
}

func newVADASR(address string, callback x.AsrResultCallBack, report func(error)) (*vadASR, error) {
	if callback == nil {
		return nil, fmt.Errorf("VAD ASR callback is nil")
	}
	detector, err := vad.New()
	if err != nil {
		return nil, err
	}
	return startVADASR(detector, vadASRConnector(address, callback), report), nil
}

func vadASRConnector(address string, callback x.AsrResultCallBack) func() (vadASRClient, error) {
	var indexMu sync.Mutex
	nextIndex := 0
	return func() (vadASRClient, error) {
		indexMu.Lock()
		base := nextIndex
		indexMu.Unlock()
		client, err := x.NewClient(address, func(response *x.AllResponse) error {
			copy := *response
			indexMu.Lock()
			switch copy.Type {
			case x.TypeSentenceStart:
				copy.StartResult.Index += base
				nextIndex = max(nextIndex, copy.StartResult.Index+1)
			case x.TypeRecognizing, x.TypeSentenceEnd:
				copy.Result.Index += base
				nextIndex = max(nextIndex, copy.Result.Index+1)
			}
			indexMu.Unlock()
			return callback(&copy)
		})
		if err != nil {
			return nil, fmt.Errorf("%w: connect VAD ASR: %w", ErrASRProxyUnavailable, err)
		}
		if err := client.Start(x.StartConfig{SampleRate: 16000, BitsPerSample: 16}); err != nil {
			client.Close()
			return nil, fmt.Errorf("%w: start VAD ASR: %w", ErrASRProxyUnavailable, err)
		}
		return client, nil
	}
}

func startVADASR(detector vad.Detector, connect func() (vadASRClient, error), report func(error)) *vadASR {
	v := &vadASR{
		detector: detector, connect: connect, report: report,
		messages: make(chan vadASRMessage, 128), done: make(chan struct{}),
	}
	go v.run()
	return v
}

func (v *vadASR) failure() error {
	v.errMu.Lock()
	defer v.errMu.Unlock()
	return v.err
}

func (v *vadASR) fail(err error) {
	v.errMu.Lock()
	first := v.err == nil
	if first {
		v.err = err
	}
	v.errMu.Unlock()
	if first {
		v.report(err)
	}
}

// Calls are serialized by SoundCap.ioMu, including finish and close.
func (v *vadASR) write(audio []byte) error {
	if err := v.failure(); err != nil {
		return err
	}
	if v.queued.Load()+int64(len(audio)) > vadQueueBytes {
		return fmt.Errorf("VAD ASR audio backlog exceeds 10 seconds")
	}
	copy := append([]byte(nil), audio...)
	v.queued.Add(int64(len(copy)))
	select {
	case v.messages <- vadASRMessage{audio: copy}:
		return nil
	default:
		v.queued.Add(-int64(len(copy)))
		return fmt.Errorf("VAD ASR audio queue is full")
	}
}

func (v *vadASR) finish() error {
	ack := make(chan error, 1)
	v.messages <- vadASRMessage{end: ack}
	return <-ack
}

func (v *vadASR) close() error {
	err := v.finish()
	close(v.messages)
	<-v.done
	return errors.Join(err, v.failure())
}

func (v *vadASR) run() {
	defer close(v.done)
	defer func() {
		if err := v.detector.Close(); err != nil {
			v.fail(err)
		}
	}()
	for message := range v.messages {
		if message.end != nil {
			err := v.end()
			if err != nil {
				v.fail(err)
			}
			message.end <- errors.Join(err, v.failure())
			continue
		}
		if v.failure() == nil {
			if err := v.process(message.audio); err != nil {
				v.fail(err)
				if err := v.end(); err != nil {
					log.Printf("finish failed VAD ASR: %v", err)
				}
			}
		}
		v.queued.Add(-int64(len(message.audio)))
	}
}

func (v *vadASR) process(audio []byte) error {
	v.pending = append(v.pending, audio...)
	for len(v.pending) >= vadFrameBytes {
		frame := v.pending[:vadFrameBytes]
		samples := make([]int16, vad.FrameSamples)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(frame[i*2:]))
		}
		speech, err := v.detector.Process(samples)
		if err != nil {
			return err
		}
		if v.client == nil {
			v.preRoll = append(v.preRoll, frame...)
			if len(v.preRoll) > vadPreRollBytes {
				v.preRoll = v.preRoll[len(v.preRoll)-vadPreRollBytes:]
			}
			if speech {
				v.voiced++
			} else {
				v.voiced = 0
			}
			if v.voiced >= vadStartFrames {
				v.client, err = v.connect()
				if err != nil {
					return err
				}
				log.Printf("VAD ASR opened: speech_ms=96 pre_roll_ms=%d", len(v.preRoll)/32)
				if err := v.client.Send(v.preRoll); err != nil {
					return fmt.Errorf("send VAD pre-roll: %w", err)
				}
				v.preRoll = nil
				v.silent = 0
			}
		} else {
			if err := v.client.Send(frame); err != nil {
				return fmt.Errorf("send VAD audio: %w", err)
			}
			if speech {
				v.silent = 0
			} else {
				v.silent++
			}
			if v.silent >= vadEndFrames {
				// Remove this frame before ending; remaining queued PCM belongs
				// to the next session and must survive finalization.
				v.pending = v.pending[vadFrameBytes:]
				if err := v.endSession(); err != nil {
					return err
				}
				log.Printf("VAD ASR closed: silence_ms=2000")
				continue
			}
		}
		v.pending = v.pending[vadFrameBytes:]
	}
	return nil
}

func (v *vadASR) endSession() error {
	v.voiced, v.silent = 0, 0
	v.preRoll = nil
	if v.client == nil {
		return nil
	}
	client := v.client
	v.client = nil
	defer client.Close()
	if err := client.End(); err != nil {
		return fmt.Errorf("finish VAD ASR session: %w", err)
	}
	return nil
}

func (v *vadASR) end() error {
	var sendErr error
	if v.client != nil && len(v.pending) > 0 {
		sendErr = v.client.Send(v.pending)
	}
	v.pending = nil
	return errors.Join(sendErr, v.endSession())
}
