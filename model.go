package main

import "time"

// -------------------------------------------------------------------
// EVE / zKill Models
// -------------------------------------------------------------------

// ZkillMail represents the JSON structure from the zKillboard feed.
type ZkillMail struct {
	KillmailID    int64      `json:"killmail_id"`
	SolarSystemID int        `json:"solar_system_id"`
	Victim        Victim     `json:"victim"`
	Attackers     []Attacker `json:"attackers"`
	ZKB           ZKB        `json:"zkb"`
}

// ZKB holds the hash and economic info from zKill
type ZKB struct {
	LocationID     int64   `json:"locationID"`
	Hash           string  `json:"hash"`
	FittedValue    float64 `json:"fittedValue"`
	DroppedValue   float64 `json:"droppedValue"`
	DestroyedValue float64 `json:"destroyedValue"`
	TotalValue     float64 `json:"totalValue"`
	Points         int     `json:"points"`
	NPC            bool    `json:"npc"`
	Solo           bool    `json:"solo"`
	Awox           bool    `json:"awox"`
}

// Victim from either zKill or ESI
type Victim struct {
	AllianceID    int `json:"alliance_id"`
	CorporationID int `json:"corporation_id"`
	CharacterID   int `json:"character_id"`
	DamageTaken   int `json:"damage_taken"`

	// ESI-specific
	ShipTypeID int `json:"ship_type_id"`
}

// Attacker from either zKill or ESI
type Attacker struct {
	AllianceID     int     `json:"alliance_id"`
	CharacterID    int     `json:"character_id"`
	CorporationID  int     `json:"corporation_id"`
	DamageDone     int     `json:"damage_done"`
	FinalBlow      bool    `json:"final_blow"`
	SecurityStatus float64 `json:"security_status"`
	ShipTypeID     int     `json:"ship_type_id"`
	WeaponTypeID   int     `json:"weapon_type_id"`
}

// -------------------------------------------------------------------
// ESI Models
// -------------------------------------------------------------------

// EsiKillMail is what ESI returns for a killmail lookup.
type EsiKillMail struct {
	KillMailID    int        `json:"killmail_id"`
	KillMailTime  time.Time  `json:"killmail_time"`
	SolarSystemID int        `json:"solar_system_id"`
	Victim        Victim     `json:"victim"`
	Attackers     []Attacker `json:"attackers"`
}

type EsiCharacterResponse struct {
	Name string `json:"name"`
}

// -------------------------------------------------------------------
// FlattenedKillMail merges zKill + ESI data
// -------------------------------------------------------------------

type FlattenedKillMail struct {
	// Basic IDs
	KillMailID    int64     `json:"killmail_id"`
	Hash          string    `json:"hash"`          // from zKill's "zkb.hash"
	KillMailTime  time.Time `json:"killmail_time"` // from ESI
	SolarSystemID int       `json:"solar_system_id"`

	LocationID     int64   `json:"locationID"`
	FittedValue    float64 `json:"fittedValue"`
	DroppedValue   float64 `json:"droppedValue"`
	DestroyedValue float64 `json:"destroyedValue"`
	TotalValue     float64 `json:"totalValue"`
	Points         int     `json:"points"`
	NPC            bool    `json:"npc"`
	Solo           bool    `json:"solo"`
	Awox           bool    `json:"awox"`

	SystemName string `json:"system_name"`

	// Entities
	Victim    Victim     `json:"victim"`
	Attackers []Attacker `json:"attackers"`

	VictimCharacterName string `json:"victim_character_name"`

	FinalAttackerID           int    `json:"final_attacker_id"`
	FinalAttackerName         string `json:"final_attacker_name"`
	FinalAttackerCorpID       int    `json:"final_attacker_corp_id"`
	FinalAttackerAllianceID   int    `json:"final_attacker_alliance_id"`
	FinalAttackerShipName     string `json:"final_attacker_ship_name"`
	FinalAttackerCorpName     string `json:"final_attacker_corp_name"`
	FinalAttackerAllianceName string `json:"final_attacker_alliance_name"`

	VictimShipName     string `json:"victim_ship_name"`
	VictimCorpName     string `json:"victim_corp_name"`
	VictimAllianceName string `json:"victim_alliance_name"`
}

// -------------------------------------------------------------------
// Systems and Characters Models
// -------------------------------------------------------------------

type SystemInfo struct {
	SystemId int
	Alias    string
}

type MapCharacter struct {
	CharacterId   string
	CorporationId int
	AllianceId    int
}
