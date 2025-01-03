package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// KillDetails merges zKill + ESI data into FlattenedKillMail, plus IsKill
type KillDetails struct {
	logger  *logrus.Logger
	config  *AppConfig
	rawData []byte // raw zKill message

	// The combined/merged data from zKill + ESI
	FKM    FlattenedKillMail
	IsKill bool
}

func NewKillDetails(logger *logrus.Logger, config *AppConfig, raw []byte) *KillDetails {
	return &KillDetails{
		logger:  logger,
		config:  config,
		rawData: raw,
	}
}

// GetKillDetails merges the data from zKill + ESI into kd.FKM
func (kd *KillDetails) GetKillDetails() error {
	var zm ZkillMail
	if err := json.Unmarshal(kd.rawData, &zm); err != nil {
		return fmt.Errorf("unmarshal zkill message: %w", err)
	}

	kd.FKM.KillMailID = zm.KillmailID
	kd.FKM.SolarSystemID = zm.SolarSystemID

	if kd.FKM.KillMailID == 0 || zm.ZKB.Hash == "" {
		kd.logger.Printf("No killmail_id or hash found in zkill message.")
		return nil
	}
	kd.FKM.Hash = zm.ZKB.Hash

	killmailURL := fmt.Sprintf("https://esi.evetech.net/latest/killmails/%d/%s/?datasource=tranquility",
		kd.FKM.KillMailID, kd.FKM.Hash)
	resp, err := doGetRequest(killmailURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("ESI killmail returned status %d", resp.StatusCode)
	}

	var km EsiKillMail
	if err = json.NewDecoder(resp.Body).Decode(&km); err != nil {
		return fmt.Errorf("JSON decode error: %w", err)
	}

	kd.FKM.KillMailTime = km.KillMailTime
	kd.FKM.SolarSystemID = km.SolarSystemID
	kd.FKM.Victim = km.Victim
	kd.FKM.Attackers = km.Attackers

	kd.FKM.TotalValue = zm.ZKB.TotalValue
	kd.FKM.DestroyedValue = zm.ZKB.DestroyedValue
	kd.FKM.DroppedValue = zm.ZKB.DroppedValue
	kd.FKM.FittedValue = zm.ZKB.FittedValue
	kd.FKM.Points = zm.ZKB.Points
	kd.FKM.NPC = zm.ZKB.NPC
	kd.FKM.Solo = zm.ZKB.Solo
	kd.FKM.Awox = zm.ZKB.Awox

	if kd.FKM.SolarSystemID > 0 {
		sysName, sysErr := fetchSystemName(kd.FKM.SolarSystemID)
		if sysErr != nil {
			kd.logger.Printf("Error fetching system name: %v", sysErr)
		} else {
			kd.FKM.SystemName = sysName
		}
	}

	if kd.FKM.Victim.ShipTypeID > 0 {
		vsn, vsnErr := fetchTypeName(kd.FKM.Victim.ShipTypeID)
		if vsnErr != nil {
			kd.logger.Printf("Error fetching victim ship name: %v", vsnErr)
		} else {
			kd.FKM.VictimShipName = vsn
		}
	}

	finalIdx := -1
	for i, att := range km.Attackers {
		if att.FinalBlow {
			finalIdx = i
			break
		}
	}
	if finalIdx < 0 && len(km.Attackers) > 0 {
		finalIdx = 0 // fallback
	}
	if finalIdx >= 0 {
		final := km.Attackers[finalIdx]
		kd.FKM.FinalAttackerID = final.CharacterID
		kd.FKM.FinalAttackerCorpID = final.CorporationID
		kd.FKM.FinalAttackerAllianceID = final.AllianceID

		if final.CharacterID > 0 {
			attName, _ := fetchCharacterName(final.CharacterID)
			kd.FKM.FinalAttackerName = attName
		}

		if kd.FKM.FinalAttackerCorpID > 0 {
			corpName, _ := fetchCorporationName(final.CorporationID)
			kd.FKM.FinalAttackerCorpName = corpName
		}

		if kd.FKM.FinalAttackerAllianceID > 0 {
			alliName, _ := fetchAllianceName(final.AllianceID)
			kd.FKM.FinalAttackerAllianceName = alliName
		}

		if final.ShipTypeID > 0 {
			attShipName, _ := fetchTypeName(final.ShipTypeID)
			kd.FKM.FinalAttackerShipName = attShipName
		}
	}

	if kd.FKM.Victim.CharacterID > 0 {
		name, errName := fetchCharacterName(kd.FKM.Victim.CharacterID)
		if errName != nil {
			kd.logger.Printf("Error fetching victim name: %v", errName)
		} else {
			kd.FKM.VictimCharacterName = name
		}
	}

	if kd.FKM.Victim.CorporationID > 0 {
		corpName, _ := fetchCorporationName(kd.FKM.Victim.CorporationID)
		kd.FKM.VictimCorpName = corpName
	}

	if kd.FKM.Victim.AllianceID > 0 {
		alliName, _ := fetchAllianceName(kd.FKM.Victim.AllianceID)
		kd.FKM.VictimAllianceName = alliName
	}

	kd.logger.Printf("Fetched kill details, system=%s, victimShip=%s", kd.FKM.SystemName, kd.FKM.VictimShipName)
	return nil
}

// fetchVictimName calls ESI's character endpoint to get name
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

	var cr EsiCharacterResponse
	if err = json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return fmt.Errorf("JSON decode error from ESI character: %w", err)
	}

	kd.FKM.VictimCharacterName = cr.Name
	return nil
}

func fetchCharacterName(charID int) (string, error) {
	url := fmt.Sprintf("https://esi.evetech.net/latest/characters/%d/?datasource=tranquility", charID)
	resp, err := doGetRequest(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("bad status %d", resp.StatusCode)
	}
	var c EsiCharacterResponse
	if err = json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return "", err
	}
	return c.Name, nil
}

// fetchCorporationName queries ESI for corporation info, returns its name.
func fetchCorporationName(corpID int) (string, error) {
	url := fmt.Sprintf("https://esi.evetech.net/latest/corporations/%d/?datasource=tranquility", corpID)
	resp, err := doGetRequest(url)
	if err != nil {
		return "", fmt.Errorf("fetchCorporationName: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetchCorporationName got status %d", resp.StatusCode)
	}

	var corp struct {
		Name   string `json:"name"`
		Ticker string `json:"ticker"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&corp); err != nil {
		return "", fmt.Errorf("JSON decode error (corp): %w", err)
	}

	return corp.Name, nil
}

// fetchAllianceName queries ESI for alliance info, returns its name.
func fetchAllianceName(allianceID int) (string, error) {
	url := fmt.Sprintf("https://esi.evetech.net/latest/alliances/%d/?datasource=tranquility", allianceID)
	resp, err := doGetRequest(url)
	if err != nil {
		return "", fmt.Errorf("fetchAllianceName: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetchAllianceName got status %d", resp.StatusCode)
	}

	var alli struct {
		Name   string `json:"name"`
		Ticker string `json:"ticker"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&alli); err != nil {
		return "", fmt.Errorf("JSON decode error (alliance): %w", err)
	}

	return alli.Name, nil
}

func fetchTypeName(typeID int) (string, error) {
	url := fmt.Sprintf("https://esi.evetech.net/latest/universe/types/%d/?datasource=tranquility", typeID)
	resp, err := doGetRequest(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("bad status %d", resp.StatusCode)
	}
	var body struct {
		Name string `json:"name"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Name, nil
}

func fetchSystemName(systemID int) (string, error) {
	url := fmt.Sprintf("https://esi.evetech.net/latest/universe/systems/%d/?datasource=tranquility", systemID)
	resp, err := doGetRequest(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetchSystemName got status %d", resp.StatusCode)
	}

	var sys struct {
		Name string `json:"name"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&sys); err != nil {
		return "", err
	}
	return sys.Name, nil
}

func doGetRequest(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
