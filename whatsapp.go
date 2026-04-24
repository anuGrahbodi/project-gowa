package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/types"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func getOrCreateWaSession(phone string) *WaSession {
	waSessionsMu.Lock()
	defer waSessionsMu.Unlock()

	if ws, ok := waSessions[phone]; ok {
		return ws
	}

	ws := &WaSession{
		PhoneLoginMode: true,
		MonitorOn:      false,
		IncomingMsgs:   make([]MonitorMessage, 0),
		UserPhone:      phone,
	}

	dbLog := waLog.Stdout("Database", "WARN", true)
	dbName := fmt.Sprintf("file:wastore_%s.db?_foreign_keys=on", phone)
	container, err := sqlstore.New(context.Background(), "sqlite3", dbName, dbLog)
	if err != nil {
		log.Println("Failed to create store for", phone, ":", err)
		return nil
	}
	ws.Container = container

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		log.Println("Failed to get device for", phone, ":", err)
		return nil
	}

	clientLog := waLog.Stdout("Client", "WARN", true)
	ws.Client = whatsmeow.NewClient(deviceStore, clientLog)

	registerEventHandlers(ws)

	if ws.Client.Store.ID == nil {
		log.Printf("📱 [%s] Belum ada sesi WhatsApp. Silakan scan QR atau gunakan pairing code.\n", phone)
		connectWithQR(ws)
	} else {
		err = ws.Client.Connect()
		if err != nil {
			log.Printf("⚠️ [%s] Gagal connect: %v\n", phone, err)
		} else {
			ws.IsReady = true
			startHeartbeat(ws)
		}
	}

	waSessions[phone] = ws
	return ws
}

func connectWithQR(ws *WaSession) {
	ws.QrChanMu.Lock()
	ws.QrChan, _ = ws.Client.GetQRChannel(context.Background())
	ws.QrChanMu.Unlock()

	err := ws.Client.Connect()
	if err != nil {
		log.Printf("⚠️ [%s] Gagal connect untuk QR: %v\n", ws.UserPhone, err)
		return
	}

	go func() {
		for evt := range ws.QrChan {
			if evt.Event == "code" {
				png, err := qrcode.Encode(evt.Code, qrcode.Medium, 300)
				if err != nil {
					log.Printf("❌ [%s] Gagal generate QR image: %v\n", ws.UserPhone, err)
					continue
				}
				b64 := base64.StdEncoding.EncodeToString(png)
				ws.LatestQrDataUrl = "data:image/png;base64," + b64
				log.Printf("📱 [%s] QR Code baru tersedia di halaman web.\n", ws.UserPhone)
			} else {
				log.Printf("QR Event [%s]: %s\n", ws.UserPhone, evt.Event)
			}
		}
	}()
}

func registerEventHandlers(ws *WaSession) {
	ws.Client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			log.Printf("✅ [%s] WhatsApp Client is ready!\n", ws.UserPhone)
			ws.IsReady = true
			ws.LatestQrDataUrl = ""
			startHeartbeat(ws)

		case *events.LoggedOut:
			log.Printf("🔌 [%s] Client logged out: %v\n", ws.UserPhone, v.Reason)
			ws.IsReady = false
			ws.LatestQrDataUrl = ""
			stopHeartbeat(ws)
			
			userName := "Unknown"
			usersMu.RLock()
			if u, ok := users[ws.UserPhone]; ok {
				userName = u.Name
			}
			usersMu.RUnlock()
			go sendLogoutAlert(userName, ws.UserPhone)

		case *events.Disconnected:
			log.Printf("🔌 [%s] Client disconnected\n", ws.UserPhone)
			ws.IsReady = false

		case *events.StreamReplaced:
			log.Printf("🔌 [%s] Stream replaced (another device connected)\n", ws.UserPhone)
			ws.IsReady = false
			ws.LatestQrDataUrl = ""
			stopHeartbeat(ws)
			
			userName := "Unknown"
			usersMu.RLock()
			if u, ok := users[ws.UserPhone]; ok {
				userName = u.Name
			}
			usersMu.RUnlock()
			go sendLogoutAlert(userName, ws.UserPhone)

		case *events.Message:
			if !ws.MonitorOn {
				return
			}
			body := ""
			if v.Message.GetConversation() != "" {
				body = v.Message.GetConversation()
			} else if v.Message.GetExtendedTextMessage() != nil {
				body = v.Message.GetExtendedTextMessage().GetText()
			} else {
				body = "(media/kosong)"
			}
			if len(body) > 200 {
				body = body[:200]
			}

			entry := MonitorMessage{
				Time:    time.Now().Format("15:04:05"),
				From:    v.Info.Chat.String(),
				Body:    body,
				IsGroup: v.Info.IsGroup,
			}

			ws.MsgMu.Lock()
			ws.IncomingMsgs = append([]MonitorMessage{entry}, ws.IncomingMsgs...)
			if len(ws.IncomingMsgs) > 100 {
				ws.IncomingMsgs = ws.IncomingMsgs[:100]
			}
			ws.MsgMu.Unlock()

			truncBody := body
			if len(truncBody) > 50 {
				truncBody = truncBody[:50]
			}
			log.Printf("📩 [Monitor-%s] Pesan dari %s: %s\n", ws.UserPhone, v.Info.Chat.String(), truncBody)
		}
	})
}

func startHeartbeat(ws *WaSession) {
	stopHeartbeat(ws)
	ws.HeartbeatStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ws.HeartbeatStop:
				return
			case <-ticker.C:
				if !ws.IsReady {
					return
				}
				if !ws.Client.IsConnected() {
					log.Printf("💔 Heartbeat [%s]: WhatsApp terputus\n", ws.UserPhone)
					ws.IsReady = false
					ws.LatestQrDataUrl = ""
					
					userName := "Unknown"
					usersMu.RLock()
					if u, ok := users[ws.UserPhone]; ok {
						userName = u.Name
					}
					usersMu.RUnlock()
					go sendLogoutAlert(userName, ws.UserPhone)
					return
				}
			}
		}
	}()
}

func stopHeartbeat(ws *WaSession) {
	if ws.HeartbeatStop != nil {
		select {
		case <-ws.HeartbeatStop:
		default:
			close(ws.HeartbeatStop)
		}
	}
}



// ===== Send Message Functions =====
func sendTextMessage(ws *WaSession, jid types.JID, text string, mentions []string) error {
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(text),
		},
	}

	if len(mentions) > 0 {
		msg.ExtendedTextMessage.ContextInfo = &waProto.ContextInfo{
			MentionedJID: mentions,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := ws.Client.SendMessage(ctx, jid, msg)
	return err
}

func sendSimpleMessage(ws *WaSession, jid types.JID, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := ws.Client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(text),
	})
	return err
}

// ===== Group Functions =====
func getGroupParticipants(ws *WaSession, groupJID types.JID) ([]types.GroupParticipant, error) {
	info, err := ws.Client.GetGroupInfo(context.Background(), groupJID)
	if err != nil {
		return nil, err
	}
	return info.Participants, nil
}

func getGroupInfo(ws *WaSession, groupJID types.JID) (*types.GroupInfo, error) {
	return ws.Client.GetGroupInfo(context.Background(), groupJID)
}

// ===== Execute Schedule Functions =====
func executeGroupSchedule(job *Schedule) {
	ws := getOrCreateWaSession(job.UserPhone)
	if ws == nil || !ws.IsReady {
		log.Printf("❌ [%s] Sesi tidak siap untuk jadwal grup", job.UserPhone)
		return
	}

	payloadBytes, err := json.Marshal(job.Payload)
	if err != nil {
		log.Println("❌ Gagal marshal payload:", err)
		return
	}

	var payload struct {
		Message         string `json:"message"`
		SelectedTargets []int  `json:"selectedTargets"`
		HideTag         bool   `json:"hideTag"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.Println("❌ Gagal parse payload grup:", err)
		return
	}

	targetsMu.RLock()
	var sendTargets []Target
	if len(payload.SelectedTargets) > 0 {
		for _, i := range payload.SelectedTargets {
			if i >= 0 && i < len(targets) {
				sendTargets = append(sendTargets, targets[i])
			}
		}
	} else {
		sendTargets = make([]Target, len(targets))
		copy(sendTargets, targets)
	}
	targetsMu.RUnlock()

	if len(sendTargets) == 0 {
		return
	}

	listsMu.RLock()
	finalMessage := payload.Message
	var mentionIDs []string
	for key, list := range lists {
		placeholder := "${" + key + "}"
		if strings.Contains(finalMessage, placeholder) {
			finalMessage = strings.ReplaceAll(finalMessage, placeholder, processList(list))
			mentionIDs = append(mentionIDs, getMentionsList(list)...)
		}
	}
	listsMu.RUnlock()

	if payload.HideTag {
		for _, target := range sendTargets {
			if strings.HasSuffix(target.ID, "@g.us") {
				jid, err := types.ParseJID(target.ID)
				if err == nil {
					participants, pErr := getGroupParticipants(ws, jid)
					if pErr == nil {
						for _, p := range participants {
							mentionIDs = append(mentionIDs, p.JID.String())
						}
					}
				}
			}
		}
	}

	mentionIDs = uniqueStrings(mentionIDs)
	log.Printf("🚀 Schedule: Mengirim ke %d target, %d mentions\n", len(sendTargets), len(mentionIDs))

	for _, target := range sendTargets {
		jid, err := types.ParseJID(target.ID)
		if err != nil {
			log.Println("  ⚠️ JID tidak valid:", target.ID)
			continue
		}
		if len(mentionIDs) > 0 {
			err = sendTextMessage(ws, jid, finalMessage, mentionIDs)
		} else {
			err = sendSimpleMessage(ws, jid, finalMessage)
		}
		if err != nil {
			log.Printf("  ❌ Gagal ke %s: %v\n", target.Label, err)
		} else {
			log.Printf("  ✅ Terkirim ke %s\n", target.Label)
		}
	}
}

func executePrivateSchedule(job *Schedule) {
	ws := getOrCreateWaSession(job.UserPhone)
	if ws == nil || !ws.IsReady {
		log.Printf("❌ [%s] Sesi tidak siap untuk jadwal pribadi", job.UserPhone)
		return
	}

	payloadBytes, err := json.Marshal(job.Payload)
	if err != nil {
		log.Println("❌ Gagal marshal payload:", err)
		return
	}

	var payload []struct {
		Target  string `json:"target"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.Println("❌ Gagal parse payload pribadi:", err)
		return
	}

	for i, item := range payload {
		chatID := formatPhoneNumber(item.Target)
		jid := types.NewJID(chatID, types.DefaultUserServer)
		err := sendSimpleMessage(ws, jid, item.Message)
		if err != nil {
			log.Printf("  ❌ Gagal ke %s: %v\n", chatID, err)
		}

		if i < len(payload)-1 {
			delay := randomDelay(7000, 62000)
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}

func executeExcelBroadcastSchedule(job *Schedule) {
	payloadBytes, err := json.Marshal(job.Payload)
	if err != nil {
		log.Println("❌ Gagal marshal payload:", err)
		return
	}

	var payload struct {
		Items []struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		} `json:"items"`
		MinDelay     int    `json:"minDelay"`
		MaxDelay     int    `json:"maxDelay"`
		UserName     string `json:"userName"`
		SessionPhone string `json:"sessionPhone"`
	}

	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		log.Println("❌ Gagal parse payload excel_broadcast:", err)
		return
	}

	var details []BroadcastLogDetail

	ws := getOrCreateWaSession(payload.SessionPhone)
	if ws == nil || !ws.IsReady {
		log.Printf("❌ [%s] Sesi tidak siap untuk broadcast excel", payload.SessionPhone)
		return
	}

	for i, item := range payload.Items {
		targetStr := formatExcelNumber(item.Target)
		if targetStr == "" {
			continue
		}

		updateScheduleField(job.ID, func(s *Schedule) {
			s.CurrentTarget = targetStr
		})

		jid, err := types.ParseJID(targetStr + "@s.whatsapp.net")
		if err != nil {
			errMsg := "Invalid JID: " + err.Error()
			details = append(details, BroadcastLogDetail{
				Target:  item.Target,
				Status:  "failed",
				Message: errMsg,
			})
			updateScheduleField(job.ID, func(s *Schedule) {
				s.FailedCount++
			})
			go sendFailedBroadcastAlert(payload.UserName, payload.SessionPhone, item.Target, errMsg)
			continue
		}

		// Cek apakah nomor terdaftar di WhatsApp
		isRegistered := false
		resp, err := ws.Client.IsOnWhatsApp(context.Background(), []string{"+" + targetStr})
		if err == nil && len(resp) > 0 {
			isRegistered = resp[0].IsIn
		}

		if !isRegistered {
			errMsg := "Nomor tidak terdaftar di WhatsApp"
			details = append(details, BroadcastLogDetail{
				Target:  targetStr,
				Status:  "failed",
				Message: errMsg,
			})
			updateScheduleField(job.ID, func(s *Schedule) {
				s.FailedCount++
			})
			log.Printf("  ❌ Broadcast Gagal ke %s: %s\n", targetStr, errMsg)
			go sendFailedBroadcastAlert(payload.UserName, payload.SessionPhone, targetStr, errMsg)
			continue
		}

		err = sendSimpleMessage(ws, jid, item.Message)
		if err != nil {
			details = append(details, BroadcastLogDetail{
				Target:  targetStr,
				Status:  "failed",
				Message: err.Error(),
			})
			updateScheduleField(job.ID, func(s *Schedule) {
				s.FailedCount++
			})
			log.Printf("  ❌ Broadcast Gagal ke %s: %v\n", targetStr, err)
			go sendFailedBroadcastAlert(payload.UserName, payload.SessionPhone, targetStr, err.Error())
		} else {
			details = append(details, BroadcastLogDetail{
				Target:  targetStr,
				Status:  "success",
				Message: "Terkirim",
			})
			updateScheduleField(job.ID, func(s *Schedule) {
				s.SentCount++
			})
			log.Printf("  ✅ Broadcast Terkirim ke %s\n", targetStr)
		}

		// Beri jeda acak (kecuali baris terakhir)
		if i < len(payload.Items)-1 {
			minD := payload.MinDelay
			maxD := payload.MaxDelay
			if maxD < minD {
				maxD = minD
			}
			if maxD > 0 {
				delay := minD
				if maxD > minD {
					delay = rand.Intn(maxD-minD+1) + minD
				}
				log.Printf("  ⏳ Menunggu %d detik sebelum baris berikutnya...\n", delay)
				for d := delay; d > 0; d-- {
					updateScheduleField(job.ID, func(s *Schedule) {
						s.CurrentDelay = fmt.Sprintf("Menunggu %d detik...", d)
					})
					time.Sleep(time.Second)
				}
			}
		}
	}

	updateScheduleField(job.ID, func(s *Schedule) {
		s.CurrentTarget = "Selesai"
		s.CurrentDelay = ""
	})

	logID := fmt.Sprintf("BCAST-%d", time.Now().Unix())
	newLog := BroadcastLog{
		ID:        logID,
		Timestamp: time.Now().Format(time.RFC3339),
		Total:     len(payload.Items),
		Success:   job.SentCount,
		Failed:    job.FailedCount,
		Details:   details,
	}

	broadcastLogsMu.Lock()
	broadcastLogs = append([]BroadcastLog{newLog}, broadcastLogs...)
	saveBroadcastLogs()
	broadcastLogsMu.Unlock()

	if job.FailedCount > 0 {
		sendDiscordWebhook(payload.UserName, payload.SessionPhone, newLog)
	}
}

// ===== Helpers =====
func formatPhoneNumber(phone string) string {
	var digits strings.Builder
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits.WriteRune(c)
		}
	}
	result := digits.String()
	if strings.HasPrefix(result, "0") {
		result = "62" + result[1:]
	}
	return result
}

func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func randomDelay(minMs, maxMs int) int {
	return minMs + rand.Intn(maxMs-minMs+1)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ===== Pairing Code =====
func requestPairingCode(ws *WaSession, targetWaPhone string) (string, error) {
	if ws.Client.Store.ID != nil {
		return "", fmt.Errorf("sudah login")
	}

	// Need to connect first if not connected
	if !ws.Client.IsConnected() {
		err := ws.Client.Connect()
		if err != nil {
			return "", fmt.Errorf("gagal connect: %v", err)
		}
		// Wait briefly for connection
		time.Sleep(2 * time.Second)
	}

	code, err := ws.Client.PairPhone(context.Background(), targetWaPhone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		return "", err
	}
	return code, nil
}

// ===== Reconnect =====
func reconnectWA(ws *WaSession) {
	log.Printf("🔄 [%s] Re-initializing WhatsApp client...\n", ws.UserPhone)
	if ws.Client != nil {
		ws.Client.Disconnect()
	}

	// Delete session store to force new QR
	ws.Client.Store.Delete(context.Background())

	// Re-init
	deviceStore, err := ws.Container.GetFirstDevice(context.Background())
	if err != nil {
		log.Printf("❌ [%s] Gagal get device: %v\n", ws.UserPhone, err)
		return
	}

	ws.Client = whatsmeow.NewClient(deviceStore, ws.Client.Log)
	registerEventHandlers(ws)
	connectWithQR(ws)
}
