package gui

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
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
	listItems []*ListItem
	mu        sync.Mutex
	running   int32
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

func (s *SpeechRTTrans) Start(onClosed func()) {
	s.window = fyne.CurrentApp().NewWindow("xaux 聆听")
	s.window.Resize(fyne.NewSize(800, 256))
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
	s.window.SetContent(container.NewBorder(nil, nil, nil, nil, s.list))
	s.window.Show()
	s.window.SetOnClosed(func() {
		fmt.Println("quit ......")
		if onClosed != nil {
			onClosed()
		}
		atomic.StoreInt32(&s.running, 0)
	})
	atomic.StoreInt32(&s.running, 1)
}

func (s *SpeechRTTrans) Release() {
	atomic.AddInt32(&WinCnt, -1)
}
