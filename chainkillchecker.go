package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ChainKillChecker is the Go equivalent of the ChainKillChecker class in JS.
type ChainKillChecker struct {
	logger                *log.Logger
	config                *AppConfig
	mapIds                []int
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

// SystemInfo is a small struct for storing system data from your new API
type SystemInfo struct {
	SystemId int
	Alias    string
}

// MapCharacter is a small struct for storing character data from your new API
type MapCharacter struct {
	CharacterId   string
	CorporationId int
	AllianceId    int
}

// NewChainKillChecker constructor
func NewChainKillChecker(logger *log.Logger, config *AppConfig) (*ChainKillChecker, error) {
	// parse mapIds from config
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
	// We'll attempt reconnection in a loop
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
			if e := ck.handleZKillMessage(string(msg)); e != nil {
				ck.logger.Printf("Error handling zKill message: %v", e)
			}
		}(message)
	}
}

// Close closes the checker’s websocket if it’s open
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

// handleZKillMessage is analogous to the JS version
func (ck *ChainKillChecker) handleZKillMessage(jsonData string) error {
	var messageData struct {
		KillmailId    int `json:"killmail_id"`
		SolarSystemId int `json:"solar_system_id"`
		Victim        struct {
			AllianceId    int `json:"alliance_id"`
			CorporationId int `json:"corporation_id"`
		} `json:"victim"`
		Attackers []struct {
			AllianceId    int `json:"alliance_id"`
			CorporationId int `json:"corporation_id"`
			CharacterId   int `json:"character_id"`
		} `json:"attackers"`
	}
	if err := json.Unmarshal([]byte(jsonData), &messageData); err != nil {
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
		messageData.KillmailId, messageData.SolarSystemId, minSinceLastSystems, minSinceLastStatus)
	if int(minSinceLastSystems) > ck.minToGetLatestSystems {
		if err := ck.updateSystems(); err != nil {
			ck.logger.Printf("Error updating systems: %v", err)
		}
	}

	// Check if kill is by/against tracked corp/alliance
	matchedCorpKill := false
	isKill := false

	allianceId := messageData.Victim.AllianceId
	victimCorpId := messageData.Victim.CorporationId

	// is the victim in insightTrackedIds?
	for _, tid := range ck.insightTrackedIds {
		if tid == victimCorpId || tid == allianceId {
			ck.logger.Printf("KillId %d => victim match. corpId=%d, allianceId=%d",
				messageData.KillmailId, victimCorpId, allianceId)
			matchedCorpKill = true
			isKill = false
			break
		}
	}

	if !matchedCorpKill {
		// check attackers
		var matchedAttackersCorp int
		var matchedAttackersAlli int
		for _, att := range messageData.Attackers {
			for _, tid := range ck.insightTrackedIds {
				if att.CorporationId == tid {
					matchedCorpKill = true
					isKill = true
					matchedAttackersCorp = att.CorporationId
					break
				}
				if att.AllianceId == tid {
					matchedCorpKill = true
					isKill = true
					matchedAttackersAlli = att.AllianceId
					break
				}
			}
			if matchedCorpKill {
				if matchedAttackersCorp > 0 {
					ck.logger.Printf("KillId %d => attacker corp match. corpId=%d",
						messageData.KillmailId, matchedAttackersCorp)
				} else {
					ck.logger.Printf("KillId %d => attacker alliance match. allianceId=%d",
						messageData.KillmailId, matchedAttackersAlli)
				}
				break
			}
		}
	}

	if matchedCorpKill {
		ck.sendCorpKillMessage(messageData, isKill)
		return nil
	}

	// else check if it happened in a system we track
	var matchedSystem *SystemInfo
	for i := range ck.systems {
		if ck.systems[i].SystemId == messageData.SolarSystemId {
			matchedSystem = &ck.systems[i]
			break
		}
	}
	if matchedSystem != nil && !contains(ck.ignoreSystemIds, matchedSystem.SystemId) {
		ck.logger.Printf("SystemId (%d) matched; checking map characters...", matchedSystem.SystemId)
		// see if any of the attackers are in mapCharacters
		foundMappedAttacker := false
		for _, att := range messageData.Attackers {
			for _, mc := range ck.mapCharacters {
				if mc.CharacterId == strconv.FormatInt(int64(att.CharacterId), 10) {
					foundMappedAttacker = true
					break
				}
			}
			if foundMappedAttacker {
				break
			}
		}

		if !foundMappedAttacker {
			ck.logger.Printf("Zero mapped attackers out of %d. Sending chain message.", len(messageData.Attackers))
			post := fmt.Sprintf("@here A ship just died in %s to %d people, zkill link: https://zkillboard.com/kill/%d/",
				matchedSystem.Alias, len(messageData.Attackers), messageData.KillmailId)

			// update system status (stub)
			ck.updateSystemStatus(messageData.SolarSystemId, 4, matchedSystem.Alias)

			// send chain message
			ck.sendChainMessage(post)
		} else {
			ck.logger.Printf("Skipping chain message; found mapped attackers.")
		}
	}
	return nil
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
			Id            string `json:"id"` // not used
			Name          string `json:"name"`
			SolarSystemId int    `json:"solar_system_id"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	var newSystems []SystemInfo
	for _, item := range body.Data {
		nameLen := len(item.Name)
		if nameLen > 0 {
			ck.logger.Printf("processing system: %v \n", item)

			lastChar := item.Name[nameLen-1]
			if (lastChar >= 'A' && lastChar <= 'Z') || (lastChar >= 'a' && lastChar <= 'z') {
				// Ends with a letter => skip
				continue
			}

			newSystems = append(newSystems, SystemInfo{
				SystemId: item.SolarSystemId,
				Alias:    item.Name,
			})
		}
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
			Id        string `json:"id"` // not used
			Character struct {
				Id            string `json:"id"`
				EveId         string `json:"eve_id"`
				CorporationId int    `json:"corporation_id"`
				AllianceId    int    `json:"alliance_id"`
			} `json:"character"`
		} `json:"data"`
	}
	ck.logger.Println(fmt.Sprintf("character data %v", body))
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	var newChars []MapCharacter
	for _, item := range body.Data {
		mc := MapCharacter{
			CharacterId:   item.Character.EveId,
			CorporationId: item.Character.CorporationId,
			AllianceId:    item.Character.AllianceId,
		}
		newChars = append(newChars, mc)
	}
	ck.mapCharacters = newChars
	ck.logger.Printf("[getMapCharacters] Fetched %d characters.\n", len(ck.mapCharacters))
	return nil
}

// updateSystemStatus is a stub (no DB usage)
func (ck *ChainKillChecker) updateSystemStatus(systemId, statusId int, systemName string) {
	ck.logger.Printf("[updateSystemStatus] systemId=%d, statusId=%d, name=%s", systemId, statusId, systemName)
	// If you need to call an external API to note the status, do it here.
}

// sendChainMessage uses Discord's webhook
func (ck *ChainKillChecker) sendChainMessage(messageBody string) {
	err := sendDiscordWebhook(ck.config.DiscordChainkillWebhookId, ck.config.DiscordChainkillWebhookToken, messageBody, nil)
	if err != nil {
		ck.logger.Printf("Error sending chain message: %v", err)
	}
}

// sendCorpKillMessage builds a kill embed & sends to the corp kill channel
func (ck *ChainKillChecker) sendCorpKillMessage(msgData interface{}, isKill bool) {
	ck.logger.Printf("Sending Corp kill message")
	// In JS, you constructed a KillDetails object and used it to build a KillEmbed
	// We'll do something simpler in Go:
	kd := NewKillDetails(ck.logger, ck.config, msgData)
	if err := kd.GetKillDetails(); err != nil {
		ck.logger.Printf("GetKillDetails error: %v", err)
	}
	kd.IsKill = isKill

	ke := NewKillEmbed(ck.logger, ck.config, kd)
	embed := ke.CreateEmbed() // returns a simple struct with Title, Description, etc.

	err := sendDiscordWebhook(ck.config.DiscordCorpkillWebhookId, ck.config.DiscordCorpkillWebhookToken, "", &embed)
	if err != nil {
		ck.logger.Printf("Error sending corp kill embed: %v", err)
	}
}

// sendInfoMessage uses the "info" webhook
func (ck *ChainKillChecker) sendInfoMessage(messageBody string) {
	ck.logger.Printf("Sending info message: %s", messageBody)
	err := sendDiscordWebhook(ck.config.DiscordInfoWebhookId, ck.config.DiscordInfoWebhookToken, messageBody, nil)
	if err != nil {
		ck.logger.Printf("Error sending info message: %v", err)
	}
}

// --- Helper funcs

func contains(arr []int, v int) bool {
	for _, a := range arr {
		if a == v {
			return true
		}
	}
	return false
}

func parseCommaSeparatedInts(csv string) []int {
	var result []int
	if csv == "" {
		return result
	}
	var tmp int
	for _, val := range splitComma(csv) {
		if _, err := fmt.Sscanf(val, "%d", &tmp); err == nil {
			if tmp > 0 {
				result = append(result, tmp)
			}
		}
	}
	return result
}

func splitComma(str string) []string {
	var out []string
	start := 0
	for i := 0; i < len(str); i++ {
		if str[i] == ',' {
			out = append(out, str[start:i])
			start = i + 1
		}
	}
	out = append(out, str[start:])
	return out
}
