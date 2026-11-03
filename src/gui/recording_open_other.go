//go:build !windows

package gui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

func openRecordingFile(path string) error {
	uri, err := url.Parse(storage.NewFileURI(path).String())
	if err != nil {
		return err
	}
	return fyne.CurrentApp().OpenURL(uri)
}
