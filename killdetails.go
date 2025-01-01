package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// KillDetails in JS handled fetching details from ESI or zKill’s endpoints.
// In Go, we’ll do the same: use the killmail_id/hash from zKill, call ESI,
// then (optionally) fetch the victim's character name from ESI.
type KillDetails struct {
	logger  *log.Logger
	config  *AppConfig
	msgData interface{}

	// Data we fill in after ESI calls:
	KillmailID          int
	KillmailHash        string
	SolarSystemID       int
	KillmailTime        time.Time
	VictimCharacterID   int
	VictimCorporationID int
	VictimAllianceID    int
	VictimCharacterName string
	IsKill              bool // set later by your chainkillchecker
}

// This is what ESI returns from the killmail endpoint
type esiKillmailResponse struct {
	Attackers []struct {
		CharacterID   int `json:"character_id"`
		CorporationID int `json:"corporation_id"`
		AllianceID    int `json:"alliance_id"`
	} `json:"attackers"`
	Victim struct {
		CharacterID   int `json:"character_id"`
		CorporationID int `json:"corporation_id"`
		AllianceID    int `json:"alliance_id"`
	} `json:"victim"`
	KillmailTime  string `json:"killmail_time"`   // e.g. "2023-01-02T15:04:05Z"
	SolarSystemID int    `json:"solar_system_id"` // e.g. 31000265 for a wormhole
	KillmailID    int    `json:"killmail_id"`     // might be redundant but returned
	ZkbHash       string // not actually in ESI, but we store it if needed
}

// ESI character response for name lookups, etc.
type esiCharacterResponse struct {
	Name string `json:"name"`
	// ESI includes other fields (corp, alliance, birthday, security_status, etc.)
	// but we only need name here.
}

func NewKillDetails(logger *log.Logger, config *AppConfig, msgData interface{}) *KillDetails {
	return &KillDetails{
		logger:  logger,
		config:  config,
		msgData: msgData,
	}
}

// GetKillDetails extracts killmail_id & hash from msgData, calls ESI, and sets victim info
func (kd *KillDetails) GetKillDetails() error {
	// 1) Parse killmail_id and hash from kd.msgData
	//    zKill typically includes something like: { killmail_id, zkb: { hash } }
	zkillJSON, err := json.Marshal(kd.msgData)
	if err != nil {
		return fmt.Errorf("failed to marshal kd.msgData: %w", err)
	}
	var zkill struct {
		KillmailID int `json:"killmail_id"`
		Zkb        struct {
			Hash string `json:"hash"`
		} `json:"zkb"`
	}
	if err := json.Unmarshal(zkillJSON, &zkill); err != nil {
		return fmt.Errorf("failed to unmarshal zkill message: %w", err)
	}
	kd.KillmailID = zkill.KillmailID
	kd.KillmailHash = zkill.Zkb.Hash

	// If either is missing, just log & exit
	if kd.KillmailID == 0 || kd.KillmailHash == "" {
		kd.logger.Printf("No killmail_id or hash found in zkill message.")
		return nil
	}

	// 2) Build ESI killmail URL
	//    e.g. https://esi.evetech.net/latest/killmails/{killmail_id}/{hash}/
	killmailURL := fmt.Sprintf("https://esi.evetech.net/latest/killmails/%d/%s/?datasource=tranquility",
		kd.KillmailID, kd.KillmailHash)
	kd.logger.Printf("Fetching ESI killmail data from %s", killmailURL)

	// 3) Fetch from ESI
	killmailResp, err := doGetRequest(killmailURL)
	if err != nil {
		kd.logger.Printf("ESI killmail fetch error: %v", err)
		return err
	}
	defer killmailResp.Body.Close()
	if killmailResp.StatusCode < 200 || killmailResp.StatusCode > 299 {
		return fmt.Errorf("ESI killmail returned status %d", killmailResp.StatusCode)
	}

	// 4) Parse JSON
	var km esiKillmailResponse
	if err := json.NewDecoder(killmailResp.Body).Decode(&km); err != nil {
		return fmt.Errorf("JSON decode error from ESI killmail: %w", err)
	}

	// 5) Fill in your KillDetails
	kd.SolarSystemID = km.SolarSystemID
	kd.VictimCharacterID = km.Victim.CharacterID
	kd.VictimCorporationID = km.Victim.CorporationID
	kd.VictimAllianceID = km.Victim.AllianceID
	if km.KillmailTime != "" {
		// parse time
		t, err := time.Parse(time.RFC3339, km.KillmailTime)
		if err == nil {
			kd.KillmailTime = t
		}
	}

	// 6) Optionally fetch the victim’s character name (if victimCharacterID > 0)
	if kd.VictimCharacterID > 0 {
		if err := kd.fetchVictimName(kd.VictimCharacterID); err != nil {
			kd.logger.Printf("Error fetching victim name: %v", err)
		}
	}

	kd.logger.Printf("Fetched kill details, victim name = %s", kd.VictimCharacterName)
	return nil
}

// fetchVictimName calls ESI’s character endpoint to get the Name
func (kd *KillDetails) fetchVictimName(charID int) error {
	charURL := fmt.Sprintf("https://esi.evetech.net/latest/characters/%d/?datasource=tranquility", charID)
	kd.logger.Printf("Fetching ESI character data from %s", charURL)

	resp, err := doGetRequest(charURL)
	if err != nil {
		return fmt.Errorf("ESI character fetch error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ESI character returned status %d", resp.StatusCode)
	}

	var c esiCharacterResponse
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return fmt.Errorf("JSON decode error from ESI character: %w", err)
	}

	kd.VictimCharacterName = c.Name
	return nil
}

// doGetRequest is a small helper for GET calls with a default timeout.
func doGetRequest(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// If you need custom headers, add them here
	// req.Header.Set("User-Agent", "MyEveApp/1.0")

	return client.Do(req)
}
