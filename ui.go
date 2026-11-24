package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 256-color palette, same family as vamola-doctor.
const (
	cGreen = "38;5;114"
	cAmber = "38;5;179"
	cRed   = "38;5;203"
	cDim   = "38;5;244"
	cBold  = "1"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// colorOn is true when stdout is a terminal and NO_COLOR is not set.
var colorOn = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}()

func paint(code, s string) string {
	if !colorOn || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func visibleLen(s string) int { return utf8.RuneCountInString(ansiRE.ReplaceAllString(s, "")) }

func padRight(s string, n int) string {
	if d := n - visibleLen(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// stepOK prints a finished step: "  ✔ label  detail".
func stepOK(label, detail string) {
	fmt.Printf("  %s %s %s\n", paint(cGreen, "✔"), padRight(paint(cBold, label), 10), paint(cDim, detail))
}

// box draws a rounded box around lines in the given color.
func box(title string, lines []string, color string) string {
	inner := visibleLen(title) + 4
	for _, l := range lines {
		if n := visibleLen(l) + 4; n > inner {
			inner = n
		}
	}
	if inner < 46 {
		inner = 46
	}
	var b strings.Builder
	top := "─ " + paint(cBold+";"+color, title) + " "
	b.WriteString(paint(color, "╭─ ") + paint(cBold+";"+color, title) + paint(color, " "+strings.Repeat("─", inner-visibleLen(top))+"╮") + "\n")
	for _, l := range lines {
		b.WriteString(paint(color, "│") + padRight("  "+l, inner) + paint(color, "│") + "\n")
	}
	b.WriteString(paint(color, "╰"+strings.Repeat("─", inner)+"╯") + "\n")
	return b.String()
}
