package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type stats struct {
	total   int64
	checked atomic.Int64
	hits    atomic.Int64
	taken   atomic.Int64
	invalid atomic.Int64
	failed  atomic.Int64
	retries atomic.Int64
	start   time.Time
	top     float64
}

func (s *stats) peak() float64 {
	return s.top
}

type display struct {
	lines       chan string
	done        chan struct{}
	st          *stats
	platform    *Platform
	pool        *proxyPool
	stopping    atomic.Bool
	speed       float64
	lastCount   int64
	lastTime    time.Time
	footerShown bool
}

func newDisplay(st *stats, platform *Platform, pool *proxyPool) *display {
	d := &display{}
	d.lines = make(chan string, 4096)
	d.done = make(chan struct{})
	d.st = st
	d.platform = platform
	d.pool = pool
	d.lastTime = time.Now()
	return d
}

func (d *display) log(kind string, text string) {
	d.lines <- logLine(kind, text)
}

func (d *display) updateSpeed() {
	now := time.Now()
	elapsed := now.Sub(d.lastTime).Seconds()
	if elapsed < 1.0 {
		return
	}
	count := d.st.checked.Load()
	current := float64(count-d.lastCount) / elapsed
	if d.speed == 0 {
		d.speed = current
	} else {
		d.speed = d.speed*0.6 + current*0.4
	}
	d.lastCount = count
	d.lastTime = now
	if d.speed > d.st.top {
		d.st.top = d.speed
	}
}

func progressBar(fraction float64, width int) string {
	filled := int(fraction * float64(width))
	var sb strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			sb.WriteString(fg(gradientAt(float64(i) / float64(width))))
			sb.WriteString("█")
		} else {
			sb.WriteString(cBorder)
			sb.WriteString("░")
		}
	}
	sb.WriteString(reset)
	return sb.String()
}

func (d *display) footerLines() (string, string) {
	width, _ := termSize()
	checked := d.st.checked.Load()
	remaining := d.st.total - checked
	fraction := 0.0
	if d.st.total > 0 {
		fraction = float64(checked) / float64(d.st.total)
	}

	eta := "--:--:--"
	if d.speed > 0.01 {
		eta = formatDuration(time.Duration(float64(remaining)/d.speed) * time.Second)
	}
	health := d.pool.health()
	state := cGood + bold + "RUNNING" + reset
	if health.limited {
		wait := ""
		if health.nextIn > 0 {
			wait = fmt.Sprintf(" %.1fs", health.nextIn.Seconds())
		}
		state = cWarn + bold + "LIMITED" + wait + reset
	}
	if d.stopping.Load() {
		state = cWarn + bold + "STOPPING" + reset
	}
	barWidth := width - 44
	if barWidth > 40 {
		barWidth = 40
	}
	if barWidth < 10 {
		barWidth = 10
	}
	first := "  " + progressBar(fraction, barWidth) + "  " + cText + bold + fmt.Sprintf("%5.1f%%", fraction*100) + reset
	first += "   " + cMuted + "ETA " + reset + cText + eta + reset + "   " + state

	items := []string{
		cMuted + "hits " + reset + cGood + bold + formatInt(d.st.hits.Load()) + reset,
		cMuted + "checked " + reset + cText + formatInt(checked) + cMuted + "/" + formatInt(d.st.total) + reset,
		cMuted + "left " + reset + cText + formatInt(remaining) + reset,
		cMuted + "speed " + reset + cAccent + fmt.Sprintf("%.1f/s", d.speed) + reset,
		proxyItem(d.pool, health),
		cMuted + "retry " + reset + cWarn + formatInt(d.st.retries.Load()) + reset,
		cMuted + formatDuration(time.Since(d.st.start)) + reset,
	}
	second := " "
	sep := cBorder + "  │  " + reset
	for i, item := range items {
		candidate := second
		if i > 0 {
			candidate += sep
		} else {
			candidate += " "
		}
		candidate += item
		if visibleLen(candidate) >= width {
			break
		}
		second = candidate
	}
	return first, second
}

func (d *display) clearFooter() {
	if d.footerShown {
		fmt.Print("\r\033[2K\033[1A\033[2K\r")
		d.footerShown = false
	}
}

func (d *display) drawFooter() {
	d.updateSpeed()
	first, second := d.footerLines()
	d.clearFooter()
	fmt.Print(first + "\n" + second)
	d.footerShown = true
	title := fmt.Sprintf("%s | %s | hits %d | %d/%d | %.1f/s", appName, d.platform.Name, d.st.hits.Load(), d.st.checked.Load(), d.st.total, d.speed)
	setTitle(title)
}

func (d *display) run() {
	fmt.Print(hideCursor)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case line, ok := <-d.lines:
			if !ok {
				d.drawFooter()
				fmt.Print("\n" + showCursor)
				close(d.done)
				return
			}
			d.clearFooter()
			fmt.Print(line + "\n")
			d.drawFooter()
		case <-ticker.C:
			d.drawFooter()
		}
	}
}

func (d *display) stop() {
	close(d.lines)
	<-d.done
}

var lastBeep atomic.Int64

func hitSound() {
	now := time.Now().UnixMilli()
	previous := lastBeep.Load()
	if now-previous < 400 {
		return
	}
	if !lastBeep.CompareAndSwap(previous, now) {
		return
	}
	go beep()
}

func proxyItem(pool *proxyPool, health poolHealth) string {
	if pool.direct {
		if health.cooling > 0 {
			return cMuted + "direct " + reset + cWarn + "cooling" + reset
		}
		return cMuted + "direct" + reset
	}
	text := cMuted + "proxies " + reset + cGood + formatInt(int64(health.ready)) + reset + cMuted + " ok" + reset
	if health.cooling > 0 {
		text += cMuted + " · " + reset + cWarn + formatInt(int64(health.cooling)) + reset + cMuted + " cool" + reset
	}
	if health.dead > 0 {
		text += cMuted + " · " + reset + cBad + formatInt(int64(health.dead)) + reset + cMuted + " dead" + reset
	}
	return text
}
