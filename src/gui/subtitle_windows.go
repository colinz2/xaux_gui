//go:build windows

package gui

import (
	"unsafe"

	"fyne.io/fyne/v2"
	fynedriver "fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

// On Windows the subtitle strip uses a borderless splash shell because
// moveSubtitleToBottom docks it to the screen programmatically.
const subtitleUsesSplashShell = true

var (
	subtitleUser32       = windows.NewLazySystemDLL("user32.dll")
	procGetSystemMetrics = subtitleUser32.NewProc("GetSystemMetrics")
	procGetWindowRect    = subtitleUser32.NewProc("GetWindowRect")
	procSetWindowPos     = subtitleUser32.NewProc("SetWindowPos")
)

const (
	smCXScreen        = 0
	smCYScreen        = 1
	swpNoSize         = 0x0001
	swpNoActivate     = 0x0010
	subtitleBottomGap = 48 // physical pixels above the screen's bottom edge
)

// moveSubtitleToBottom centers the subtitle window horizontally, docks it near
// the bottom of the primary screen, and keeps it above other windows.
func moveSubtitleToBottom(window fyne.Window) {
	native, ok := window.(fynedriver.NativeWindow)
	if !ok {
		return
	}
	native.RunNative(func(ctx any) {
		windowsCtx, ok := ctx.(fynedriver.WindowsWindowContext)
		if !ok || windowsCtx.HWND == 0 {
			return
		}
		var rect struct{ left, top, right, bottom int32 }
		if ret, _, _ := procGetWindowRect.Call(windowsCtx.HWND, uintptr(unsafe.Pointer(&rect))); ret == 0 {
			return
		}
		screenWidth, _, _ := procGetSystemMetrics.Call(smCXScreen)
		screenHeight, _, _ := procGetSystemMetrics.Call(smCYScreen)
		x := (int32(screenWidth) - (rect.right - rect.left)) / 2
		y := int32(screenHeight) - (rect.bottom - rect.top) - subtitleBottomGap
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		setSubtitleWindowPos(windowsCtx.HWND, x, y)
	})
}

// moveSubtitleBy shifts the subtitle window by a drag delta given in logical
// units, letting the user place the strip anywhere.
func moveSubtitleBy(window fyne.Window, dx, dy float32) {
	native, ok := window.(fynedriver.NativeWindow)
	if !ok {
		return
	}
	scale := window.Canvas().Scale()
	if scale <= 0 {
		scale = 1
	}
	native.RunNative(func(ctx any) {
		windowsCtx, ok := ctx.(fynedriver.WindowsWindowContext)
		if !ok || windowsCtx.HWND == 0 {
			return
		}
		var rect struct{ left, top, right, bottom int32 }
		if ret, _, _ := procGetWindowRect.Call(windowsCtx.HWND, uintptr(unsafe.Pointer(&rect))); ret == 0 {
			return
		}
		setSubtitleWindowPos(windowsCtx.HWND, rect.left+int32(dx*scale), rect.top+int32(dy*scale))
	})
}

// setSubtitleWindowPos moves the window to physical coordinates and keeps it
// above other windows. Negative coordinates are valid on multi-monitor setups.
func setSubtitleWindowPos(hwnd uintptr, x, y int32) {
	const topMost = ^uintptr(0) // HWND_TOPMOST (-1)
	procSetWindowPos.Call(hwnd, topMost, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoActivate)
}
