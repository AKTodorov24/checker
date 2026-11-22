package main

import (
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	setupTerminal()
	setTitle(appName)
	handleExit()
	ensureFiles()
	cfg := loadConfig()
	if cfg.Animations {
		playIntro()
	}
	for {
		choice := showMenu(cfg)
		switch strings.ToLower(choice) {
		case "1":
			runSniper(cfg)
		case "2":
			chooseMode(cfg)
		case "3":
			cfg.Random = !cfg.Random
			cfg.save()
		case "4":
			chooseThreads(cfg)
		case "5":
			chooseList(cfg)
		case "6":
			cfg.ShowTaken = !cfg.ShowTaken
			cfg.save()
		case "7":
			cfg.Sound = !cfg.Sound
			cfg.save()
			if cfg.Sound {
				go beep()
			}
		case "8":
			cfg.Animations = !cfg.Animations
			cfg.save()
		case "9":
			editSecrets(cfg)
		case "p":
			proxyMenu(cfg)
		case "h":
			openHits()
		case "0", "q":
			clearScreen()
			fmt.Println("  " + cBrand + bold + "LIGHTNING" + reset + " " + cText + bold + "CHECKER" + reset + cMuted + "  ·  bye" + reset)
			exitClean()
		}
	}
}

func handleExit() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		for range signals {
			if running.Load() {
				continue
			}
			exitClean()
		}
	}()
}

func ensureFiles() {
	_, err := os.Stat(defaultListPath)
	if err != nil {
		sample := "# one username per line\nexample1\nexample2\n"
		os.WriteFile(defaultListPath, []byte(sample), 0644)
	}
	_, err = os.Stat(proxiesPath)
	if err != nil {
		proxySample := "# one proxy per line, any of:\n# ip:port\n# ip:port:user:pass\n# user:pass@ip:port\n# socks5://user:pass@ip:port\n"
		os.WriteFile(proxiesPath, []byte(proxySample), 0644)
	}
}

func showMenu(cfg *Config) string {
	platform := findPlatform(cfg.Mode)
	users := loadUsers(cfg.ListPath)
	invalid := countInvalid(users, platform)
	proxies := len(loadProxies())

	order := cText + "First to last" + reset
	if cfg.Random {
		order = cText + "Random" + reset
	}
	listValue := cText + filepath.Base(cfg.ListPath) + reset + cMuted + "  " + formatInt(int64(len(users))) + " names" + reset
	if invalid > 0 {
		listValue += cMuted + ", " + formatInt(int64(invalid)) + " skipped" + reset
	}
	threadValue := workerText(cfg, proxies)
	proxyValue := cText + formatInt(int64(proxies)) + reset + cMuted + " loaded" + reset
	if proxies == 0 {
		proxyValue = cWarn + "none" + reset + cMuted + " · running direct" + reset
	}
	if proxies > 0 && cfg.TestProxy {
		proxyValue += cMuted + " · tested on start" + reset
	}
	secretCount := 0
	for _, group := range secretGroups() {
		for _, field := range group.Fields {
			if cfg.secret(field.Key) != "" {
				secretCount++
			}
		}
	}

	printHeader()
	fmt.Println(boxTop("MENU"))
	fmt.Println(boxRow(cBrand + bold + "1   ► Start checking" + reset + cMuted + "   on " + reset + platform.Color + bold + platform.Name + reset))
	fmt.Println(boxDivider())
	fmt.Println(menuRow("2", "Mode", platform.Color+bold+platform.Name+reset+cMuted+"  "+platform.Info+reset))
	fmt.Println(menuRow("3", "Order", order))
	fmt.Println(menuRow("4", "Workers", threadValue))
	fmt.Println(menuRow("5", "List file", listValue))
	fmt.Println(menuRow("P", "Proxies", proxyValue))
	fmt.Println(menuRow("6", "Show taken", onOff(cfg.ShowTaken)))
	fmt.Println(menuRow("7", "Hit sound", onOff(cfg.Sound)))
	fmt.Println(menuRow("8", "Animations", onOff(cfg.Animations)))
	fmt.Println(boxDivider())
	fmt.Println(menuRow("9", "Saved logins", cText+fmt.Sprint(secretCount)+reset+cMuted+" saved · tokens, sessions, webhook"+reset))
	fmt.Println(menuRow("H", "Hits folder", cMuted+"open in Explorer"+reset))
	fmt.Println(menuRow("0", "Exit", ""))
	fmt.Println(boxBottom())
	return askAnimated("Choose an option:", cfg)
}

func chooseMode(cfg *Config) {
	printHeader()
	fmt.Println(boxTop("MODE"))
	for i, p := range platforms {
		current := ""
		if p.Key == cfg.Mode {
			current = cGood + "  ● current" + reset
		}
		label := p.Color + bold + padRight(p.Name, 12) + reset
		fmt.Println(boxRow(cAccent + bold + strconv.Itoa(i+1) + reset + "   " + label + cMuted + padRight(p.Info, 18) + reset + current))
	}
	fmt.Println(boxDivider())
	fmt.Println(boxRow(cAccent + bold + "0" + reset + "   " + cText + "Back" + reset))
	fmt.Println(boxBottom())
	fmt.Println()
	choice := askAnimated("Choose a mode:", cfg)
	index, err := strconv.Atoi(choice)
	if err != nil {
		return
	}
	if index < 1 || index > len(platforms) {
		return
	}
	cfg.Mode = platforms[index-1].Key
	cfg.save()
}

func workerText(cfg *Config, proxies int) string {
	count := cfg.workers(proxies)
	if cfg.Threads == 0 {
		return cGood + "Auto" + reset + cMuted + "  " + strconv.Itoa(count) + " now" + reset
	}
	text := cText + strconv.Itoa(count) + reset
	if proxies == 0 && count > 5 {
		text += cWarn + "  high without proxies" + reset
	}
	return text
}

func chooseThreads(cfg *Config) {
	fmt.Println()
	note("info", "Auto = proxies × per-proxy requests (5 when direct). Rate limits are handled either way.")
	value := strings.ToLower(ask("Workers (number or \"auto\", Enter to keep):"))
	if value == "" {
		return
	}
	if value == "auto" || value == "a" || value == "0" {
		cfg.Threads = 0
		cfg.save()
		return
	}
	threads, err := strconv.Atoi(value)
	if err != nil || threads < 1 || threads > 2000 {
		note("fail", "Enter a number from 1 to 2000, or auto.")
		waitEnter()
		return
	}
	cfg.Threads = threads
	cfg.save()
}

func proxyMenu(cfg *Config) {
	for {
		proxies := loadProxies()
		printHeader()
		fmt.Println(boxTop("PROXIES"))
		fmt.Println(boxRow(cMuted + "proxies.txt  " + reset + cText + formatInt(int64(len(proxies))) + reset + cMuted + " unique proxies loaded" + reset))
		fmt.Println(boxDivider())
		fmt.Println(menuRow("1", "Test now", cMuted+"against "+findPlatform(cfg.Mode).Name+", remove dead ones"+reset))
		fmt.Println(menuRow("2", "Test on start", onOff(cfg.TestProxy)))
		fmt.Println(menuRow("3", "Per proxy", cText+strconv.Itoa(cfg.PerProxy)+reset+cMuted+" requests at once"+reset))
		fmt.Println(menuRow("0", "Back", ""))
		fmt.Println(boxBottom())
		fmt.Println("  " + cMuted + "Dead or limited proxies are switched out automatically during a run." + reset)
		fmt.Println()
		choice := askAnimated("Choose an option:", cfg)
		switch choice {
		case "1":
			testProxiesNow(cfg, proxies)
		case "2":
			cfg.TestProxy = !cfg.TestProxy
			cfg.save()
		case "3":
			value := ask("Requests per proxy at once (1-50, Enter to keep):")
			number, err := strconv.Atoi(value)
			if err == nil && number >= 1 && number <= 50 {
				cfg.PerProxy = number
				cfg.save()
			}
		default:
			return
		}
	}
}

func testProxiesNow(cfg *Config, proxies []*url.URL) {
	fmt.Println()
	if len(proxies) == 0 {
		note("fail", "proxies.txt is empty.")
		waitEnter()
		return
	}
	platform := findPlatform(cfg.Mode)
	note("info", fmt.Sprintf("Testing %s proxies against %s...", formatInt(int64(len(proxies))), platform.Name))
	alive := runProxyTest(proxies, platform)
	dead := len(proxies) - len(alive)
	note("ok", fmt.Sprintf("%s working · %s dead · avg %dms", formatInt(int64(len(alive))), formatInt(int64(dead)), averageLatency(alive).Milliseconds()))
	if dead == 0 || len(alive) == 0 {
		waitEnter()
		return
	}
	answer := strings.ToLower(ask("Save only working proxies, fastest first? Old list goes to proxies_backup.txt (y/N):"))
	if answer != "y" {
		return
	}
	original, err := os.ReadFile(proxiesPath)
	if err == nil {
		os.WriteFile("proxies_backup.txt", original, 0644)
	}
	var lines []string
	for _, item := range alive {
		lines = append(lines, item.url.String())
	}
	err = os.WriteFile(proxiesPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	if err != nil {
		note("fail", "Could not save: "+err.Error())
	} else {
		note("ok", "Saved "+formatInt(int64(len(alive)))+" working proxies.")
	}
	waitEnter()
}

func chooseList(cfg *Config) {
	fmt.Println()
	note("info", "Drag a .txt file into this window, or type a path. One username per line.")
	value := cleanPath(ask("List file (Enter for " + defaultListPath + "):"))
	if value == "" {
		value = defaultListPath
	}
	_, err := os.Stat(value)
	if err != nil {
		note("fail", "File not found: "+value)
		waitEnter()
		return
	}
	cfg.ListPath = value
	cfg.save()
}

func openHits() {
	err := os.MkdirAll("hits", 0755)
	if err == nil {
		err = openFolder("hits")
	}
	if err != nil {
		note("fail", "Could not open the hits folder: "+err.Error())
		waitEnter()
	}
}

type secretGroup struct {
	Name   string
	Color  string
	Fields []Field
}

func secretGroups() []secretGroup {
	var groups []secretGroup
	for _, p := range platforms {
		if len(p.Fields) == 0 {
			continue
		}
		groups = append(groups, secretGroup{Name: p.Name, Color: p.Color, Fields: p.Fields})
	}
	groups = append(groups, secretGroup{Name: "Webhook", Color: cBrand, Fields: []Field{webhookField}})
	return groups
}

func editSecrets(cfg *Config) {
	for {
		groups := secretGroups()
		printHeader()
		fmt.Println(boxTop("SAVED LOGINS"))
		for i, group := range groups {
			var values []string
			for _, field := range group.Fields {
				values = append(values, mask(cfg.Secrets[field.Key]))
			}
			label := group.Color + bold + padRight(group.Name, 12) + reset
			fmt.Println(boxRow(cAccent + bold + strconv.Itoa(i+1) + reset + "   " + label + strings.Join(values, "  ")))
		}
		fmt.Println(boxDivider())
		fmt.Println(boxRow(cAccent + bold + "C" + reset + "   " + cText + "Clear everything" + reset))
		fmt.Println(boxRow(cAccent + bold + "0" + reset + "   " + cText + "Back" + reset))
		fmt.Println(boxBottom())
		fmt.Println("  " + cMuted + "Stored in config.json next to the exe. Don't share that file." + reset)
		fmt.Println()
		choice := askAnimated("Choose one to change:", cfg)
		if choice == "0" || choice == "" {
			return
		}
		if strings.ToLower(choice) == "c" {
			answer := ask("Clear all saved logins? (y/N):")
			if strings.ToLower(answer) == "y" {
				cfg.Secrets = map[string]string{}
				cfg.save()
			}
			continue
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index < 1 || index > len(groups) {
			continue
		}
		askFields(cfg, groups[index-1].Fields, true)
		waitEnter()
	}
}
