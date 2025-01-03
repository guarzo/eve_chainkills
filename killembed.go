package main

import (
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"
)

// -------------------------------------------------------------------
// Detailed Discord Embed Structures
// -------------------------------------------------------------------

// DiscordEmbed matches Discord’s Embed JSON structure
type DiscordEmbed struct {
	Title       string            `json:"title,omitempty"`
	URL         string            `json:"url,omitempty"`
	Description string            `json:"description,omitempty"`
	Color       int               `json:"color,omitempty"`
	Timestamp   string            `json:"timestamp,omitempty"`
	Footer      *DiscordFooter    `json:"footer,omitempty"`
	Thumbnail   *DiscordThumbnail `json:"thumbnail,omitempty"`
	Author      *DiscordAuthor    `json:"author,omitempty"`
	Fields      []DiscordField    `json:"fields,omitempty"`
}

type DiscordFooter struct {
	Text string `json:"text,omitempty"`
}

type DiscordThumbnail struct {
	URL string `json:"url,omitempty"`
}

type DiscordAuthor struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

type DiscordField struct {
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
	Inline bool   `json:"inline,omitempty"`
}

// -------------------------------------------------------------------
// KillEmbed
// -------------------------------------------------------------------

type KillEmbed struct {
	logger      *logrus.Logger
	config      *AppConfig
	killDetails *KillDetails
}

// NewKillEmbed constructor
func NewKillEmbed(logger *logrus.Logger, config *AppConfig, kd *KillDetails) *KillEmbed {
	return &KillEmbed{
		logger:      logger,
		config:      config,
		killDetails: kd,
	}
}

// CreateEmbed replicates your old JS logic, but also uses real ship + character + system names
func (ke *KillEmbed) CreateEmbed() DiscordEmbed {
	kd := ke.killDetails
	fkm := kd.FKM // FlattenedKillMail
	isKill := kd.IsKill
	isAwox := fkm.Awox

	colorHex := ke.pickColor(isKill)
	intColor := parseHexColor(colorHex)

	// zKillboard link
	zkillLink := fmt.Sprintf("https://zkillboard.com/kill/%d/", fkm.KillMailID)

	// author block (top-left). "Kill" or "Loss"
	authorText := "Loss"
	if isKill {
		authorText = "Kill"
	}

	if isAwox {
		authorText = "Cowardly Awox"
	}

	// If alliance > 0, use alliance image, else corp
	authorImage := ""
	if fkm.Victim.AllianceID > 0 {
		authorImage = fmt.Sprintf("https://image.eveonline.com/Alliance/%d_64.png", fkm.Victim.AllianceID)
	} else {
		authorImage = fmt.Sprintf("https://image.eveonline.com/Corporation/%d_64.png", fkm.Victim.CorporationID)
	}

	// Final attacker name/ship
	finalAttackerName := fkm.FinalAttackerName
	if finalAttackerName == "" {
		finalAttackerName = "UnknownAttacker"
	}
	finalAttackerShip := fkm.FinalAttackerShipName
	if finalAttackerShip == "" {
		finalAttackerShip = "UnknownShip"
	}

	// Victim’s character name + ship name
	victimCharName := fkm.VictimCharacterName
	if victimCharName == "" {
		victimCharName = "UnknownVictim"
	}
	victimShipName := fkm.VictimShipName
	if victimShipName == "" {
		victimShipName = "UnknownShip"
	}

	// zKill links to victim/attacker
	victimZkillURL := fmt.Sprintf("https://zkillboard.com/character/%d/", fkm.Victim.CharacterID)
	attackerZkillURL := "https://zkillboard.com/"
	if fkm.FinalAttackerID > 0 {
		attackerZkillURL = fmt.Sprintf("https://zkillboard.com/character/%d/", fkm.FinalAttackerID)
	}

	// If multiple attackers, mention "and X others"
	attackersCount := len(fkm.Attackers)
	descEnd := "solo"
	if attackersCount > 1 {
		descEnd = fmt.Sprintf("and **%d** others", attackersCount-1)
	}

	// Build description matching your JS version
	victimGroupName := "UnknownGroup"
	if fkm.Victim.AllianceID > 0 {
		victimGroupName = fkm.VictimAllianceName
	} else if fkm.Victim.CorporationID > 0 {
		victimGroupName = fkm.VictimCorpName
	}

	attackerGroupName := "UnknownGroup"
	if fkm.FinalAttackerAllianceID > 0 {
		attackerGroupName = fkm.FinalAttackerAllianceName
	} else if fkm.FinalAttackerCorpID > 0 {
		attackerGroupName = fkm.FinalAttackerCorpName
	}

	description := fmt.Sprintf(
		"**[%s](%s)(%s)** lost their **%s** to **[%s](%s)(%s)** flying in a **%s** %s.",
		victimCharName, victimZkillURL, victimGroupName,
		victimShipName,
		finalAttackerName, attackerZkillURL, attackerGroupName,
		finalAttackerShip,
		descEnd,
	)

	// If you have a system name, use it. Otherwise fallback to "SystemID:%d"
	systemName := fkm.SystemName
	if systemName == "" {
		systemName = fmt.Sprintf("SystemID:%d", fkm.SolarSystemID)
	}
	// Title: "Hurricane destroyed in J123456"
	title := fmt.Sprintf("%s destroyed in %s", victimShipName, systemName)

	embed := DiscordEmbed{
		Title:     title,
		URL:       zkillLink,
		Color:     intColor,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Author: &DiscordAuthor{
			Name:    authorText,
			URL:     zkillLink,
			IconURL: authorImage,
		},
		Description: description,
		Thumbnail: &DiscordThumbnail{
			// Victim’s ship 64x64
			URL: fmt.Sprintf("https://image.eveonline.com/Type/%d_64.png", fkm.Victim.ShipTypeID),
		},
		Footer: &DiscordFooter{
			Text: fmt.Sprintf("Value: %s", formatISKValue(fkm.TotalValue)),
		},
	}

	return embed
}

// pickColor returns the hex code string from config
func (ke *KillEmbed) pickColor(isKill bool) string {
	if isKill {
		return ke.config.DiscordKillNotifications.KillColor
	}
	return ke.config.DiscordKillNotifications.LossColor
}

// parseHexColor parses "#RRGGBB" into an int
func parseHexColor(hexStr string) int {
	var r, g, b int
	if _, err := fmt.Sscanf(hexStr, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0xFFFFFF
	}
	return (r << 16) + (g << 8) + b
}

// formatISKValue - formats ISK with commas in the thousands place
// e.g. 1234567.89 => 1,234,567.89 ISK
func formatISKValue(amount float64) string {
	if amount < 1_000_000 {
		return "<1 M ISK"
	}
	// Convert to millions
	millions := amount / 1_000_000

	// Round to 2 decimals (e.g. 1.2345 => 1.23)
	millions = math.Round(millions*100) / 100

	return fmt.Sprintf("%.1fm ISK", millions)
}
