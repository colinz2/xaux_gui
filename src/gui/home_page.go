package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func newHomePage(audioDevices fyne.CanvasObject) fyne.CanvasObject {
	topGap := canvas.NewRectangle(color.NRGBA{})
	topGap.SetMinSize(fyne.NewSize(0, 4))

	audioCard := widget.NewCard("", "", audioDevices)
	return container.NewPadded(container.NewVBox(topGap, audioCard))
}
