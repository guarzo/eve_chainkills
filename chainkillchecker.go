package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
)

// ChainKillChecker is the Go equivalent of the ChainKillChecker class in JS.
type ChainKillChecker struct {
	logger                *logrus.Logger
	config                *AppConfig
	ignoreSystemIds       []int
	apiBaseUrl            string
	apiSlug               string
	apiToken              string
	insightTrackedIds     []int
	minToSendDiscord      int
	systems               []SystemInfo
	mapCharacters         []MapCharacter
	lastUpdateTime        time.Time
	lastDiscordStatusTime time.Time
	minToGetLatestSystems int
	// internal fields
	wsConn       *websocket.Conn
	wsCancelFunc context.CancelFunc
	mu           sync.Mutex
}

// NewChainKillChecker constructor
func NewChainKillChecker(logger *logrus.Logger, config *AppConfig) (*ChainKillChecker, error) {
	var ignoreSys []int
	if len(config.IgnoreSystemIds) > 0 {
		ignoreSys = config.IgnoreSystemIds
	}

	ck := &ChainKillChecker{
		logger:                logger,
		config:                config,
		ignoreSystemIds:       ignoreSys,
		apiBaseUrl:            config.APIBaseUrl,
		apiSlug:               config.APISlug,
		apiToken:              config.APIToken,
		insightTrackedIds:     config.InsightTrackedIds,
		minToSendDiscord:      config.DiscordStatusReportMins,
		systems:               []SystemInfo{},
		mapCharacters:         []MapCharacter{},
		lastUpdateTime:        time.Now(),
		lastDiscordStatusTime: time.Now(),
		minToGetLatestSystems: 0, // was 0 in the JS code
	}
	ck.logger.Printf("[ChainKillChecker] Initialized. insightTrackedIds: %v", ck.insightTrackedIds)
	return ck, nil
}

// StartListening is analogous to the StartListening() in the JS code
func (ck *ChainKillChecker) StartListening() {
	// fetch initial data
	if err := ck.updateSystems(); err != nil {
		ck.logger.Printf("Error updating systems on startup: %v", err)
	}
	if err := ck.getMapCharacters(); err != nil {
		ck.logger.Printf("Error updating map characters on startup: %v", err)
	}

	// start a goroutine that attempts to maintain the WebSocket connection
	go ck.connectAndListenZKill()
}

// connectAndListenZKill attempts a (re)connection to the zKillboard feed
func (ck *ChainKillChecker) connectAndListenZKill() {
	reconnectDelay := 10 * time.Second
	wsURL := "wss://zkillboard.com/websocket/"
	for {
		ctx, cancel := context.WithCancel(context.Background())
		ck.mu.Lock()
		ck.wsCancelFunc = cancel
		ck.mu.Unlock()

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			ck.logger.Printf("WebSocket dial error: %v. Retrying in %s ...", err, reconnectDelay)
			time.Sleep(reconnectDelay)
			continue
		}

		ck.logger.Printf("Connected to zKillboard feed.")
		ck.mu.Lock()
		ck.wsConn = conn
		ck.mu.Unlock()

		// subscribe to killstream
		subMessage := map[string]string{
			"action":  "sub",
			"channel": "killstream",
		}
		if err = conn.WriteJSON(subMessage); err != nil {
			ck.logger.Printf("Error sending sub message to zKill: %v", err)
			conn.Close()
			time.Sleep(reconnectDelay)
			continue
		} else {
			ck.logger.Printf("Sent sub message to zkill: %+v", subMessage)
			ck.sendInfoMessage("zkill socket opened.")
		}

		// read messages in a loop
		err = ck.readLoop(ctx, conn)
		if err != nil {
			ck.logger.Printf("readLoop error: %v", err)
		}

		ck.logger.Println("Socket closed; reattempting in", reconnectDelay)
		conn.Close()
		time.Sleep(reconnectDelay)
	}
}

// readLoop reads from the websocket until an error
func (ck *ChainKillChecker) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		go func(msg []byte) {
			if e := ck.handleZKillMessage(msg); e != nil {
				ck.logger.Printf("Error handling zKill message: %v", e)
			}
		}(message)
	}
}

// handleZKillMessage is analogous to the JS version but uses our ZkillMail struct.
func (ck *ChainKillChecker) handleZKillMessage(raw []byte) error {
	var zm ZkillMail
	if err := json.Unmarshal(raw, &zm); err != nil {
		return fmt.Errorf("unmarshal zKill message: %w", err)
	}

	// Possibly send a status update
	minSinceLastStatus := time.Since(ck.lastDiscordStatusTime).Minutes()
	if int(minSinceLastStatus) > ck.minToSendDiscord {
		ck.lastDiscordStatusTime = time.Now()
		ck.sendInfoMessage("Chainkills checker running.")
	}

	// Possibly refresh systems from API
	minSinceLastSystems := time.Since(ck.lastUpdateTime).Minutes()
	ck.logger.Printf("[ZKill] killId=%d, solarSystem=%d, lastSysUpdate=%.1f mins, lastStatus=%.1f mins",
		zm.KillmailID, zm.SolarSystemID, minSinceLastSystems, minSinceLastStatus)
	if int(minSinceLastSystems) > ck.minToGetLatestSystems {
		if err := ck.updateSystems(); err != nil {
			ck.logger.Printf("Error updating systems: %v", err)
		}
	}

	// Check if kill is by/against tracked corp/alliance
	matchedCorpKill := false
	isKill := false

	allianceId := zm.Victim.AllianceID
	victimCorpId := zm.Victim.CorporationID

	// is the victim in insightTrackedIds?
	for _, tid := range ck.insightTrackedIds {
		if tid == victimCorpId || tid == allianceId {
			ck.logger.Printf("KillId %d => victim match. corpId=%d, allianceId=%d",
				zm.KillmailID, victimCorpId, allianceId)
			matchedCorpKill = true
			isKill = false
			break
		}
	}

	if !matchedCorpKill {
		// check attackers
		var matchedAttackersCorp int
		var matchedAttackersAlli int
		var matchedAttackerCharacter int
		for _, att := range zm.Attackers {
			for _, tid := range ck.insightTrackedIds {
				if att.CorporationID == tid {
					matchedCorpKill = true
					isKill = true
					matchedAttackersCorp = att.CorporationID
					break
				}
				if att.AllianceID == tid {
					matchedCorpKill = true
					isKill = true
					matchedAttackersAlli = att.AllianceID
					break
				}
			}
			// Also check if the attacker’s character ID is in mapCharacters
			for _, mc := range ck.mapCharacters {
				if mc.CharacterId == strconv.Itoa(att.CharacterID) {
					matchedCorpKill = true
					isKill = true
					matchedAttackerCharacter = att.CharacterID
					break
				}
			}
			if matchedCorpKill {
				if matchedAttackersCorp > 0 {
					ck.logger.Printf("KillId %d => attacker corp match. corpId=%d",
						zm.KillmailID, matchedAttackersCorp)
				} else if matchedAttackerCharacter > 0 {
					ck.logger.Printf("KillId %d => attacker char match. characterId=%d",
						zm.KillmailID, matchedAttackerCharacter)
				} else {
					ck.logger.Printf("KillId %d => attacker alliance match. allianceId=%d",
						zm.KillmailID, matchedAttackersAlli)
				}
				break
			}
		}
	}

	if matchedCorpKill {
		// We’ll pass the original raw message, which includes zkb hash, to "sendCorpKillMessage"
		ck.sendCorpKillMessage(raw, isKill)
		return nil
	}

	// else check if it happened in a system we track
	var matchedSystem *SystemInfo
	for i := range ck.systems {
		if ck.systems[i].SystemId == int(zm.SolarSystemID) {
			matchedSystem = &ck.systems[i]
			break
		}
	}

	if matchedSystem != nil && !slices.Contains(ck.ignoreSystemIds, matchedSystem.SystemId) {
		ck.logger.Printf("SystemId (%d) matched; checking map characters...", matchedSystem.SystemId)
		// see if any of the attackers are in mapCharacters
		foundMappedAttacker := false
		for _, att := range zm.Attackers {
			for _, mc := range ck.mapCharacters {
				if mc.CharacterId == strconv.Itoa(att.CharacterID) {
					foundMappedAttacker = true
					break
				}
			}
			if foundMappedAttacker {
				break
			}
		}

		if !foundMappedAttacker {
			ck.logger.Printf("Zero mapped attackers out of %d. Sending chain message.", len(zm.Attackers))
			post := fmt.Sprintf("@here A ship just died in %s to %d people, zkill link: https://zkillboard.com/kill/%d/",
				matchedSystem.Alias, len(zm.Attackers), zm.KillmailID)

			// send chain message
			ck.sendChainMessage(post)
		} else {
			ck.logger.Printf("Skipping chain message; found mapped attackers.")
		}
	}
	return nil
}

// readLoop / connectAndListenZKill call this on shutdown
func (ck *ChainKillChecker) Close() {
	ck.logger.Println("ChainKillChecker closing.")
	ck.mu.Lock()
	defer ck.mu.Unlock()
	if ck.wsConn != nil {
		ck.wsConn.Close()
	}
	if ck.wsCancelFunc != nil {
		ck.wsCancelFunc()
	}
}

// updateSystems fetches systems from the new API
func (ck *ChainKillChecker) updateSystems() error {
	ck.logger.Println("Updating system list from API...")
	url := fmt.Sprintf("%s/systems?slug=%s", ck.apiBaseUrl, ck.apiSlug)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ck.apiToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		ck.sendInfoMessage(fmt.Sprintf("Error updateSystems : %v", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ck.sendInfoMessage(fmt.Sprintf("Error updateSystems : %v", resp.Status))
		return fmt.Errorf("updateSystems: bad status %s", resp.Status)
	}

	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			SolarSystemId int    `json:"solar_system_id"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	var newSystems []SystemInfo
	for _, item := range body.Data {
		if len(item.Name) == 0 {
			continue
		}
		// Example skip if name ends with letter
		lastChar := item.Name[len(item.Name)-1]
		if (lastChar >= 'A' && lastChar <= 'Z') || (lastChar >= 'a' && lastChar <= 'z') {
			// skip
			continue
		}
		newSystems = append(newSystems, SystemInfo{
			SystemId: item.SolarSystemId,
			Alias:    item.Name,
		})
	}
	ck.systems = newSystems
	ck.logger.Printf("[updateSystems] Fetched %d systems.\n", len(ck.systems))
	ck.lastUpdateTime = time.Now()
	return nil
}

// getMapCharacters fetches the characters from your new API
func (ck *ChainKillChecker) getMapCharacters() error {
	ck.logger.Println("Getting characters from API...")
	url := fmt.Sprintf("%s/characters?slug=%s", ck.apiBaseUrl, ck.apiSlug)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ck.apiToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		ck.sendInfoMessage(fmt.Sprintf("Error getMapCharacters : %v", err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ck.sendInfoMessage(fmt.Sprintf("Error getMapCharacters : %v", resp.Status))
		return fmt.Errorf("getMapCharacters: bad status %s", resp.Status)
	}

	var body struct {
		Data []struct {
			ID        string `json:"id"`
			Character struct {
				ID            string `json:"id"`
				EveID         string `json:"eve_id"`
				CorporationID int    `json:"corporation_id"`
				AllianceID    int    `json:"alliance_id"`
			} `json:"character"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	var newChars []MapCharacter
	for _, item := range body.Data {
		mc := MapCharacter{
			CharacterId:   item.Character.EveID,
			CorporationId: item.Character.CorporationID,
			AllianceId:    item.Character.AllianceID,
		}
		newChars = append(newChars, mc)
	}
	ck.mapCharacters = newChars
	ck.logger.Printf("[getMapCharacters] Fetched %d characters.\n", len(ck.mapCharacters))
	return nil
}

// sendChainMessage uses Discord's webhook
func (ck *ChainKillChecker) sendChainMessage(messageBody string) {
	err := sendDiscordWebhook(
		ck.config.DiscordChainkillWebhookId,
		ck.config.DiscordChainkillWebhookToken,
		messageBody,
		nil,
	)
	if err != nil {
		ck.logger.Printf("Error sending chain message: %v", err)
	}
}

// sendCorpKillMessage builds a kill embed & sends to the corp kill channel
func (ck *ChainKillChecker) sendCorpKillMessage(raw []byte, isKill bool) {
	kd := NewKillDetails(ck.logger, ck.config, raw)
	if err := kd.GetKillDetails(); err != nil {
		ck.logger.Printf("GetKillDetails error: %v", err)
	}
	kd.IsKill = isKill

	// Now kd.FKM holds everything from zKill + ESI
	ke := NewKillEmbed(ck.logger, ck.config, kd)
	embed := ke.CreateEmbed()

	err := sendDiscordWebhook(
		ck.config.DiscordCorpkillWebhookId,
		ck.config.DiscordCorpkillWebhookToken,
		"",
		&embed,
	)
	if err != nil {
		ck.logger.Printf("Error sending corp kill embed: %v", err)
	}
}

// sendInfoMessage uses the "info" webhook
func (ck *ChainKillChecker) sendInfoMessage(messageBody string) {
	ck.logger.Printf("Sending info message: %s", messageBody)
	err := sendDiscordWebhook(
		ck.config.DiscordInfoWebhookId,
		ck.config.DiscordInfoWebhookToken,
		messageBody,
		nil,
	)
	if err != nil {
		ck.logger.Printf("Error sending info message: %v", err)
	}
}
