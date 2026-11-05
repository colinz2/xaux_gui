package sound_cap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/realzhangm/xaux/pkg/x"
)

// singleStartProxy mimics the real ASR proxy: it binds one upstream session
// per TCP connection, and once that session has been started it can never be
// started again — exactly the doubao backend behavior that broke resume.
type singleStartProxy struct {
	listener net.Listener

	mu          sync.Mutex
	connections int
	started     int
}

func newSingleStartProxy(t *testing.T) *singleStartProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &singleStartProxy{listener: listener}
	t.Cleanup(func() { _ = listener.Close() })
	go proxy.serve()
	return proxy
}

func (p *singleStartProxy) addr() string {
	return p.listener.Addr().String()
}

func (p *singleStartProxy) counts() (connections, started int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connections, p.started
}

func (p *singleStartProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		p.connections++
		p.mu.Unlock()
		go p.handle(conn)
	}
}

func (p *singleStartProxy) handle(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	sessionStarted := false
	for {
		request := x.AllRequest{}
		if err := decoder.Decode(&request); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		switch request.Cmd {
		case x.CmdStart:
			response := x.StartResponse{Type: x.TypeRspStart, UDPPort: 1}
			if sessionStarted {
				response.Error = x.Error{Msg: "doubao ASR session already started"}
			} else {
				sessionStarted = true
				p.mu.Lock()
				p.started++
				p.mu.Unlock()
			}
			if err := encoder.Encode(&response); err != nil {
				return
			}
		case x.CmdEnd:
			if err := encoder.Encode(&x.EndResponse{Type: x.TypeRspEnd}); err != nil {
				return
			}
		default:
			return
		}
	}
}

func discardASRResult(*x.AllResponse) error { return nil }

// TestResumeReconnectsASRSession reproduces the pause/resume failure: the
// proxy cannot restart a finished session on the same connection, so resuming
// must open a fresh connection instead of calling Start on the old one.
func TestResumeReconnectsASRSession(t *testing.T) {
	proxy := newSingleStartProxy(t)

	capture, err := NewSoundCap(context.Background(), &Config{
		ProxyAddr: proxy.addr(),
		Mode:      CaptureSpeaker,
	}, discardASRResult)
	if err != nil {
		t.Fatalf("NewSoundCap() with fake proxy: %v", err)
	}
	defer capture.Close()

	// Mirror the ASR part of Pause: finish the current utterance/session.
	capture.ioMu.Lock()
	err = capture.asrClient.End()
	capture.ioMu.Unlock()
	if err != nil {
		t.Fatalf("end ASR session: %v", err)
	}

	// Mirror the ASR part of Resume: it must start a brand-new session.
	capture.ioMu.Lock()
	err = capture.restartASRClientLocked()
	capture.ioMu.Unlock()
	if err != nil {
		t.Fatalf("restart ASR session on resume: %v", err)
	}
	if capture.asrClient == nil {
		t.Fatal("resume left the capture without an ASR client")
	}

	connections, started := proxy.counts()
	if connections != 2 || started != 2 {
		t.Fatalf("proxy saw %d connections and %d started sessions, want 2 and 2",
			connections, started)
	}

	// A second pause/resume cycle must keep working the same way.
	capture.ioMu.Lock()
	err = capture.asrClient.End()
	capture.ioMu.Unlock()
	if err != nil {
		t.Fatalf("end ASR session again: %v", err)
	}
	capture.ioMu.Lock()
	err = capture.restartASRClientLocked()
	capture.ioMu.Unlock()
	if err != nil {
		t.Fatalf("restart ASR session on second resume: %v", err)
	}
	if connections, started = proxy.counts(); connections != 3 || started != 3 {
		t.Fatalf("proxy saw %d connections and %d started sessions, want 3 and 3",
			connections, started)
	}
}

func TestRestartASRClientLockedRecordingOnly(t *testing.T) {
	capture, err := NewSoundCap(context.Background(), &Config{Mode: CaptureMicrophone}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()

	capture.ioMu.Lock()
	err = capture.restartASRClientLocked()
	capture.ioMu.Unlock()
	if err != nil {
		t.Fatalf("restart without ASR client: %v", err)
	}
	if capture.asrClient != nil {
		t.Fatal("recording-only capture gained an ASR client on resume")
	}
}
