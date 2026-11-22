package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const appName = "Lightning Checker"
const appVersion = "2.1"
const logoWidth = 69
const logoTop = 2
const logoLeft = 3
const contentRow = 10

var logoLines = []string{
	"██╗     ██╗ ██████╗ ██╗  ██╗████████╗███╗   ██╗██╗███╗   ██╗ ██████╗ ",
	"██║     ██║██╔════╝ ██║  ██║╚══██╔══╝████╗  ██║██║████╗  ██║██╔════╝ ",
	"██║     ██║██║  ███╗███████║   ██║   ██╔██╗ ██║██║██╔██╗ ██║██║  ███╗",
	"██║     ██║██║   ██║██╔══██║   ██║   ██║╚██╗██║██║██║╚██╗██║██║   ██║",
	"███████╗██║╚██████╔╝██║  ██║   ██║   ██║ ╚████║██║██║ ╚████║╚██████╔╝",
	"╚══════╝╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═══╝╚═╝╚═╝  ╚═══╝ ╚═════╝ ",
}

type rgb struct {
	r int
	g int
	b int
}

var (
	colorBlack = rgb{0, 0, 0}
	colorWhite = rgb{255, 255, 255}
	colorGlow  = rgb{255, 212, 59}
	colorBolt  = rgb{255, 243, 191}
	gradStart  = rgb{165, 243, 252}
	gradMiddle = rgb{96, 165, 250}
	gradEnd    = rgb{167, 139, 250}
)

type logoFrame struct {
	reveal float64
	center int
	flash  float64
	shine  float64
}

func clamp(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func blend(a rgb, b rgb, t float64) rgb {
	t = clamp(t)
	var c rgb
	c.r = a.r + int(float64(b.r-a.r)*t)
	c.g = a.g + int(float64(b.g-a.g)*t)
	c.b = a.b + int(float64(b.b-a.b)*t)
	return c
}

func fg(c rgb) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", c.r, c.g, c.b)
}

func gradientAt(t float64) rgb {
	t = clamp(t)
	if t <= 0.5 {
		return blend(gradStart, gradMiddle, t*2)
	}
	return blend(gradMiddle, gradEnd, (t-0.5)*2)
}

func fullFrame() logoFrame {
	var f logoFrame
	f.reveal = -1
	f.shine = -1000
	return f
}

func renderLogo(f logoFrame) string {
	var sb strings.Builder
	for row, line := range logoLines {
		sb.WriteString(fmt.Sprintf("\033[%d;%dH", logoTop+row, logoLeft))
		col := 0
		for _, ch := range line {
			if ch == ' ' {
				sb.WriteByte(' ')
				col++
				continue
			}
			c := gradientAt(float64(col) / float64(logoWidth-1))
			if ch != '█' {
				c = blend(colorBlack, c, 0.55)
			}
			if f.reveal >= 0 {
				dist := math.Abs(float64(col-f.center)) + float64(row)*0.8
				if dist > f.reveal {
					sb.WriteByte(' ')
					col++
					continue
				}
				edge := f.reveal - dist
				if edge < 7 {
					c = blend(colorBolt, c, edge/7)
				}
			}
			if f.flash > 0 {
				c = blend(c, colorWhite, f.flash)
			}
			shineDist := math.Abs(float64(col+row*2) - f.shine)
			if shineDist < 7 {
				c = blend(c, colorWhite, (1-shineDist/7)*0.8)
			}
			sb.WriteString(fg(c))
			sb.WriteRune(ch)
			col++
		}
	}
	sb.WriteString(reset)
	return sb.String()
}

type segment struct {
	text  string
	color string
}

func subtitleSegments() []segment {
	return []segment{
		{strings.Repeat("─", 26), cBorder},
		{"  ", ""},
		{"C H E C K E R", cBrand + bold},
		{"  ", ""},
		{strings.Repeat("─", 21), cBorder},
		{" v" + appVersion, cMuted},
	}
}

func renderSubtitle(visible int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\033[%d;%dH", logoTop+len(logoLines), logoLeft))
	count := 0
	for _, seg := range subtitleSegments() {
		sb.WriteString(seg.color)
		for _, ch := range seg.text {
			if count >= visible {
				sb.WriteString(reset)
				return sb.String()
			}
			sb.WriteRune(ch)
			count++
		}
		sb.WriteString(reset)
	}
	return sb.String()
}

func boltColumns(center int) []int {
	return []int{center + 5, center + 3, center + 1, center + 4, center + 2, center, center - 2, center + 1, center - 1}
}

func renderBolt(center int, c rgb, visible bool) string {
	cols := boltColumns(center)
	var sb strings.Builder
	for i := 0; i < len(cols)-1; i++ {
		left := cols[i]
		right := cols[i+1]
		if right < left {
			left = cols[i+1]
			right = cols[i]
		}
		width := right - left + 2
		body := strings.Repeat("█", width)
		glow := "░"
		if !visible {
			body = strings.Repeat(" ", width)
			glow = " "
		}
		sb.WriteString(fmt.Sprintf("\033[%d;%dH", i+1, logoLeft+left-1))
		sb.WriteString(fg(blend(colorBlack, c, 0.5)) + glow)
		sb.WriteString(fg(c) + body)
		sb.WriteString(fg(blend(colorBlack, c, 0.5)) + glow + reset)
	}
	return sb.String()
}

func printHeader() {
	clearScreen()
	fmt.Print(renderLogo(fullFrame()))
	fmt.Print(renderSubtitle(1000))
	fmt.Printf("\033[%d;1H", contentRow)
}

func playIntro() {
	clearScreen()
	fmt.Print(hideCursor)
	center := logoWidth / 2

	fmt.Print(renderBolt(center, colorGlow, true))
	time.Sleep(70 * time.Millisecond)
	fmt.Print(renderBolt(center, colorGlow, false))
	time.Sleep(50 * time.Millisecond)
	fmt.Print(renderBolt(center, colorWhite, true))
	time.Sleep(90 * time.Millisecond)
	fmt.Print(renderBolt(center, colorWhite, false))

	frame := fullFrame()
	frame.center = center
	for radius := 0.0; radius <= float64(logoWidth)/2+14; radius += 2.5 {
		frame.reveal = radius
		fmt.Print(renderLogo(frame))
		time.Sleep(16 * time.Millisecond)
	}

	frame = fullFrame()
	flashes := []float64{0.9, 0, 0.55, 0, 0.25, 0}
	waits := []int{45, 55, 40, 70, 30, 0}
	for i, amount := range flashes {
		frame.flash = amount
		fmt.Print(renderLogo(frame))
		time.Sleep(time.Duration(waits[i]) * time.Millisecond)
	}

	for visible := 0; visible <= logoWidth; visible += 3 {
		fmt.Print(renderSubtitle(visible))
		time.Sleep(12 * time.Millisecond)
	}
	fmt.Print(renderSubtitle(1000))

	frame = fullFrame()
	for pos := -15.0; pos <= float64(logoWidth)+25; pos += 3 {
		frame.shine = pos
		fmt.Print(renderLogo(frame))
		time.Sleep(14 * time.Millisecond)
	}
	fmt.Print(renderLogo(fullFrame()))
	fmt.Printf("\033[%d;1H", contentRow)
	fmt.Print(showCursor)
}

type shimmer struct {
	stop chan struct{}
	done chan struct{}
}

func startShimmer(cfg *Config) *shimmer {
	if !cfg.Animations {
		return nil
	}
	_, height := termSize()
	if height < 30 {
		return nil
	}
	s := &shimmer{}
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop()
	return s
}

func (s *shimmer) draw(frame logoFrame) {
	fmt.Print("\0337" + hideCursor + renderLogo(frame) + "\0338" + showCursor)
}

func (s *shimmer) loop() {
	defer close(s.done)
	for {
		timer := time.NewTimer(3500 * time.Millisecond)
		select {
		case <-s.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		frame := fullFrame()
		for pos := -15.0; pos <= float64(logoWidth)+25; pos += 2.5 {
			select {
			case <-s.stop:
				s.draw(fullFrame())
				return
			default:
			}
			frame.shine = pos
			s.draw(frame)
			time.Sleep(28 * time.Millisecond)
		}
		s.draw(fullFrame())
	}
}

func (s *shimmer) end() {
	if s == nil {
		return
	}
	close(s.stop)
	<-s.done
}
