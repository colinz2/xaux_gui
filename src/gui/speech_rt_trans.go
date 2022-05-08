package gui

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
	"github.com/sqweek/dialog"
	"sync"
	"sync/atomic"
	"time"
	"xaux_gui/src/mytheme"
)

var (
	WinCnt int32 = 0
)

type ListItem struct {
	ffDevName    string
	isMicroPhone bool
	timeString   string
	content      string
	final        bool
}

type SpeechRTTrans struct {
	binding   binding.UntypedList
	list      *widget.List
	window    fyne.Window
	listIndex int
	listItems []*ListItem // to show in UI
	mu        sync.Mutex
	running   int32
	pauseFlag int ///button
}

func NewSpeechRTTrans() *SpeechRTTrans {
	if !atomic.CompareAndSwapInt32(&WinCnt, 0, 1) {
		fmt.Printf("%d \n", WinCnt)
		return nil
	}

	return &SpeechRTTrans{
		binding:   binding.NewUntypedList(),
		list:      nil,
		listIndex: -1,
		listItems: make([]*ListItem, 0),
	}
}

// AsrUpdateUI 跟新 UI
func (s *SpeechRTTrans) AsrUpdateUI(arc *AsrRTSoundCap, rsp *x.AllResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if atomic.LoadInt32(&s.running) == 0 {
		return
	}
	if rsp.Type != x.TypeRecognizing && rsp.Type != x.TypeSentenceEnd {
		return
	}

	if arc.resultIndex == rsp.Result.Index {
		index := arc.writeListIndex
		s.listItems[index].content = rsp.Result.Result
		s.listItems[index].final = !rsp.Result.Interim
		s.binding.SetValue(index, s.listItems[index])
	} else {
		li := &ListItem{
			ffDevName:    arc.devName,
			isMicroPhone: arc.isMicroPhone,
			timeString:   time.Now().Format("2006-01-02 03:04:05"),
			content:      rsp.Result.Result,
			final:        !rsp.Result.Interim,
		}
		s.listItems = append(s.listItems, li)
		s.listIndex++
		arc.writeListIndex = s.listIndex
		s.binding.Append(li)
	}
	s.list.ScrollToBottom()
	s.list.Refresh()
	arc.resultIndex = rsp.Result.Index
}

func (s *SpeechRTTrans) Start(onClosed func(), onPause func(), onResume func()) {
	s.window = fyne.CurrentApp().NewWindow("正在聆听")
	s.window.Resize(fyne.NewSize(1024, 256+128+24))
	listObjs := make([]fyne.CanvasObject, 0)

	s.list = widget.NewListWithData(s.binding,
		func() fyne.CanvasObject {
			timeLabel := widget.NewLabel("")
			contentL := widget.NewMultiLineEntry()
			contentL.Wrapping = fyne.TextWrapBreak
			//contentL.FocusGained()
			//LineC := container.NewHBox(widget.NewIcon(mytheme.ResSpeakerIcon), contentL)
			//LineC.Resize(fyne.NewSize(780, 128))
			//c := container.NewGridWithRows(2, timeLabel, contentL)

			LineC := container.NewHBox(widget.NewIcon(mytheme.ResSpeakerIcon), timeLabel)
			c := container.NewVBox(LineC, contentL)
			listObjs = append(listObjs, c)
			return c
		},
		func(i binding.DataItem, o fyne.CanvasObject) {
			di := i.(binding.Untyped)
			iii, err := di.Get()
			if err != nil {
				return
			}
			li := iii.(*ListItem)

			container := o.(*fyne.Container)
			labelStr := fmt.Sprintf("%s %s", li.ffDevName, li.timeString)
			container1 := container.Objects[0].(*fyne.Container)
			if li.isMicroPhone {
				container1.Objects[0].(*widget.Icon).SetResource(mytheme.ResMicrophoneIcon)
			} else {
				container1.Objects[0].(*widget.Icon).SetResource(mytheme.ResSpeakerIcon)
			}
			container1.Objects[1].(*widget.Label).SetText(labelStr)
			contentEntry := container.Objects[1].(*widget.Entry)
			contentEntry.SetText(li.content)
			contentEntry.TextStyle.Bold = true
			contentEntry.FocusGained()
			if li.final {
				contentEntry.Disable()
			}
		})

	endButton := widget.NewButtonWithIcon("停止识别", mytheme.ResEnd, nil)
	endButton.OnTapped = func() {
		if s.pauseFlag == 0 {
			endButton.SetIcon(mytheme.ResBegin)
			endButton.SetText("开始识别")
			s.window.SetTitle("聆听停止")
			onPause()
			s.pauseFlag = 1
		} else {
			endButton.SetIcon(mytheme.ResEnd)
			endButton.SetText("停止识别")
			s.window.SetTitle("正在聆听")
			onResume()
			s.pauseFlag = 0
		}
	}

	p1 := widget.NewProgressBar()
	p2 := widget.NewProgressBar()
	l1 := container.NewHBox(p1, p2, endButton, layout.NewSpacer())

	s.window.SetContent(container.NewBorder(l1, nil, nil, nil, s.list))
	s.window.Show()
	s.window.SetCloseIntercept(func() {
		if dialog.Message("退出识别吗？").Title("确认窗").YesNo() {
			if onClosed != nil {
				onClosed()
			}
			s.Close()
		}
	})
	atomic.StoreInt32(&s.running, 1)
}

func (s *SpeechRTTrans) Close() {
	atomic.StoreInt32(&s.running, 0)
	s.window.Close()
}

func (s *SpeechRTTrans) Release() {
	atomic.AddInt32(&WinCnt, -1)
}
