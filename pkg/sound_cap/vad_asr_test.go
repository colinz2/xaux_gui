package sound_cap

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"xaux_gui/pkg/ffaudio"

	"github.com/realzhangm/xaux/pkg/x"
)

type sampleDetector struct{}

func (sampleDetector) Process(samples []int16) (bool, error) { return samples[0] != 0, nil }
func (sampleDetector) Close() error                          { return nil }

type vadTestClient struct {
	audio  bytes.Buffer
	ends   int
	closes int
}

func (c *vadTestClient) Send(p []byte) error { _, err := c.audio.Write(p); return err }
func (c *vadTestClient) End() error          { c.ends++; return nil }
func (c *vadTestClient) Close()              { c.closes++ }

func vadFrames(count int, speech bool) []byte {
	audio := make([]byte, count*vadFrameBytes)
	if speech {
		for i := 0; i < len(audio); i += 2 {
			binary.LittleEndian.PutUint16(audio[i:], 1)
		}
	}
	return audio
}

func TestVADASROnsetPreRollSilenceAndReconnect(t *testing.T) {
	var clients []*vadTestClient
	v := &vadASR{
		detector: sampleDetector{},
		connect: func() (vadASRClient, error) {
			client := &vadTestClient{}
			clients = append(clients, client)
			return client, nil
		},
	}
	process := func(audio []byte) {
		t.Helper()
		if err := v.process(audio); err != nil {
			t.Fatal(err)
		}
	}
	process(vadFrames(500, false))
	process(vadFrames(vadStartFrames-1, true))
	process(vadFrames(1, false))
	if len(clients) != 0 {
		t.Fatal("silence or short noise opened a cloud session")
	}
	process(vadFrames(vadStartFrames, true))
	if len(clients) != 1 || clients[0].audio.Len() != vadPreRollBytes {
		t.Fatal("speech must open one session with 400 ms pre-roll")
	}
	process(vadFrames(vadEndFrames-1, false))
	if clients[0].ends != 0 {
		t.Fatal("short pause prematurely ended ASR")
	}
	process(vadFrames(1, true))
	process(vadFrames(vadEndFrames, false))
	if clients[0].ends != 1 || clients[0].closes != 1 {
		t.Fatal("2 seconds of silence must finalize and disconnect ASR")
	}
	process(vadFrames(500, false))
	if len(clients) != 1 {
		t.Fatal("continued silence reopened ASR")
	}
	// Arbitrary byte boundaries must not lose frames.
	speech := vadFrames(vadStartFrames, true)
	for _, b := range speech {
		process([]byte{b})
	}
	if len(clients) != 2 {
		t.Fatal("new speech did not create a fresh session")
	}
	if err := v.end(); err != nil {
		t.Fatal(err)
	}
	if clients[1].ends != 1 || clients[1].closes != 1 {
		t.Fatal("stop must finalize the active session")
	}
}

func TestVADASRWorkerDoesNotBlockCaptureDuringConnect(t *testing.T) {
	connecting := make(chan struct{})
	release := make(chan struct{})
	client := &vadTestClient{}
	v := startVADASR(sampleDetector{}, func() (vadASRClient, error) {
		close(connecting)
		<-release
		return client, nil
	}, func(err error) { t.Errorf("worker: %v", err) })
	if err := v.write(vadFrames(vadStartFrames, true)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connecting:
	case <-time.After(time.Second):
		close(release)
		_ = v.close()
		t.Fatal("ASR did not start connecting")
	}
	err := v.write(vadFrames(10, true))
	close(release)
	if err != nil {
		t.Error(err)
	}
	if err := v.close(); err != nil {
		t.Fatal(err)
	}
	if client.audio.Len() != (vadStartFrames+10)*vadFrameBytes {
		t.Fatalf("audio during connection was lost: %d bytes", client.audio.Len())
	}
}

func TestVADASRFinishResetsOnsetAndFlushesTail(t *testing.T) {
	var clients []*vadTestClient
	v := startVADASR(sampleDetector{}, func() (vadASRClient, error) {
		client := &vadTestClient{}
		clients = append(clients, client)
		return client, nil
	}, func(err error) { t.Error(err) })
	defer func() {
		if err := v.close(); err != nil {
			t.Error(err)
		}
	}()
	write := func(audio []byte) {
		t.Helper()
		if err := v.write(audio); err != nil {
			t.Fatal(err)
		}
	}
	finish := func() {
		t.Helper()
		if err := v.finish(); err != nil {
			t.Fatal(err)
		}
	}
	write(vadFrames(vadStartFrames-1, true))
	finish()
	write(vadFrames(1, true))
	finish()
	if len(clients) != 0 {
		t.Fatal("pause did not reset the consecutive speech counter")
	}
	write(vadFrames(vadEndFrames, false))
	finish()
	if len(clients) != 0 {
		t.Fatal("silence after resume reopened ASR")
	}
	speech := vadFrames(vadStartFrames, true)
	tail := []byte{2, 0}
	write(append(speech, tail...))
	finish()
	if len(clients) != 1 || clients[0].audio.Len() != len(speech)+len(tail) ||
		clients[0].ends != 1 || clients[0].closes != 1 {
		t.Fatal("pause lost the partial final frame or failed to close ASR")
	}
}

func TestVADASRBacklogAndConnectionFailure(t *testing.T) {
	want := errors.New("connection failed")
	reported := make(chan error, 1)
	v := startVADASR(sampleDetector{}, func() (vadASRClient, error) {
		return nil, want
	}, func(err error) { reported <- err })
	if err := v.write(make([]byte, vadQueueBytes+1)); err == nil {
		t.Fatal("unbounded audio queue accepted")
	}
	if err := v.write(vadFrames(vadStartFrames, true)); err != nil {
		t.Fatal(err)
	}
	if err := v.close(); !errors.Is(err, want) {
		t.Fatalf("close lost connection failure: %v", err)
	}
	select {
	case err := <-reported:
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	default:
		t.Fatal("connection failure was not surfaced")
	}
}

func TestVADASRPreservesFullRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complete.wav")
	capture, err := NewSoundCap(t.Context(), &Config{Mode: CaptureSpeaker, RecordFilePath: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	capture.vadASR = startVADASR(sampleDetector{}, func() (vadASRClient, error) {
		t.Error("silence must not connect to ASR")
		return &vadTestClient{}, nil
	}, func(err error) { t.Error(err) })
	format := ffaudio.PCMFormat{SampleRate: 16000, Channels: 1, Format: ffaudio.FormatInt16}
	capture.converter, err = ffaudio.NewPCMConverter(format, format)
	if err != nil {
		t.Fatal(err)
	}
	capture.recording, err = newWAVWriter(path, 16000, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	capture.setState(StateRunning)
	audio := vadFrames(100, false)
	if _, err := capture.Write(audio); err != nil {
		t.Fatal(err)
	}
	capture.Close()
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) != 44+len(audio) || !bytes.Equal(wav[44:], audio) {
		t.Fatal("VAD changed the original recording")
	}
}

func TestVADASRWithoutRecording(t *testing.T) {
	capture, err := NewSoundCap(t.Context(), &Config{Mode: CaptureSpeaker}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	client := &vadTestClient{}
	capture.vadASR = startVADASR(sampleDetector{}, func() (vadASRClient, error) {
		return client, nil
	}, func(err error) { t.Error(err) })
	format := ffaudio.PCMFormat{SampleRate: 16000, Channels: 1, Format: ffaudio.FormatInt16}
	capture.converter, err = ffaudio.NewPCMConverter(format, format)
	if err != nil {
		t.Fatal(err)
	}
	capture.setState(StateRunning)
	audio := vadFrames(vadStartFrames, true)
	if _, err := capture.Write(audio); err != nil {
		t.Fatal(err)
	}
	capture.Close()
	if !bytes.Equal(client.audio.Bytes(), audio) || client.ends != 1 || client.closes != 1 {
		t.Fatal("subtitle-only capture must send speech and finalize ASR without a WAV writer")
	}
}

func TestVADASRProtocolReconnectAndUniqueIndices(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, _, err := udp.ReadFrom(buffer); err != nil {
				return
			}
		}
	}()
	var servers sync.WaitGroup
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			servers.Add(1)
			go func() {
				defer servers.Done()
				defer conn.Close()
				decoder, encoder := json.NewDecoder(conn), json.NewEncoder(conn)
				started := false
				for {
					var request x.AllRequest
					if err := decoder.Decode(&request); err != nil {
						return
					}
					switch request.Cmd {
					case x.CmdStart:
						response := x.StartResponse{Type: x.TypeRspStart, UDPPort: int32(udp.LocalAddr().(*net.UDPAddr).Port)}
						if started {
							response.Error.Msg = "doubao ASR session already started"
						}
						started = true
						if err := encoder.Encode(response); err != nil {
							return
						}
					case x.CmdEnd:
						responses := []x.AllResponse{
							{Type: x.TypeSentenceStart, StartResult: x.StartResult{Index: 0}},
							{Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "speech"}},
							{Type: x.TypeRspEnd},
						}
						for _, response := range responses {
							if err := encoder.Encode(response); err != nil {
								return
							}
						}
					}
				}
			}()
		}
	}()
	var mu sync.Mutex
	var indices []int
	connector := vadASRConnector(listener.Addr().String(), func(response *x.AllResponse) error {
		if response.Type == x.TypeSentenceEnd {
			mu.Lock()
			indices = append(indices, response.Result.Index)
			mu.Unlock()
		}
		return nil
	})
	v := startVADASR(sampleDetector{}, connector, func(err error) { t.Error(err) })
	for range 2 {
		if err := v.write(vadFrames(vadStartFrames, true)); err != nil {
			t.Error(err)
		}
		if err := v.write(vadFrames(vadEndFrames, false)); err != nil {
			t.Error(err)
		}
	}
	if err := v.close(); err != nil {
		t.Error(err)
	}
	listener.Close()
	<-serverDone
	servers.Wait()
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(indices, []int{0, 1}) {
		t.Fatalf("reopened sessions reused sentence indices: %v", indices)
	}
}
