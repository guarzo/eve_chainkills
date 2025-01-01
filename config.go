package main

import (
	"encoding/json"
	"os"
)

// AppConfig mirrors your config.json fields
type AppConfig struct {
	IgnoreSystemIds              []int  `json:"ignoreSystemIds"`
	DiscordChainkillWebhookId    string `json:"discordChainkillWebhookId"`
	DiscordChainkillWebhookToken string `json:"discordChainkillWebhookToken"`
	DiscordInfoWebhookId         string `json:"discordInfoWebhookId"`
	DiscordInfoWebhookToken      string `json:"discordInfoWebhookToken"`
	DiscordCorpkillWebhookId     string `json:"discordCorpkillWebhookId"`
	DiscordCorpkillWebhookToken  string `json:"discordCorpkillWebhookToken"`

	CharacterIdForUpdates        string `json:"characterIdForUpdates"`
	SystemKillStatusResetMinutes int    `json:"systemKillStatusResetMinutes"`
	DiscordStatusReportMins      int    `json:"discordStatusReportMins"`

	DiscordKillNotifications struct {
		KillColor string `json:"killColor"`
		LossColor string `json:"lossColor"`
	} `json:"discordKillNotifications"`

	// The new API fields:
	APIBaseUrl string `json:"apiBaseUrl"`
	APISlug    string `json:"apiSlug"`
	APIToken   string `json:"apiToken"`
}

// LoadConfig loads JSON from file into AppConfig
func LoadConfig(path string) (*AppConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	cfg := &AppConfig{}
	if err = json.NewDecoder(file).Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
