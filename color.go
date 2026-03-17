package main

import (
	"fmt"
	"os"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[1;33m"
	colorCyan   = "\033[0;36m"
	colorGray   = "\033[0;37m"
)

var useColor = isTerminal()

func isTerminal() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func colorPrint(color, s string) {
	if useColor {
		fmt.Print(color + s + colorReset)
	} else {
		fmt.Print(s)
	}
}

func colorPrintf(color, format string, args ...any) {
	colorPrint(color, fmt.Sprintf(format, args...))
}
