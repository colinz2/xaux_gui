package ffaudio

import (
	"context"
	"os"
	"testing"
	"time"
)

func init() {
	Init()
}

func TestListDev(t *testing.T) {
	playBack, err := ListDevPlayback()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(playBack)
}

func TestFFAudio_ListenAndRunLoopBack(t *testing.T) {
	devs, err := GetDevPlaybackAndCapture()
	if err != nil {
		t.Fatal(err)
	}

	t.Log(devs)
	t.Log(devs.PlayBackDevNameList[0])

	ffa := NewFFAudio()
	if err = ffa.OpenPlayBack(&Config{
		DeviceName: devs.PlaybackDefault,
	}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile("10s_16k.pcm", os.O_TRUNC|os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		t.Fatal(err)
	}
	ffa.Writer = f
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.TODO(), time.Second*10)
	go func() {
		t.Log("---")
		<-ctx.Done()
		cancel()
		t.Log("------")
	}()

	if err = ffa.ListenAndRun(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("ok")
}
