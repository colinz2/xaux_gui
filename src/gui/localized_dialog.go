package gui

import (
	"unicode"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func showLocalizedConfirm(title, message string, callback func(bool), parent fyne.Window) {
	prompt := dialog.NewConfirm(title, message, callback, parent)
	prompt.SetConfirmText(tr("common.yes"))
	prompt.SetDismissText(tr("common.no"))
	prompt.Show()
}

func showLocalizedInformation(title, message string, parent fyne.Window) {
	prompt := dialog.NewInformation(title, message, parent)
	prompt.SetDismissText(tr("common.ok"))
	prompt.Show()
}

func showLocalizedError(err error, parent fyne.Window) {
	message := err.Error()
	if first, size := utf8.DecodeRuneInString(message); first != utf8.RuneError {
		message = string(unicode.ToUpper(first)) + message[size:]
	}
	content := widget.NewLabel(message)
	content.Alignment = fyne.TextAlignCenter
	content.Wrapping = fyne.TextWrapWord
	prompt := dialog.NewCustom(tr("common.error_title"), tr("common.ok"), content, parent)
	prompt.SetIcon(theme.ErrorIcon())
	width := fyne.Min(widget.NewLabel(message).MinSize().Width+32+theme.Padding()*2, 600)
	width = fyne.Min(width, parent.Canvas().Size().Width*0.9)
	prompt.Resize(fyne.NewSize(width, prompt.MinSize().Height))
	prompt.Show()
}
