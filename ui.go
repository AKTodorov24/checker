package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	reset      = "\033[0m"
	bold       = "\033[1m"
	hideCursor = "\033[?25l"
	showCursor = "\033[?25h"
	cText      = "\033[38;2;229;231;235m"
	cMuted     = "\033[38;2;120;128;142m"
	cBorder    = "\033[38;2;75;85;99m"
	cAccent    = "\033[38;2;96;165;250m"
	cBrand     = "\033[38;2;255;212;59m"
	cGood      = "\033[38;2;81;207;102m"
	cBad       = "\033[38;2;255;107;107m"
	cWarn      = "\033[38;2;252;196;25m"
)

const boxWidth = logoWidth

var stdin = bufio.NewReader(os.Stdin)

func clearScreen() {
	fmt.Print("\033[H\033[2J\033[3J")
}

func exitClean() {
	fmt.Print(reset + showCursor + "\n")
	setTitle(appName)
	os.Exit(0)
}

func visibleLen(s string) int {
	count := 0
	inEscape := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		count++
	}
	return count
}

func padRight(s string, width int) string {
	missing := width - visibleLen(s)
	if missing <= 0 {
		return s
	}
	return s + strings.Repeat(" ", missing)
}

func boxTop(title string) string {
	inner := boxWidth - 2
	used := 3 + visibleLen(title)
	line := "  " + cBorder + "┌─ " + reset + cBrand + bold + title + reset + " "
	line += cBorder + strings.Repeat("─", inner-used) + "┐" + reset
	return line
}

func boxRow(content string) string {
	inner := boxWidth - 2
	return "  " + cBorder + "│" + reset + padRight("  "+content, inner) + cBorder + "│" + reset
}

func boxDivider() string {
	return "  " + cBorder + "├" + strings.Repeat("─", boxWidth-2) + "┤" + reset
}

func boxBottom() string {
	return "  " + cBorder + "└" + strings.Repeat("─", boxWidth-2) + "┘" + reset
}

func menuRow(key string, label string, value string) string {
	keyText := cAccent + bold + padRight(key, 2) + reset
	return boxRow(keyText + "  " + cText + padRight(label, 16) + reset + value)
}

func onOff(value bool) string {
	if value {
		return cGood + "● On" + reset
	}
	return cMuted + "○ Off" + reset
}

func promptText(prompt string) {
	fmt.Print("  " + cBrand + bold + "»" + reset + " " + cText + prompt + reset + " ")
}

func readLine() string {
	line, err := stdin.ReadString('\n')
	if err == io.EOF && len(line) == 0 {
		exitClean()
	}
	return strings.TrimSpace(line)
}

func ask(prompt string) string {
	promptText(prompt)
	return readLine()
}

func askAnimated(prompt string, cfg *Config) string {
	promptText(prompt)
	s := startShimmer(cfg)
	answer := readLine()
	s.end()
	return answer
}

func waitEnter() {
	ask(cMuted + "Press Enter to continue" + reset)
}

func badge(kind string) string {
	switch kind {
	case "hit":
		return "\033[48;2;81;207;102m\033[38;2;17;24;39m" + bold + " HIT  " + reset
	case "taken":
		return cMuted + "TAKEN " + reset
	case "skip":
		return cMuted + "SKIP  " + reset
	case "fail":
		return cBad + bold + "FAIL  " + reset
	case "warn":
		return cWarn + bold + "WARN  " + reset
	case "ok":
		return cGood + bold + "OK    " + reset
	}
	return cAccent + bold + "INFO  " + reset
}

func logLine(kind string, text string) string {
	stamp := cBorder + time.Now().Format("15:04:05") + reset
	return "  " + stamp + "  " + badge(kind) + " " + text
}

func note(kind string, text string) {
	fmt.Println(logLine(kind, text))
}

func formatDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func formatInt(n int64) string {
	text := strconv.FormatInt(n, 10)
	negative := false
	if strings.HasPrefix(text, "-") {
		negative = true
		text = text[1:]
	}
	var sb strings.Builder
	for i, ch := range text {
		if i > 0 && (len(text)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(ch)
	}
	if negative {
		return "-" + sb.String()
	}
	return sb.String()
}

func mask(value string) string {
	if value == "" || value == skipValue {
		return cMuted + "○ not set" + reset
	}
	if len(value) <= 12 {
		return cGood + "● saved" + reset
	}
	return cGood + "● " + value[:4] + "…" + value[len(value)-4:] + reset
}

func cleanPath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	if strings.HasPrefix(value, "& ") {
		value = strings.TrimPrefix(value, "& ")
		value = strings.Trim(value, "\"'")
	}
	return value
}
