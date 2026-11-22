package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type Status int

const (
	StatusAvailable Status = iota
	StatusTaken
	StatusInvalid
	StatusLimited
	StatusError
	StatusFatal
)

type Result struct {
	Status Status
	Wait   time.Duration
	Exact  bool
	Global bool
	Limit  *limitInfo
	Note   string
}

type Field struct {
	Key      string
	Label    string
	Optional bool
	Help     []string
}

type Platform struct {
	Key          string
	Name         string
	Color        string
	Info         string
	TestURL      string
	LimitHeaders bool
	Fields       []Field
	Valid        func(name string) bool
	Check        func(ctx context.Context, client *http.Client, cfg *Config, name string) Result
	Verify       func(ctx context.Context, client *http.Client, cfg *Config) (string, error)
}

type response struct {
	status    int
	body      string
	finalPath string
	header    http.Header
}

var platforms = []*Platform{
	{
		Key:          "discord",
		Name:         "Discord",
		Color:        "\033[38;2;88;101;242m",
		Info:         "token optional",
		TestURL:      "https://discord.com/robots.txt",
		LimitHeaders: true,
		Fields: []Field{
			{
				Key:      "discord_token",
				Label:    "Discord token",
				Optional: true,
				Help: []string{
					"No token: logged-out check, limited per IP (proxies help).",
					"Token: logged-in check, limited per account.",
					"Get it: discord.com/app → F12 → Network → click a channel →",
					"any request to /api → copy the \"authorization\" header.",
					"Use an alt. Automating a user token breaks Discord ToS.",
				},
			},
		},
		Valid:  validDiscord,
		Check:  checkDiscord,
		Verify: verifyDiscord,
	},
	{
		Key:     "telegram",
		Name:    "Telegram",
		Color:   "\033[38;2;42;171;238m",
		Info:    "no login",
		TestURL: "https://t.me/robots.txt",
		Fields:  []Field{},
		Valid:   validTelegram,
		Check:   checkTelegram,
	},
	{
		Key:     "instagram",
		Name:    "Instagram",
		Color:   "\033[38;2;225;48;108m",
		Info:    "session optional",
		TestURL: "https://www.instagram.com/robots.txt",
		Fields: []Field{
			{
				Key:      "instagram_sessionid",
				Label:    "Instagram sessionid",
				Optional: true,
				Help: []string{
					"Logged out gets rate limited fast; a session lasts longer.",
					"Get it: log in on instagram.com → F12 → Application →",
					"Cookies → instagram.com → copy the \"sessionid\" value.",
					"Use an alt account.",
				},
			},
		},
		Valid: validInstagram,
		Check: checkInstagram,
	},
	{
		Key:     "tiktok",
		Name:    "TikTok",
		Color:   "\033[38;2;37;244;238m",
		Info:    "no login",
		TestURL: "https://www.tiktok.com/robots.txt",
		Fields:  []Field{},
		Valid:   validTikTok,
		Check:   checkTikTok,
	},
}

func findPlatform(key string) *Platform {
	for _, p := range platforms {
		if p.Key == key {
			return p
		}
	}
	return nil
}

var reDiscord = regexp.MustCompile(`^[a-z0-9_.]{2,32}$`)
var reTelegram = regexp.MustCompile(`^[a-z][a-z0-9_]{4,31}$`)
var reInstagram = regexp.MustCompile(`^[a-z0-9_.]{1,30}$`)
var reTikTok = regexp.MustCompile(`^[a-z0-9_.]{2,24}$`)

func validDiscord(name string) bool {
	if !reDiscord.MatchString(name) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return true
}

func validTelegram(name string) bool {
	if !reTelegram.MatchString(name) {
		return false
	}
	if strings.HasSuffix(name, "_") {
		return false
	}
	return true
}

func validInstagram(name string) bool {
	if !reInstagram.MatchString(name) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".") {
		return false
	}
	return true
}

func validTikTok(name string) bool {
	if !reTikTok.MatchString(name) {
		return false
	}
	if strings.HasSuffix(name, ".") {
		return false
	}
	return true
}

func doRequest(ctx context.Context, client *http.Client, method string, url string, body string, headers map[string]string) (response, error) {
	var res response
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return res, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return res, err
	}
	res.status = resp.StatusCode
	res.body = string(data)
	res.finalPath = resp.Request.URL.Path
	res.header = resp.Header
	return res, nil
}

func failed(note string) Result {
	return Result{Status: StatusError, Note: note}
}

func limited(res response, fallback time.Duration, note string) Result {
	wait, exact := parseRetryAfter(res.header)
	if !exact {
		wait = fallback
	}
	return Result{Status: StatusLimited, Wait: wait, Exact: exact, Note: note}
}

func parseSeconds(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func parseRetryAfter(header http.Header) (time.Duration, bool) {
	if header == nil {
		return 0, false
	}
	value := header.Get("Retry-After")
	wait, ok := parseSeconds(value)
	if ok {
		return wait, true
	}
	when, err := http.ParseTime(value)
	if err == nil {
		wait = time.Until(when)
		if wait < 0 {
			wait = 0
		}
		return wait, true
	}
	return 0, false
}

func discordLimits(header http.Header) *limitInfo {
	if header == nil {
		return nil
	}
	limit, err := strconv.Atoi(header.Get("X-RateLimit-Limit"))
	if err != nil || limit <= 0 {
		return nil
	}
	remaining, err := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return nil
	}
	resetAfter, ok := parseSeconds(header.Get("X-RateLimit-Reset-After"))
	if !ok {
		return nil
	}
	info := &limitInfo{}
	info.limit = limit
	info.remaining = remaining
	info.resetAfter = resetAfter
	return info
}

type discordReply struct {
	Taken      *bool   `json:"taken"`
	RetryAfter float64 `json:"retry_after"`
	Global     bool    `json:"global"`
	Username   string  `json:"username"`
}

func checkDiscord(ctx context.Context, client *http.Client, cfg *Config, name string) Result {
	token := cfg.secret("discord_token")
	url := "https://discord.com/api/v9/unique-username/username-attempt-unauthed"
	headers := map[string]string{
		"Content-Type": "application/json",
		"Origin":       "https://discord.com",
		"Referer":      "https://discord.com/register",
	}
	if token != "" {
		url = "https://discord.com/api/v9/users/@me/pomelo-attempt"
		headers["Authorization"] = token
		headers["Referer"] = "https://discord.com/channels/@me"
	}
	res, err := doRequest(ctx, client, "POST", url, `{"username":"`+name+`"}`, headers)
	if err != nil {
		return failed("network")
	}
	limits := discordLimits(res.header)
	userScoped := token != ""
	var reply discordReply
	jsonErr := json.Unmarshal([]byte(res.body), &reply)

	if res.status == 429 {
		result := Result{Status: StatusLimited, Exact: true, Global: userScoped, Limit: limits, Note: "rate limited"}
		wait, ok := parseSeconds(res.header.Get("X-RateLimit-Reset-After"))
		if jsonErr == nil && reply.RetryAfter > 0 {
			wait = time.Duration(reply.RetryAfter * float64(time.Second))
			ok = true
		}
		if !ok {
			wait, ok = parseRetryAfter(res.header)
		}
		if !ok {
			wait = 5 * time.Second
			result.Exact = false
		}
		result.Wait = wait
		if res.header.Get("X-RateLimit-Scope") == "shared" {
			result.Note = "shared limit"
		}
		return result
	}
	if res.status == 401 || res.status == 403 {
		if token != "" {
			return Result{Status: StatusFatal, Note: "Discord token is invalid or locked"}
		}
		return Result{Status: StatusLimited, Wait: 30 * time.Second, Note: "blocked"}
	}
	if res.status == 400 {
		return Result{Status: StatusInvalid, Global: userScoped, Limit: limits, Note: "rejected by Discord"}
	}
	if res.status != 200 || jsonErr != nil || reply.Taken == nil {
		if res.status == 200 || res.status >= 500 {
			return failed("bad response")
		}
		return Result{Status: StatusLimited, Wait: 15 * time.Second, Note: "blocked"}
	}
	if *reply.Taken {
		return Result{Status: StatusTaken, Global: userScoped, Limit: limits}
	}
	return Result{Status: StatusAvailable, Global: userScoped, Limit: limits}
}

func verifyDiscord(ctx context.Context, client *http.Client, cfg *Config) (string, error) {
	token := cfg.secret("discord_token")
	if token == "" {
		return "No token: using the logged-out check", nil
	}
	headers := map[string]string{"Authorization": token}
	res, err := doRequest(ctx, client, "GET", "https://discord.com/api/v9/users/@me", "", headers)
	if err != nil {
		return "", errors.New("could not reach Discord: " + err.Error())
	}
	if res.status == 401 || res.status == 403 {
		return "", errors.New("token is invalid")
	}
	if res.status != 200 {
		return "Could not verify the token, continuing", nil
	}
	var reply discordReply
	err = json.Unmarshal([]byte(res.body), &reply)
	if err != nil {
		return "Token OK", nil
	}
	return "Logged in as " + reply.Username, nil
}

func checkTelegram(ctx context.Context, client *http.Client, cfg *Config, name string) Result {
	res, err := doRequest(ctx, client, "GET", "https://t.me/"+name, "", nil)
	if err != nil {
		return failed("network")
	}
	if res.status == 429 {
		return limited(res, 5*time.Second, "rate limited")
	}
	if res.status != 200 || !strings.Contains(res.body, "tgme_page") {
		return limited(res, 10*time.Second, "blocked")
	}
	if strings.Contains(res.body, "tgme_page_extra") {
		return Result{Status: StatusTaken}
	}
	frag, err := doRequest(ctx, client, "GET", "https://fragment.com/username/"+name, "", nil)
	if err != nil {
		return failed("network")
	}
	if frag.status == 429 {
		return limited(frag, 5*time.Second, "fragment rate limited")
	}
	if frag.status != 200 {
		return limited(frag, 10*time.Second, "fragment blocked")
	}
	if frag.finalPath == "/username/"+name {
		return Result{Status: StatusTaken, Note: "on Fragment"}
	}
	return Result{Status: StatusAvailable}
}

func checkInstagram(ctx context.Context, client *http.Client, cfg *Config, name string) Result {
	headers := map[string]string{
		"X-IG-App-ID":      "936619743392459",
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          "https://www.instagram.com/" + name + "/",
		"Accept":           "*/*",
	}
	session := cfg.secret("instagram_sessionid")
	if session != "" {
		headers["Cookie"] = "sessionid=" + session
	}
	url := "https://www.instagram.com/api/v1/users/web_profile_info/?username=" + name
	res, err := doRequest(ctx, client, "GET", url, "", headers)
	if err != nil {
		return failed("network")
	}
	if res.status == 404 {
		return Result{Status: StatusAvailable}
	}
	if res.status == 429 || res.status == 401 || res.status == 403 {
		return limited(res, 30*time.Second, "rate limited")
	}
	if res.status >= 500 {
		return failed("server error")
	}
	if res.status != 200 {
		return limited(res, 20*time.Second, "blocked")
	}
	compact := strings.ReplaceAll(res.body, " ", "")
	if strings.Contains(compact, `"user":null`) {
		return Result{Status: StatusAvailable}
	}
	if strings.Contains(compact, `"user":{`) {
		return Result{Status: StatusTaken}
	}
	return limited(res, 20*time.Second, "unexpected page")
}

func checkTikTok(ctx context.Context, client *http.Client, cfg *Config, name string) Result {
	res, err := doRequest(ctx, client, "GET", "https://www.tiktok.com/@"+name+"?lang=en", "", nil)
	if err != nil {
		return failed("network")
	}
	if res.status == 429 {
		return limited(res, 10*time.Second, "rate limited")
	}
	if res.status >= 500 {
		return failed("server error")
	}
	if !strings.Contains(res.body, "webapp.user-detail") {
		return limited(res, 15*time.Second, "blocked")
	}
	if strings.Contains(res.body, `"statusCode":10202`) {
		return Result{Status: StatusAvailable}
	}
	if strings.Contains(res.body, `"statusCode":10221`) {
		return Result{Status: StatusTaken, Note: "banned"}
	}
	return Result{Status: StatusTaken}
}
