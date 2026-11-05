//go:build !windows

package gui

import "fyne.io/fyne/v2"

// Without native positioning the subtitle strip stays a normal window, so the
// user can move it with the window decorations instead.
const subtitleUsesSplashShell = false

func moveSubtitleToBottom(fyne.Window) {}

func moveSubtitleBy(fyne.Window, float32, float32) {}
