//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

func setupTerminal() {
}

func termSize() (int, int) {
	width := 100
	height := 40
	columns, err := strconv.Atoi(os.Getenv("COLUMNS"))
	if err == nil && columns > 0 {
		width = columns
	}
	lines, err := strconv.Atoi(os.Getenv("LINES"))
	if err == nil && lines > 0 {
		height = lines
	}
	return width, height
}

func pauseQuickEdit() {
}

func restoreQuickEdit() {
}

func beep() {
	fmt.Fprint(os.Stderr, "\a")
}

func setTitle(title string) {
	fmt.Print("\033]0;" + title + "\007")
}

func openFolder(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
