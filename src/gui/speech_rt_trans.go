package gui

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
	"github.com/sqweek/dialog"
)

var speechWindowOpen atomic.Bool

type transcriptItem struct {
	deviceName   string
	isMicrophone bool
	timeString   string
	content      string
	final        bool
}

type speechRTTrans struct {
	binding     binding.UntypedList
	list        *widget.List
	window      fyne.Window
	listItems   []*transcriptItem
	running     atomic.Bool
	releaseOnce sync.Once
	paused      bool
}

func newSpeechRTTrans() *speechRTTrans {
	if !speechWindowOpen.CompareAndSwap(false, true) {
		return nil
	}
	return &speechRTTrans{binding: binding.NewUntypedList()}
}

func (s *speechRTTrans) updateASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if capture == nil || response == nil {
		return
	}
	if response.Type != x.TypeRecognizing && response.Type != x.TypeSentenceEnd {
		return
	}
	fyne.Do(func() {
		s.applyASRResult(capture, response)
	})
}

func (s *speechRTTrans) applyASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if !s.running.Load() {
		return
	}

	resultIndex := response.Result.Index
	if capture.resultIndex == resultIndex &&
		capture.writeListIndex >= 0 && capture.writeListIndex < len(s.listItems) {
		index := capture.writeListIndex
		item := s.listItems[index]
		item.content = response.Result.Result
		item.final = !response.Result.Interim
		if err := s.binding.SetValue(index, item); err != nil {
			log.Printf("update transcript item: %v", err)
			return
		}
	} else {
		item := &transcriptItem{
			deviceName:   capture.deviceName,
			isMicrophone: capture.isMicrophone,
			timeString:   time.Now().Format("2006-01-02 03:04:05"),
			content:      response.Result.Result,
			final:        !response.Result.Interim,
		}
		capture.writeListIndex = len(s.listItems)
		s.listItems = append(s.listItems, item)
		if err := s.binding.Append(item); err != nil {
			s.listItems = s.listItems[:capture.writeListIndex]
			capture.writeListIndex = -1
			log.Printf("append transcript item: %v", err)
			return
		}
	}
	capture.resultIndex = resultIndex
	s.list.Refresh()
	s.list.ScrollToBottom()
}

func (s *speechRTTrans) start(onClosed func(), onPause func(), onResume func()) {
	s.window = fyne.CurrentApp().NewWindow("正在聆听")
	s.window.Resize(fyne.NewSize(1024, 256+128+24))
	s.list = widget.NewListWithData(s.binding,
		func() fyne.CanvasObject {
			timeLabel := widget.NewLabel("")
			contentEntry := widget.NewMultiLineEntry()
			contentEntry.Wrapping = fyne.TextWrapBreak
			contentEntry.TextStyle.Bold = true
			header := container.NewHBox(widget.NewIcon(mytheme.ResSpeakerIcon), timeLabel)
			return container.NewVBox(header, contentEntry)
		},
		func(dataItem binding.DataItem, object fyne.CanvasObject) {
			value, err := dataItem.(binding.Untyped).Get()
			if err != nil {
				log.Printf("read transcript item: %v", err)
				return
			}
			item := value.(*transcriptItem)
			row := object.(*fyne.Container)
			header := row.Objects[0].(*fyne.Container)
			icon := header.Objects[0].(*widget.Icon)
			if item.isMicrophone {
				icon.SetResource(mytheme.ResMicrophoneIcon)
			} else {
				icon.SetResource(mytheme.ResSpeakerIcon)
			}
			header.Objects[1].(*widget.Label).SetText(fmt.Sprintf("%s %s", item.deviceName, item.timeString))
			contentEntry := row.Objects[1].(*widget.Entry)
			contentEntry.SetText(item.content)
			if item.final {
				contentEntry.Disable()
			} else {
				contentEntry.Enable()
			}
		},
	)

	endButton := widget.NewButtonWithIcon("停止识别", mytheme.ResEnd, nil)
	endButton.OnTapped = func() {
		if s.paused {
			endButton.SetIcon(mytheme.ResEnd)
			endButton.SetText("停止识别")
			s.window.SetTitle("正在聆听")
			onResume()
			s.paused = false
		} else {
			endButton.SetIcon(mytheme.ResBegin)
			endButton.SetText("开始识别")
			s.window.SetTitle("聆听停止")
			onPause()
			s.paused = true
		}
	}

	microphoneProgress := widget.NewProgressBar()
	speakerProgress := widget.NewProgressBar()
	controls := container.NewHBox(microphoneProgress, speakerProgress, endButton, layout.NewSpacer())
	s.window.SetContent(container.NewBorder(controls, nil, nil, nil, s.list))
	s.window.SetCloseIntercept(func() {
		if !dialog.Message("退出识别吗？").Title("确认窗").YesNo() {
			return
		}
		if onClosed != nil {
			onClosed()
		}
		s.close()
	})
	s.window.SetOnClosed(s.release)
	s.window.Show()
	s.running.Store(true)
}

func (s *speechRTTrans) close() {
	s.running.Store(false)
	s.window.Close()
}

func (s *speechRTTrans) release() {
	s.releaseOnce.Do(func() {
		speechWindowOpen.Store(false)
	})
}
