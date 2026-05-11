package cli

import (
	"fmt"
	"io"
	"os"
)

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func colorize(w io.Writer, color string, value string) string {
	if !colorEnabled(w) {
		return value
	}
	return color + value + colorReset
}

func heading(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, colorize(w, colorBold+colorCyan, title))
}

func statusText(w io.Writer, status string) string {
	switch status {
	case "ok":
		return colorize(w, colorGreen, "ok")
	case "warn":
		return colorize(w, colorYellow, "warn")
	case "fail":
		return colorize(w, colorRed, "fail")
	default:
		return status
	}
}

func dim(w io.Writer, value string) string {
	return colorize(w, colorDim, value)
}
