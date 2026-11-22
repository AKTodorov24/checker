//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procBeep                       = kernel32.NewProc("Beep")
	procSetConsoleTitle            = kernel32.NewProc("SetConsoleTitleW")
	procSetConsoleCP               = kernel32.NewProc("SetConsoleOutputCP")
	procGetScreenBufferInfo        = kernel32.NewProc("GetConsoleScreenBufferInfo")
	savedInputMode          uint32 = 0
	inputModeSaved                 = false
)

type coord struct {
	X int16
	Y int16
}

type smallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type screenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func setupTerminal() {
	handle, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	err = syscall.GetConsoleMode(handle, &mode)
	if err != nil {
		return
	}
	procSetConsoleMode.Call(uintptr(handle), uintptr(mode|0x0004|0x0001))
	procSetConsoleCP.Call(uintptr(65001))
}

func termSize() (int, int) {
	handle, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return 100, 30
	}
	var info screenBufferInfo
	ok, _, _ := procGetScreenBufferInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 100, 30
	}
	width := int(info.Window.Right-info.Window.Left) + 1
	height := int(info.Window.Bottom-info.Window.Top) + 1
	return width, height
}

func pauseQuickEdit() {
	handle, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	err = syscall.GetConsoleMode(handle, &mode)
	if err != nil {
		return
	}
	savedInputMode = mode
	inputModeSaved = true
	newMode := (mode &^ 0x0040) | 0x0080
	procSetConsoleMode.Call(uintptr(handle), uintptr(newMode))
}

func restoreQuickEdit() {
	if !inputModeSaved {
		return
	}
	handle, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err != nil {
		return
	}
	procSetConsoleMode.Call(uintptr(handle), uintptr(savedInputMode))
	inputModeSaved = false
}

func beep() {
	procBeep.Call(uintptr(1400), uintptr(80))
	procBeep.Call(uintptr(2000), uintptr(110))
}

func setTitle(title string) {
	ptr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procSetConsoleTitle.Call(uintptr(unsafe.Pointer(ptr)))
}

func openFolder(path string) error {
	return exec.Command("explorer", path).Start()
}
