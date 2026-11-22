package main

import (
	"encoding/json"
	"os"
)

const configPath = "config.json"
const skipValue = "-"
const defaultListPath = "users.txt"

type Config struct {
	Mode       string            `json:"mode"`
	Random     bool              `json:"random"`
	Threads    int               `json:"threads"`
	ShowTaken  bool              `json:"show_taken"`
	Sound      bool              `json:"sound"`
	Animations bool              `json:"animations"`
	ListPath   string            `json:"list_path"`
	PerProxy   int               `json:"per_proxy"`
	TestProxy  bool              `json:"test_proxies"`
	Secrets    map[string]string `json:"secrets"`
}

func defaultConfig() *Config {
	cfg := &Config{}
	cfg.Mode = "discord"
	cfg.Random = false
	cfg.Threads = 0
	cfg.ShowTaken = true
	cfg.Sound = true
	cfg.Animations = true
	cfg.ListPath = defaultListPath
	cfg.PerProxy = 2
	cfg.TestProxy = true
	cfg.Secrets = map[string]string{}
	return cfg
}

func loadConfig() *Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		return defaultConfig()
	}
	if cfg.Secrets == nil {
		cfg.Secrets = map[string]string{}
	}
	if cfg.Threads < 0 {
		cfg.Threads = 0
	}
	if cfg.PerProxy < 1 {
		cfg.PerProxy = 2
	}
	if cfg.ListPath == "" {
		cfg.ListPath = defaultListPath
	}
	if findPlatform(cfg.Mode) == nil {
		cfg.Mode = "discord"
	}
	return cfg
}

func (c *Config) save() {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	err = os.WriteFile(configPath, data, 0600)
	if err != nil {
		note("fail", "Could not save config.json: "+err.Error())
	}
}

func (c *Config) secret(key string) string {
	value := c.Secrets[key]
	if value == skipValue {
		return ""
	}
	return value
}

func (c *Config) workers(proxies int) int {
	if c.Threads > 0 {
		return c.Threads
	}
	if proxies == 0 {
		return 5
	}
	count := proxies * c.PerProxy
	if count < 4 {
		count = 4
	}
	if count > 2000 {
		count = 2000
	}
	return count
}
