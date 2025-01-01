package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// discordWebhookBody is the shape for a basic message or embed
type discordWebhookBody struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// sendDiscordWebhook sends either a text message or an embed
func sendDiscordWebhook(webhookID, webhookToken, textMessage string, embed *Embed) error {
	if webhookID == "" || webhookToken == "" {
		return fmt.Errorf("discord webhook not configured properly (ID/Token missing)")
	}

	url := fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", webhookID, webhookToken)

	bodyStruct := discordWebhookBody{}
	if textMessage != "" {
		bodyStruct.Content = textMessage
	}
	if embed != nil {
		bodyStruct.Embeds = []Embed{*embed}
	}

	payload, err := json.Marshal(bodyStruct)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("discord webhook got status %d", resp.StatusCode)
	}
	return nil
}
