package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var webhookField = Field{
	Key:      "webhook",
	Label:    "Discord webhook",
	Optional: true,
	Help: []string{
		"Every hit is also posted to a Discord channel.",
		"Get it: channel settings → Integrations → Webhooks →",
		"New Webhook → Copy Webhook URL.",
	},
}

type webhookSender struct {
	url      string
	platform *Platform
	queue    chan string
	done     chan struct{}
	client   *http.Client
}

func postWebhook(client *http.Client, url string, content string) (int, error) {
	payload := map[string]string{}
	payload["username"] = appName
	payload["content"] = content
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func testWebhook(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("that doesn't look like a webhook URL")
	}
	client := &http.Client{}
	client.Timeout = 10 * time.Second
	status, err := postWebhook(client, url, "**"+appName+"** webhook connected.")
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("Discord answered with status %d", status)
	}
	return nil
}

func startWebhook(url string, platform *Platform) *webhookSender {
	if url == "" {
		return nil
	}
	w := &webhookSender{}
	w.url = url
	w.platform = platform
	w.queue = make(chan string, 2048)
	w.done = make(chan struct{})
	w.client = &http.Client{}
	w.client.Timeout = 10 * time.Second
	go w.loop()
	return w
}

func (w *webhookSender) push(name string) {
	if w == nil {
		return
	}
	select {
	case w.queue <- name:
	default:
	}
}

func (w *webhookSender) loop() {
	defer close(w.done)
	for name := range w.queue {
		content := "**HIT** · " + w.platform.Name + " · `" + name + "`"
		for attempt := 0; attempt < 3; attempt++ {
			status, err := postWebhook(w.client, w.url, content)
			if err == nil && status != 429 {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func (w *webhookSender) close() {
	if w == nil {
		return
	}
	close(w.queue)
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-w.done:
	case <-timer.C:
	}
}
