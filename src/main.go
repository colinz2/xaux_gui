package main

import (
	"os"
	"xaux_gui/src/gui"
)

func init() {
	os.Setenv("FYNE_SCALE", "1.0")
}

func main() {
	mainWin := gui.NewMainWin()
	mainWin.Run()
}
