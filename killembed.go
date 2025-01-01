package main

import (
	"fmt"
	"log"
)

// KillEmbed example struct; we’ll build a simple object with Title/Description
type Embed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color,omitempty"`
}

type KillEmbed struct {
	logger      *log.Logger
	config      *AppConfig
	killDetails *KillDetails
}

func NewKillEmbed(logger *log.Logger, config *AppConfig, kd *KillDetails) *KillEmbed {
	return &KillEmbed{
		logger:      logger,
		config:      config,
		killDetails: kd,
	}
}

func (ke *KillEmbed) CreateEmbed() Embed {
	// color logic from config: "killColor" or "lossColor"
	var colorHex string
	if ke.killDetails.IsKill {
		colorHex = ke.config.DiscordKillNotifications.KillColor // e.g. "#00FF00"
	} else {
		colorHex = ke.config.DiscordKillNotifications.LossColor // e.g. "#FF0000"
	}

	intColor := parseHexColor(colorHex)

	title := "Kill Notification"
	if !ke.killDetails.IsKill {
		title = "Loss Notification"
	}
	desc := fmt.Sprintf("Victim: %s", ke.killDetails.VictimCharacterName)

	return Embed{
		Title:       title,
		Description: desc,
		Color:       intColor,
	}
}

func parseHexColor(hexStr string) int {
	// quick and dirty way to parse a color like "#RRGGBB" into an int
	var r, g, b int
	if _, err := fmt.Sscanf(hexStr, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 16777215 // white
	}
	return (r << 16) + (g << 8) + b
}
