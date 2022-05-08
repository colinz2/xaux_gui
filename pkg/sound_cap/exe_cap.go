package sound_cap

import (
	"bytes"
	"context"
	"fmt"
	"github.com/realzhangm/xaux/pkg/common/doa"
	"github.com/realzhangm/xaux/pkg/x"
	"os"
	"os/exec"
	"sync/atomic"
)

type SoundCap struct {
	asrClient     *x.Client
	cmd           *exec.Cmd
	buff          *bytes.Buffer
	channelNum    int
	sampleRate    int
	bitsPerSample int
	isClosed      int32
	stdInChan     chan string
	startDoneChan chan error
	Config
}

type Config struct {
	ProxyAddr      string
	ExeDevParam    string // --dev-loopback=1
	RecordFilePath string
}

func NewSoundCap(ctx context.Context, config *Config, asrCb x.AsrResultCallBack) (*SoundCap, error) {
	if len(config.ProxyAddr) == 0 {
		panic("len of config ProxyAddr == 0")
	}
	if len(config.ExeDevParam) == 0 {
		panic("len of config ExeDevParam == 0")
	}

	sc := &SoundCap{
		stdInChan:     make(chan string),
		startDoneChan: make(chan error, 1),
		buff:          &bytes.Buffer{},
		channelNum:    1,
		sampleRate:    16000,
		bitsPerSample: 16,
		isClosed:      0,
		Config:        *config,
	}

	var err error = nil
	if sc.asrClient, err = x.NewClient(sc.ProxyAddr, asrCb); err != nil {
		return nil, err
	}
	if err = sc.asrClient.Start(x.StartConfig{
		SampleRate:    int32(sc.sampleRate),
		BitsPerSample: int32(sc.bitsPerSample)},
	); err != nil {
		return nil, err
	}

	sc.cmd = exec.CommandContext(ctx, "fmedia",
		fmt.Sprintf("%s", sc.ExeDevParam),
		"--record", "-o", "@stdout.wav",
		"--format=int16",
		"--channels=mono",
		"--capture-buffer=64",
		fmt.Sprintf("--rate=%d", sc.sampleRate),
	)

	doa.MustTrue(sc.cmd != nil, "sc.cmd is nil")
	sc.cmd.SysProcAttr = procAttrs
	sc.cmd.Stdout = sc
	sc.cmd.Stdin = sc
	return sc, nil
}

func (s SoundCap) getMillisecond(len int) int {
	bytesPerMilli := (s.sampleRate / 1000) * (s.bitsPerSample / 8) * s.channelNum
	if bytesPerMilli > len {
		return 0
	}
	return len / bytesPerMilli
}

func (s *SoundCap) Read(p []byte) (n int, err error) {
	cmd, ok := <-s.stdInChan
	if ok {
		copy(p, []byte(cmd))
		return len(cmd), nil
	}
	return 0, nil
}

func (s *SoundCap) Write(p []byte) (n int, err error) {
	if s.isClose() {
		return len(p), nil
	}

	dataLen := len(p)
	doa.MustTrue(dataLen%2 == 0, "sample not even")
	//fmt.Println("duration=", s.getMillisecond(dataLen))
	if s.channelNum == 2 {
		for i := 0; i < dataLen; i += 4 {
			monoData := p[i : i+2]
			_, err := s.buff.Write(monoData)
			if err != nil {
				panic(err)
			}
			err = s.asrClient.Send(monoData)
			if err != nil {
				panic(err)
			}
		}
	} else {
		_, err := s.buff.Write(p)
		if err != nil {
			panic(err)
		}
		err = s.asrClient.Send(p)
		if err != nil && err == x.ErrNoLooping {
			panic(err)
		}
	}
	return len(p), nil
}

func (s *SoundCap) StartDone() <-chan error {
	return s.startDoneChan
}

func (s *SoundCap) Run() error {
	defer s.close()

	err := s.cmd.Start()
	if err != nil {
		s.startDoneChan <- err
		return err
	}
	s.startDoneChan <- nil
	return s.cmd.Wait()
}

func (s *SoundCap) close() {
	if !atomic.CompareAndSwapInt32(&s.isClosed, 0, 1) {
		return
	}
	if s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil {
		//s.stdInChan <- "s"
		s.cmd.Process.Kill()
		fmt.Println("quit ffmedi")
	}
	if s.asrClient != nil {
		s.asrClient.End()
		s.asrClient.Close()
	}
}

// Resume TODO
func (s *SoundCap) Resume() error {
	if err := s.asrClient.Start(x.StartConfig{
		SampleRate:    int32(s.sampleRate),
		BitsPerSample: int32(s.bitsPerSample)},
	); err != nil {
		panic(err)
		return err
	}
	return nil
}

func (s *SoundCap) Pause() {
	err := s.asrClient.End()
	if err != nil {
		panic(err)
	}
}

func (s *SoundCap) isClose() bool {
	return atomic.LoadInt32(&s.isClosed) == 1
}

func (s *SoundCap) Close() {
	s.close()
}

func (s *SoundCap) DumpRecordAudio() {
	if len(s.RecordFilePath) == 0 {
		return
	}
	if err := os.WriteFile(s.RecordFilePath, s.buff.Bytes(), os.ModePerm); err != nil {
		panic(err)
	}
}
