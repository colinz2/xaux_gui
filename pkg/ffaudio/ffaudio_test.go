package ffaudio

import (
	"context"
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

	ffa := NewFFAudio()
	if err = ffa.OpenLoopBack(&Config{
		DeviceName: devs.PlaybackDefault,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.TODO(), time.Second*6000)
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
