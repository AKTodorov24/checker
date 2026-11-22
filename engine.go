package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const proxiesPath = "proxies.txt"
const leftPath = "users_left.txt"
const maxErrors = 8
const maxLimited = 200

var running atomic.Bool

type job struct {
	name    string
	errors  int
	limited int
}

func readLines(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, string(rune(0xFEFF)))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func loadUsers(path string) []string {
	seen := map[string]bool{}
	var users []string
	for _, line := range readLines(path) {
		name := strings.ToLower(strings.TrimPrefix(line, "@"))
		if seen[name] {
			continue
		}
		seen[name] = true
		users = append(users, name)
	}
	return users
}

func countInvalid(users []string, platform *Platform) int {
	count := 0
	for _, name := range users {
		if !platform.Valid(name) {
			count++
		}
	}
	return count
}

func parseProxy(line string) *url.URL {
	if strings.Contains(line, "://") {
		parsed, err := url.Parse(line)
		if err != nil || parsed.Host == "" {
			return nil
		}
		return parsed
	}
	if strings.Contains(line, "@") {
		parsed, err := url.Parse("http://" + line)
		if err != nil {
			return nil
		}
		return parsed
	}
	parts := strings.Split(line, ":")
	if len(parts) == 2 {
		parsed, err := url.Parse("http://" + line)
		if err != nil {
			return nil
		}
		return parsed
	}
	if len(parts) == 4 {
		parsed := &url.URL{}
		parsed.Scheme = "http"
		parsed.Host = parts[0] + ":" + parts[1]
		parsed.User = url.UserPassword(parts[2], parts[3])
		return parsed
	}
	return nil
}

func loadProxies() []*url.URL {
	seen := map[string]bool{}
	var proxies []*url.URL
	for _, line := range readLines(proxiesPath) {
		parsed := parseProxy(line)
		if parsed == nil {
			continue
		}
		key := parsed.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		proxies = append(proxies, parsed)
	}
	return proxies
}

func askFields(cfg *Config, fields []Field, force bool) {
	for _, field := range fields {
		if !force && cfg.Secrets[field.Key] != "" {
			continue
		}
		fmt.Println()
		fmt.Println(boxTop(strings.ToUpper(field.Label)))
		for _, line := range field.Help {
			fmt.Println(boxRow(cMuted + line + reset))
		}
		fmt.Println(boxBottom())
		prompt := "Paste " + field.Label + ":"
		if field.Optional {
			prompt = "Paste " + field.Label + cMuted + " (Enter to skip)" + reset + cText + ":"
		}
		value := strings.Trim(ask(prompt), "\"' ")
		if value == "" {
			value = skipValue
		}
		cfg.Secrets[field.Key] = value
		if field.Key == webhookField.Key && value != skipValue {
			err := testWebhook(value)
			if err != nil {
				note("fail", "Webhook test failed: "+err.Error())
				cfg.Secrets[field.Key] = skipValue
			} else {
				note("ok", "Webhook works, a test message was sent.")
			}
		}
	}
	cfg.save()
}

func createHitFile(platform *Platform) (*os.File, string, error) {
	err := os.MkdirAll("hits", 0755)
	if err != nil {
		return nil, "", err
	}
	name := platform.Key + "_" + time.Now().Format("2006-01-02_15-04-05") + ".txt"
	path := filepath.Join("hits", name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, "", err
	}
	return file, path, nil
}

func runHeader(cfg *Config, platform *Platform, users int, proxies int, workers int) {
	clearScreen()
	order := "in order"
	if cfg.Random {
		order = "random"
	}
	proxyText := "direct"
	if proxies > 0 {
		proxyText = formatInt(int64(proxies)) + " proxies"
	}
	sep := cBorder + "  │  " + reset
	line := "  " + cBrand + bold + "LIGHTNING" + reset + " " + cText + bold + "CHECKER" + reset + sep
	line += platform.Color + bold + platform.Name + reset + sep
	line += cText + formatInt(int64(users)) + reset + cMuted + " names" + reset + sep
	line += cMuted + order + reset + sep
	line += cMuted + fmt.Sprint(workers) + " workers" + reset + sep
	line += cMuted + proxyText + reset
	fmt.Println()
	fmt.Println(line)
	fmt.Println("  " + cBorder + strings.Repeat("─", boxWidth) + reset)
}

func writeLeft(users []string, finished *sync.Map) int {
	var left []string
	for _, name := range users {
		_, ok := finished.Load(name)
		if !ok {
			left = append(left, name)
		}
	}
	if len(left) == 0 {
		return 0
	}
	data := strings.Join(left, "\n") + "\n"
	err := os.WriteFile(leftPath, []byte(data), 0644)
	if err != nil {
		return 0
	}
	return len(left)
}

func runProxyTest(proxies []*url.URL, platform *Platform) []testedProxy {
	total := len(proxies)
	alive := testProxies(proxies, platform.TestURL, func(done int, okCount int) {
		bar := progressBar(float64(done)/float64(total), 24)
		text := fmt.Sprintf("\r\033[2K  %s  %s%s%s%s/%s tested  ·  %s%s%s alive", bar, cText, formatInt(int64(done)), reset, cMuted, formatInt(int64(total)), cGood, formatInt(int64(okCount)), reset)
		fmt.Print(text + reset)
	})
	fmt.Print("\r\033[2K")
	return alive
}

func proxyURLs(list []testedProxy) []*url.URL {
	var urls []*url.URL
	for _, item := range list {
		urls = append(urls, item.url)
	}
	return urls
}

func runSniper(cfg *Config) {
	platform := findPlatform(cfg.Mode)
	users := loadUsers(cfg.ListPath)
	proxies := loadProxies()

	printHeader()
	if len(users) == 0 {
		note("fail", "No usernames in "+cfg.ListPath+". Add one per line, or pick a list in the menu.")
		fmt.Println()
		waitEnter()
		return
	}

	askFields(cfg, platform.Fields, false)

	if platform.Verify != nil {
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 15*time.Second)
		message, err := platform.Verify(verifyCtx, newClient(nil, 1), cfg)
		verifyCancel()
		if err != nil {
			note("fail", platform.Name+": "+err.Error())
			for _, field := range platform.Fields {
				delete(cfg.Secrets, field.Key)
			}
			cfg.save()
			note("info", "Saved login cleared. Start again to enter a new one.")
			fmt.Println()
			waitEnter()
			return
		}
		note("ok", message)
	}

	if len(proxies) > 0 && cfg.TestProxy {
		note("info", fmt.Sprintf("Testing %s proxies against %s...", formatInt(int64(len(proxies))), platform.Name))
		alive := runProxyTest(proxies, platform)
		if len(alive) == 0 {
			note("fail", "None of the proxies work. Fix proxies.txt or turn off the proxy test (menu P).")
			fmt.Println()
			waitEnter()
			return
		}
		note("ok", fmt.Sprintf("%s/%s proxies work · avg %dms · fastest used first", formatInt(int64(len(alive))), formatInt(int64(len(proxies))), averageLatency(alive).Milliseconds()))
		proxies = proxyURLs(alive)
		time.Sleep(700 * time.Millisecond)
	}

	if cfg.Random {
		rand.Shuffle(len(users), func(i int, j int) {
			users[i], users[j] = users[j], users[i]
		})
	}

	hitFile, hitPath, err := createHitFile(platform)
	if err != nil {
		note("fail", "Could not create the hits file: "+err.Error())
		waitEnter()
		return
	}

	workers := cfg.workers(len(proxies))
	pool := newPool(proxies, cfg.PerProxy, workers, platform.LimitHeaders)

	runHeader(cfg, platform, len(users), len(proxies), workers)
	invalid := countInvalid(users, platform)
	if invalid > 0 {
		note("info", fmt.Sprintf("%s names don't fit %s's rules and will be skipped.", formatInt(int64(invalid)), platform.Name))
	}
	if platform.LimitHeaders {
		note("info", "Exact rate limits: reading "+platform.Name+"'s limit headers, no wasted requests.")
	} else {
		note("info", "Adaptive rate limits: each proxy speeds up until it gets limited, then backs off.")
	}
	if platform.Key == "discord" && cfg.secret("discord_token") != "" && len(proxies) > 0 {
		note("warn", "With a token the limit is per account, so proxies won't add speed. Skip the token to use them.")
	}
	note("info", "Hits → "+cText+hitPath+reset+cMuted+"   ·   Ctrl+C stops and saves what's left"+reset)
	fmt.Println()

	webhook := startWebhook(cfg.secret(webhookField.Key), platform)
	st := &stats{}
	st.total = int64(len(users))
	st.start = time.Now()
	disp := newDisplay(st, platform, pool)
	var finished sync.Map
	var lastGlobalLog atomic.Int64

	running.Store(true)
	pauseQuickEdit()
	ctx, cancel := context.WithCancel(context.Background())
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-interrupt:
			disp.stopping.Store(true)
			disp.log("warn", "Stopping, finishing open requests...")
			cancel()
		case <-ctx.Done():
		}
	}()

	jobs := make(chan job, len(users))
	for _, name := range users {
		jobs <- job{name: name}
	}

	var fatalNote atomic.Value
	var fileLock sync.Mutex

	finish := func(name string) {
		finished.Store(name, true)
		if st.checked.Add(1) >= st.total {
			cancel()
		}
	}

	giveUp := func(j job, reason string) {
		st.failed.Add(1)
		disp.log("fail", cText+j.name+reset+cMuted+"  · gave up ("+reason+")"+reset)
		finish(j.name)
	}

	work := func(j job) {
		if !platform.Valid(j.name) {
			st.invalid.Add(1)
			if cfg.ShowTaken {
				disp.log("skip", cMuted+j.name+"  · invalid for "+platform.Name+reset)
			}
			finish(j.name)
			return
		}
		px, err := pool.acquire(ctx)
		if err != nil {
			return
		}
		start := time.Now()
		result := platform.Check(ctx, px.client, cfg, j.name)
		cancelled := ctx.Err() != nil
		pool.release(px, result, time.Since(start), cancelled)
		if cancelled && (result.Status == StatusError || result.Status == StatusLimited) {
			return
		}
		switch result.Status {
		case StatusAvailable:
			st.hits.Add(1)
			fileLock.Lock()
			_, writeErr := hitFile.WriteString(j.name + "\n")
			fileLock.Unlock()
			if writeErr != nil {
				disp.log("fail", "Could not write hit "+j.name+": "+writeErr.Error())
			}
			disp.log("hit", cGood+bold+j.name+reset)
			if cfg.Sound {
				hitSound()
			}
			webhook.push(j.name)
			finish(j.name)
		case StatusTaken:
			st.taken.Add(1)
			if cfg.ShowTaken {
				text := cMuted + j.name
				if result.Note != "" {
					text += "  · " + result.Note
				}
				disp.log("taken", text+reset)
			}
			finish(j.name)
		case StatusInvalid:
			st.invalid.Add(1)
			if cfg.ShowTaken {
				disp.log("skip", cMuted+j.name+"  · "+result.Note+reset)
			}
			finish(j.name)
		case StatusFatal:
			fatalNote.Store(result.Note)
			cancel()
		case StatusLimited:
			st.retries.Add(1)
			if result.Global {
				now := time.Now().UnixMilli()
				previous := lastGlobalLog.Load()
				if now-previous > 3000 && lastGlobalLog.CompareAndSwap(previous, now) {
					disp.log("warn", fmt.Sprintf("Account rate limited · resuming in %.1fs", result.Wait.Seconds()))
				}
			}
			j.limited++
			if j.limited >= maxLimited {
				giveUp(j, "rate limited too often")
				return
			}
			jobs <- j
		case StatusError:
			st.retries.Add(1)
			j.errors++
			if j.errors >= maxErrors {
				giveUp(j, result.Note)
				return
			}
			jobs <- j
		}
	}

	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-jobs:
					work(j)
				}
			}
		}()
	}

	go disp.run()
	group.Wait()
	cancel()
	<-watcherDone
	signal.Stop(interrupt)
	disp.stop()
	restoreQuickEdit()
	webhook.close()
	running.Store(false)

	hitFile.Close()
	if st.hits.Load() == 0 {
		os.Remove(hitPath)
	}

	left := 0
	if st.checked.Load() < st.total {
		left = writeLeft(users, &finished)
	}
	fatalText, hasFatal := fatalNote.Load().(string)
	if hasFatal {
		for _, field := range platform.Fields {
			delete(cfg.Secrets, field.Key)
		}
		cfg.save()
	}
	printSummary(st, platform, pool, hitPath, left, fatalText, hasFatal)
	setTitle(appName)
	waitEnter()
}

func summaryRow(label string, value string) string {
	return boxRow(cMuted + padRight(label, 14) + reset + value)
}

func printSummary(st *stats, platform *Platform, pool *proxyPool, hitPath string, left int, fatal string, hasFatal bool) {
	elapsed := time.Since(st.start)
	title := "FINISHED"
	if left > 0 || hasFatal {
		title = "STOPPED"
	}
	average := 0.0
	if elapsed.Seconds() > 0 {
		average = float64(st.checked.Load()) / elapsed.Seconds()
	}
	health := pool.health()
	fmt.Println()
	fmt.Println(boxTop(title + "  ·  " + strings.ToUpper(platform.Name)))
	fmt.Println(summaryRow("Hits", cGood+bold+formatInt(st.hits.Load())+reset))
	fmt.Println(summaryRow("Taken", cText+formatInt(st.taken.Load())+reset))
	fmt.Println(summaryRow("Skipped", cText+formatInt(st.invalid.Load())+reset))
	fmt.Println(summaryRow("Failed", cText+formatInt(st.failed.Load())+reset))
	fmt.Println(summaryRow("Checked", cText+formatInt(st.checked.Load())+cMuted+" / "+formatInt(st.total)+reset))
	fmt.Println(summaryRow("Time", cText+formatDuration(elapsed)+cMuted+fmt.Sprintf("   avg %.1f/s · peak %.1f/s", average, st.peak())+reset))
	limitText := cText + formatInt(pool.rateLimits.Load()) + reset + cMuted + " rate limits · " + reset + cText + formatInt(pool.proxyErrors.Load()) + reset + cMuted + " connection errors" + reset
	fmt.Println(summaryRow("Requests", limitText))
	if !pool.direct {
		proxyText := cGood + formatInt(int64(health.total-health.dead)) + reset + cMuted + " working · " + reset + cBad + formatInt(int64(health.dead)) + reset + cMuted + " dead" + reset
		fmt.Println(summaryRow("Proxies", proxyText))
	}
	if st.hits.Load() > 0 || left > 0 || hasFatal {
		fmt.Println(boxDivider())
	}
	if st.hits.Load() > 0 {
		fmt.Println(summaryRow("Hits file", cAccent+hitPath+reset))
	}
	if left > 0 {
		fmt.Println(summaryRow("Not checked", cAccent+leftPath+reset+cMuted+"  ("+formatInt(int64(left))+" names)"+reset))
	}
	if hasFatal {
		fmt.Println(summaryRow("Error", cBad+fatal+reset))
		fmt.Println(summaryRow("", cMuted+"Saved login cleared, you'll be asked again."+reset))
	}
	fmt.Println(boxBottom())
	if left > 0 {
		note("info", "To continue later, set the list file to "+leftPath+" (menu option 5).")
	}
	fmt.Println()
}
