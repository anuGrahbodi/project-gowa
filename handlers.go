package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// ===== Auth Helpers =====
func hashPin(pin string) string {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		secret = "fallback_secret_change_me_now"
	}
	salt := "wa-bot-salt-2026"
	mac := hmac.New(sha256.New, []byte(secret+salt))
	mac.Write([]byte(pin))
	return hex.EncodeToString(mac.Sum(nil))
}

func generateToken() string {
	b := make([]byte, 48)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func signToken(token, tokenType, phone string) string {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	now := time.Now().UnixMilli()
	sessions[token] = &Session{
		Token:   token,
		Type:    tokenType,
		Phone:   phone,
		Issued:  now,
		Expires: now + SessionDurationMs,
	}
	return token
}

func validateToken(token string) *Session {
	if token == "" {
		log.Println("validateToken failed: empty token")
		return nil
	}
	sessionsMu.RLock()
	s, ok := sessions[token]
	sessionsMu.RUnlock()
	if !ok {
		log.Printf("validateToken failed: token %s not found in sessions map\n", token)
		return nil
	}
	if time.Now().UnixMilli() > s.Expires {
		log.Printf("validateToken failed: token %s expired\n", token)
		sessionsMu.Lock()
		delete(sessions, token)
		sessionsMu.Unlock()
		return nil
	}
	return s
}

func getIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}

// ===== Auth Middleware =====
type AuthSession struct {
	Type  string
	Phone string
}

type contextKey string

const sessionCtxKey contextKey = "session"

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var token string
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = authHeader[7:]
		} else {
			token = r.URL.Query().Get("_token")
		}
		session := validateToken(token)
		if session == nil {
			log.Printf("requireAuth failed: token '%s' is invalid. Auth Header: '%s', URL: %s\n", token, authHeader, r.URL.Path)
			jsonResponse(w, 401, map[string]interface{}{
				"error":      "Unauthorized. Silakan login terlebih dahulu.",
				"needsLogin": true,
			})
			return
		}
		// Store session type in header for downstream use
		r.Header.Set("X-Session-Type", session.Type)
		r.Header.Set("X-Session-Phone", session.Phone)
		next(w, r)
	}
}

func isGuest(r *http.Request) bool {
	return r.Header.Get("X-Session-Type") == "guest"
}

// ===== Auth Handlers =====
func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	usersMu.RLock()
	count := len(users)
	usersMu.RUnlock()
	
	jsonResponse(w, 200, map[string]interface{}{
		"usersCount": count,
		"needsSetup": count == 0,
	})
}

func handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	usersMu.Lock()
	defer usersMu.Unlock()

	if len(users) >= 10 {
		jsonError(w, 400, "Batas maksimal 10 pengguna telah tercapai.")
		return
	}

	var body struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
		Pin   string `json:"pin"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	phoneStr := formatPhoneNumber(body.Phone)
	if phoneStr == "" {
		jsonError(w, 400, "Nomor HP wajib diisi dengan benar.")
		return
	}
	if body.Name == "" {
		jsonError(w, 400, "Nama wajib diisi.")
		return
	}

	if _, ok := users[phoneStr]; ok {
		jsonError(w, 400, "Nomor HP sudah terdaftar.")
		return
	}

	matched, _ := regexp.MatchString(`^\d{6}$`, body.Pin)
	if !matched {
		jsonError(w, 400, "PIN harus tepat 6 digit angka.")
		return
	}
	weak := []string{"000000", "111111", "222222", "333333", "444444", "555555", "666666", "777777", "888888", "999999", "123456", "654321", "012345", "098765"}
	for _, w2 := range weak {
		if body.Pin == w2 {
			jsonError(w, 400, "PIN terlalu mudah ditebak. Pilih PIN yang lebih kuat.")
			return
		}
	}

	users[phoneStr] = &User{
		Phone:          phoneStr,
		Name:           body.Name,
		PinHash:        hashPin(body.Pin),
		FailedAttempts: 0,
		Locked:         false,
		CreatedAt:      time.Now().Format(time.RFC3339),
	}
	saveUsers()

	token := generateToken()
	signToken(token, "user", phoneStr)
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "token": token})
}

func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Phone string `json:"phone"`
		Pin   string `json:"pin"`
	}
	if err := parseBody(r, &body); err != nil || body.Pin == "" || body.Phone == "" {
		jsonError(w, 400, "Nomor HP dan PIN wajib diisi.")
		return
	}

	phoneStr := formatPhoneNumber(body.Phone)

	usersMu.Lock()
	user, ok := users[phoneStr]
	if !ok {
		usersMu.Unlock()
		jsonError(w, 404, "Nomor HP belum terdaftar.")
		return
	}

	if user.Locked {
		usersMu.Unlock()
		jsonResponse(w, 429, map[string]interface{}{
			"error":  "Akun terkunci karena salah PIN 5 kali. Hubungi Admin.",
			"locked": true,
		})
		return
	}

	expected := hashPin(body.Pin)
	if !hmac.Equal([]byte(expected), []byte(user.PinHash)) {
		user.FailedAttempts++
		
		if user.FailedAttempts >= 5 {
			user.Locked = true
			
			saveUsers()
			usersMu.Unlock()
			
			log.Printf("⚠️ Akun %s terkunci karena salah PIN 5x.\n", user.Phone)
			
			jsonResponse(w, 429, map[string]interface{}{
				"error":  "Terlalu banyak percobaan. Akun dikunci.",
				"locked": true,
			})
			return
		}
		
		attempts := user.FailedAttempts
		saveUsers()
		usersMu.Unlock()
		
		jsonResponse(w, 401, map[string]interface{}{
			"error":        fmt.Sprintf("PIN salah! (%d/5)", attempts),
			"attemptsUsed": attempts,
		})
		return
	}

	user.FailedAttempts = 0
	saveUsers()
	usersMu.Unlock()

	token := generateToken()
	signToken(token, "user", phoneStr)
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "token": token})
}

func handleAuthRequestReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Phone string `json:"phone"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	phoneStr := formatPhoneNumber(body.Phone)

	usersMu.Lock()
	defer usersMu.Unlock()

	user, ok := users[phoneStr]
	if !ok {
		jsonError(w, 404, "Nomor belum terdaftar.")
		return
	}

	// Generate 8-digit OTP
	b := make([]byte, 4)
	rand.Read(b)
	otpStr := fmt.Sprintf("%08d", int(b[0])<<24|int(b[1])<<16|int(b[2])<<8|int(b[3]))
	if len(otpStr) > 8 {
		otpStr = otpStr[len(otpStr)-8:]
	}
	user.ResetOTP = otpStr
	user.OtpExpiry = time.Now().UnixMilli() + 30*60*1000 // 30 mins

	saveUsers()
	
	go sendOtpAlert(user.Name, user.Phone, otpStr)

	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

func handleAuthResetPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Phone  string `json:"phone"`
		Otp    string `json:"otp"`
		NewPin string `json:"newPin"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	phoneStr := formatPhoneNumber(body.Phone)

	usersMu.Lock()
	defer usersMu.Unlock()

	user, ok := users[phoneStr]
	if !ok {
		jsonError(w, 404, "Nomor belum terdaftar.")
		return
	}

	if user.ResetOTP == "" {
		jsonError(w, 400, "Anda belum meminta OTP reset. Silakan minta OTP terlebih dahulu.")
		return
	}

	if time.Now().UnixMilli() > user.OtpExpiry {
		jsonError(w, 400, "OTP sudah kadaluarsa. Silakan minta OTP baru.")
		return
	}

	if user.ResetOTP != body.Otp {
		jsonError(w, 400, "OTP salah.")
		return
	}

	matched, _ := regexp.MatchString(`^\d{6}$`, body.NewPin)
	if !matched {
		jsonError(w, 400, "PIN harus tepat 6 digit angka.")
		return
	}
	
	user.PinHash = hashPin(body.NewPin)
	user.Locked = false
	user.FailedAttempts = 0
	user.ResetOTP = ""
	user.OtpExpiry = 0
	saveUsers()

	token := generateToken()
	signToken(token, "user", phoneStr)
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "token": token})
}

func handleAuthGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
	}
	if err := parseBody(r, &body); err != nil || body.Phone == "" {
		jsonError(w, 400, "Nomor HP wajib diisi.")
		return
	}
	formatted := formatPhoneNumber(body.Phone)
	if len(formatted) < 10 {
		jsonError(w, 400, "Nomor tidak valid.")
		return
	}
	token := generateToken()
	signToken(token, "guest", formatted)
	jsonResponse(w, 200, map[string]interface{}{
		"ok":         true,
		"token":      token,
		"guestPhone": formatted,
	})
}

func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := authHeader[7:]
		sessionsMu.Lock()
		delete(sessions, token)
		sessionsMu.Unlock()
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

// ===== Status & Login Handlers =====
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	isGuestUser := isGuest(r)
	jsonResponse(w, 200, map[string]interface{}{
		"ready":          ws.IsReady,
		"qr":             ws.LatestQrDataUrl,
		"phoneLoginMode": ws.PhoneLoginMode,
		"isGuest":        isGuestUser,
		"guestPhone":     sessionPhone,
	})
}

func handleLoginMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if body.Mode == "phone" {
		ws.PhoneLoginMode = true
		ws.LatestQrDataUrl = ""
		log.Printf("📞 [%s] Mode login diubah ke: Nomor HP (QR dimatikan)\n", sessionPhone)
	} else {
		ws.PhoneLoginMode = false
		log.Printf("📷 [%s] Mode login diubah ke: Scan QR, meminta sesi QR baru...\n", sessionPhone)
		if ws.Client != nil && ws.Client.Store.ID == nil {
			go reconnectWA(ws)
		}
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "phoneLoginMode": ws.PhoneLoginMode})
}

func handleLoginPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || ws.Client == nil {
		jsonError(w, 400, "Client belum terinisiasi.")
		return
	}
	if ws.IsReady {
		jsonError(w, 400, "Sudah login.")
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := parseBody(r, &body); err != nil || body.Phone == "" {
		jsonError(w, 400, "Nomor HP wajib diisi.")
		return
	}
	formatted := formatPhoneNumber(body.Phone)
	log.Println("📱 Meminta pairing code untuk:", formatted)

	code, err := requestPairingCode(ws, formatted)
	if err != nil {
		log.Println("❌ Gagal request pairing code:", err)
		jsonError(w, 500, "Gagal: "+err.Error())
		return
	}
	log.Println("✅ KODE PAIRING BERHASIL DIDAPATKAN:", code)
	jsonResponse(w, 200, map[string]interface{}{"code": code})
}

// ===== Lists Handlers =====
func handleGetLists(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonResponse(w, 200, map[string]interface{}{})
		return
	}
	listsMu.RLock()
	defer listsMu.RUnlock()
	jsonResponse(w, 200, lists)
}

func handlePostList(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	var body struct {
		Name string `json:"name"`
		CSV  string `json:"csv"`
	}
	if err := parseBody(r, &body); err != nil || body.Name == "" || body.CSV == "" {
		jsonError(w, 400, "Nama dan CSV wajib diisi")
		return
	}

	lines := strings.Split(strings.TrimSpace(body.CSV), "\n")
	if len(lines) < 2 {
		jsonError(w, 400, "CSV harus punya header + minimal 1 baris data")
		return
	}

	headers := strings.Split(lines[0], ",")
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}

	var data []map[string]string
	for i := 1; i < len(lines); i++ {
		cols := strings.Split(lines[i], ",")
		for j, c := range cols {
			cols[j] = strings.TrimSpace(c)
		}
		if len(cols) < len(headers) {
			continue
		}
		obj := make(map[string]string)
		for j, h := range headers {
			obj[h] = cols[j]
		}
		data = append(data, obj)
	}

	listsMu.Lock()
	lists[body.Name] = data
	saveLists(lists)
	listsMu.Unlock()

	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

func handleDeleteList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/lists/")
	if name == "" {
		jsonError(w, 400, "Nama list wajib")
		return
	}
	listsMu.Lock()
	if _, ok := lists[name]; !ok {
		listsMu.Unlock()
		jsonError(w, 404, "List tidak ditemukan")
		return
	}
	delete(lists, name)
	saveLists(lists)
	listsMu.Unlock()
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

// ===== Targets Handlers =====
func handleGetTargets(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonResponse(w, 200, []interface{}{})
		return
	}
	targetsMu.RLock()
	defer targetsMu.RUnlock()
	jsonResponse(w, 200, targets)
}

func handlePostTarget(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	var body struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := parseBody(r, &body); err != nil || body.ID == "" {
		jsonError(w, 400, "ID tujuan wajib diisi")
		return
	}
	formatted := strings.TrimSpace(body.ID)
	if !strings.Contains(formatted, "@") {
		formatted += "@c.us"
	}
	targetsMu.Lock()
	for _, t := range targets {
		if t.ID == formatted {
			targetsMu.Unlock()
			jsonError(w, 400, "Target sudah ada di daftar")
			return
		}
	}
	label := strings.TrimSpace(body.Label)
	if label == "" {
		label = formatted
	}
	targets = append(targets, Target{ID: formatted, Label: label})
	saveTargets(targets)
	targetsMu.Unlock()
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

func handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	idxStr := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		jsonError(w, 400, "Index tidak valid")
		return
	}
	targetsMu.Lock()
	if idx >= len(targets) {
		targetsMu.Unlock()
		jsonError(w, 400, "Index tidak valid")
		return
	}
	targets = append(targets[:idx], targets[idx+1:]...)
	saveTargets(targets)
	targetsMu.Unlock()
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

// ===== Schedules Handlers =====
func handleGetSchedules(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonResponse(w, 200, []interface{}{})
		return
	}
	schedMu.RLock()
	defer schedMu.RUnlock()
	jsonResponse(w, 200, schedules)
}

func handlePostSchedule(w http.ResponseWriter, r *http.Request) {
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	var body struct {
		Time         int64       `json:"time"`
		Payload      interface{} `json:"payload"`
		Type         string      `json:"type"`
		ScheduleType string      `json:"scheduleType"`
		CronDays     []string    `json:"cronDays"`
		CronTime     string      `json:"cronTime"`
	}
	if err := parseBody(r, &body); err != nil || body.Payload == nil || body.Type == "" {
		jsonError(w, 400, "Data jadwal tidak lengkap")
		return
	}

	scheduleType := body.ScheduleType
	if scheduleType == "" {
		scheduleType = "once"
	}

	newSchedule := Schedule{
		ID:            strconv.FormatInt(time.Now().UnixMilli(), 10),
		ScheduleType:  scheduleType,
		TimeToProcess: body.Time,
		CronDays:      body.CronDays,
		CronTime:      body.CronTime,
		Type:          body.Type,
		Payload:       body.Payload,
		Status:        "pending",
		CreatedAt:     time.Now().Format("2/1/2006 15:04:05"),
		LastRunDate:   nil,
		UserPhone:     sessionPhone,
	}

	schedMu.Lock()
	schedules = append(schedules, newSchedule)
	saveSchedules(schedules)
	schedMu.Unlock()

	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

func handleScheduleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	if id == "" {
		jsonError(w, 400, "ID jadwal wajib")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		handleDeleteSchedule(w, r, id)
	case http.MethodPut:
		handleUpdateSchedule(w, r, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleDeleteSchedule(w http.ResponseWriter, r *http.Request, id string) {
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	schedMu.Lock()
	var newSchedules []Schedule
	for _, s := range schedules {
		if s.ID != id {
			newSchedules = append(newSchedules, s)
		}
	}
	schedules = newSchedules
	saveSchedules(schedules)
	schedMu.Unlock()
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

func handleUpdateSchedule(w http.ResponseWriter, r *http.Request, id string) {
	if isGuest(r) {
		jsonError(w, 403, "Mode tamu terbatas.")
		return
	}
	var body struct {
		Message      *string  `json:"message,omitempty"`
		ScheduleType string   `json:"scheduleType"`
		Time         int64    `json:"time"`
		CronDays     []string `json:"cronDays"`
		CronTime     string   `json:"cronTime"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	schedMu.Lock()
	defer schedMu.Unlock()

	idx := -1
	for i, s := range schedules {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		jsonError(w, 404, "Jadwal tidak ditemukan")
		return
	}

	s := &schedules[idx]

	// Update message in payload if provided
	if body.Message != nil {
		if s.Type == "grup" {
			payloadBytes, _ := json.Marshal(s.Payload)
			var p map[string]interface{}
			json.Unmarshal(payloadBytes, &p)
			p["message"] = *body.Message
			s.Payload = p
		} else if s.Type == "pribadi" {
			payloadBytes, _ := json.Marshal(s.Payload)
			var arr []map[string]interface{}
			json.Unmarshal(payloadBytes, &arr)
			for i := range arr {
				arr[i]["message"] = *body.Message
			}
			s.Payload = arr
		}
	}

	if body.ScheduleType != "" {
		s.ScheduleType = body.ScheduleType
	}
	if body.ScheduleType == "once" && body.Time > 0 {
		s.TimeToProcess = body.Time
		s.CronDays = nil
		s.CronTime = ""
	}
	if body.ScheduleType == "recurring" && len(body.CronDays) > 0 && body.CronTime != "" {
		s.CronDays = body.CronDays
		s.CronTime = body.CronTime
		s.TimeToProcess = 0
	}
	s.Status = "pending"
	s.LastRunDate = nil

	saveSchedules(schedules)
	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

// ===== Send Message Handlers =====
func handleSendGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || !ws.IsReady {
		jsonError(w, 400, "WhatsApp Client belum ready.")
		return
	}

	var body struct {
		Message         string `json:"message"`
		SelectedTargets []int  `json:"selectedTargets"`
		HideTag         bool   `json:"hideTag"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	targetsMu.RLock()
	var sendTargets []Target
	if len(body.SelectedTargets) > 0 {
		for _, i := range body.SelectedTargets {
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
		jsonError(w, 400, "Tidak ada target yang dipilih.")
		return
	}

	listsMu.RLock()
	finalMessage := body.Message
	var mentionIDs []string
	for key, list := range lists {
		placeholder := "${" + key + "}"
		if strings.Contains(body.Message, placeholder) {
			finalMessage = strings.ReplaceAll(finalMessage, placeholder, processList(list))
			mentionIDs = append(mentionIDs, getMentionsList(list)...)
		}
	}
	listsMu.RUnlock()

	if body.HideTag {
		for _, target := range sendTargets {
			if strings.HasSuffix(target.ID, "@g.us") {
				jid, err := types.ParseJID(target.ID)
				if err == nil {
					participants, pErr := getGroupParticipants(ws, jid)
					if pErr == nil {
						for _, p := range participants {
							mentionIDs = append(mentionIDs, p.JID.String())
						}
						log.Printf("👻 Hide tag: %d anggota dari %s\n", len(participants), target.Label)
					}
				}
			}
		}
	}

	mentionIDs = uniqueStrings(mentionIDs)
	log.Printf("🚀 Mengirim pesan ke %d target...\n", len(sendTargets))

	type Result struct {
		ID    string `json:"id"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	var results []Result

	for _, target := range sendTargets {
		jid, err := types.ParseJID(target.ID)
		if err != nil {
			results = append(results, Result{ID: target.ID, OK: false, Error: err.Error()})
			continue
		}

		if len(mentionIDs) > 0 {
			err = sendTextMessage(ws, jid, finalMessage, mentionIDs)
		} else {
			err = sendSimpleMessage(ws, jid, finalMessage)
		}

		if err != nil {
			results = append(results, Result{ID: target.ID, OK: false, Error: err.Error()})
			log.Printf("  ❌ Gagal ke %s: %v\n", target.Label, err)
		} else {
			results = append(results, Result{ID: target.ID, OK: true})
			log.Printf("  ✅ Terkirim ke %s\n", target.Label)
		}
	}

	sent := 0
	for _, r := range results {
		if r.OK {
			sent++
		}
	}
	log.Printf("🎉 Selesai: %d/%d berhasil.\n", sent, len(sendTargets))

	jsonResponse(w, 200, map[string]interface{}{
		"ok":      sent == len(results),
		"sent":    sent,
		"total":   len(sendTargets),
		"results": results,
	})
}

func handleSendPrivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || !ws.IsReady {
		jsonError(w, 400, "WhatsApp Client belum ready.")
		return
	}
	var body struct {
		Target  string `json:"target"`
		Message string `json:"message"`
	}
	if err := parseBody(r, &body); err != nil || body.Target == "" || body.Message == "" {
		jsonError(w, 400, "Target dan message wajib diisi.")
		return
	}

	chatID := formatPhoneNumber(body.Target)
	jid := types.NewJID(chatID, types.DefaultUserServer)
	log.Printf("🚀 [Private] Mengirim ke %s...\n", chatID)

	err := sendSimpleMessage(ws, jid, body.Message)
	if err != nil {
		log.Printf("❌ [Private] Gagal ke %s: %v\n", chatID, err)
		jsonError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "id": chatID + "@c.us"})
}

func handleDefaultMessage(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]interface{}{"message": buildDefaultMessage()})
}

// ===== Group Members Handler =====
func handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || !ws.IsReady {
		jsonError(w, 400, "WhatsApp Client belum ready.")
		return
	}

	groupIDStr := strings.TrimPrefix(r.URL.Path, "/api/group-members/")
	if groupIDStr == "" {
		jsonError(w, 400, "Group ID wajib")
		return
	}

	jid, err := types.ParseJID(groupIDStr)
	if err != nil {
		jsonError(w, 400, "JID tidak valid: "+err.Error())
		return
	}

	groupInfo, err := getGroupInfo(ws, jid)
	if err != nil {
		jsonError(w, 500, "Gagal mengambil info grup: "+err.Error())
		return
	}

	type Member struct {
		Nama         string `json:"nama"`
		Nomor        string `json:"nomor"`
		IsAdmin      bool   `json:"isAdmin"`
		IsSuperAdmin bool   `json:"isSuperAdmin"`
	}

	var members []Member
	for _, p := range groupInfo.Participants {
		nomor := p.JID.User
		if strings.HasPrefix(nomor, "0") {
			nomor = "62" + nomor[1:]
		} else if !strings.HasPrefix(nomor, "62") {
			nomor = "62" + nomor
		}

		nama := nomor
		// Try to get contact name - whatsmeow doesn't fetch pushnames easily
		// Use JID user as fallback
		contact, err := ws.Client.Store.Contacts.GetContact(context.Background(), p.JID)
		if err == nil && contact.FullName != "" {
			nama = contact.FullName
		} else if err == nil && contact.PushName != "" {
			nama = contact.PushName
		}

		members = append(members, Member{
			Nama:         nama,
			Nomor:        nomor,
			IsAdmin:      p.IsAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}

	// Sort: super admin first, then admin, then regular
	sort.Slice(members, func(i, j int) bool {
		if members[i].IsSuperAdmin && !members[j].IsSuperAdmin {
			return true
		}
		if !members[i].IsSuperAdmin && members[j].IsSuperAdmin {
			return false
		}
		if members[i].IsAdmin && !members[j].IsAdmin {
			return true
		}
		if !members[i].IsAdmin && members[j].IsAdmin {
			return false
		}
		return members[i].Nama < members[j].Nama
	})

	log.Printf("👥 Fetched %d anggota dari grup %s\n", len(members), groupIDStr)
	jsonResponse(w, 200, map[string]interface{}{
		"groupName": groupInfo.Name,
		"members":   members,
	})
}

// ===== Monitor Handlers =====
func handleMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)

	ws.MsgMu.Lock()
	msgs := make([]MonitorMessage, len(ws.IncomingMsgs))
	copy(msgs, ws.IncomingMsgs)
	ws.MsgMu.Unlock()
	jsonResponse(w, 200, map[string]interface{}{
		"on":       ws.MonitorOn,
		"messages": msgs,
	})
}

func handleMonitorToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)

	ws.MonitorOn = !ws.MonitorOn
	if !ws.MonitorOn {
		ws.MsgMu.Lock()
		ws.IncomingMsgs = make([]MonitorMessage, 0)
		ws.MsgMu.Unlock()
	}
	log.Printf("🔔 [%s] Monitor %s\n", sessionPhone, map[bool]string{true: "ON", false: "OFF"}[ws.MonitorOn])
	jsonResponse(w, 200, map[string]interface{}{"on": ws.MonitorOn})
}

// ===== WA Logout Handler =====
func handleWALogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || ws.Client == nil {
		jsonError(w, 400, "Client belum terinisiasi.")
		return
	}

	userName := "Tidak Diketahui"
	if sessionPhone != "" {
		usersMu.RLock()
		if u, ok := users[sessionPhone]; ok {
			userName = u.Name
		}
		usersMu.RUnlock()
	}

	log.Printf("🚪 [%s] Memproses logout...\n", sessionPhone)
	go sendLogoutAlert(userName, sessionPhone)

	ws.IsReady = false
	ws.LatestQrDataUrl = ""
	ws.MonitorOn = false
	ws.MsgMu.Lock()
	ws.IncomingMsgs = make([]MonitorMessage, 0)
	ws.MsgMu.Unlock()

	// Logout & disconnect
	ws.Client.Logout(context.Background())

	// Re-initialize in background
	go func() {
		time.Sleep(1 * time.Second)
		reconnectWA(ws)
	}()

	jsonResponse(w, 200, map[string]interface{}{"ok": true})
}

// ===== System Update Handler =====
func handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log.Println("🔄 Menerima perintah Auto-Update dari web...")
	cmd := exec.Command("sh", "-c", "git pull https://github.com/anuGrahbodi/project-gowa main && pm2 restart bot-wa --update-env")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("❌ Gagal Update: %v\n", err)
		jsonError(w, 500, err.Error())
		return
	}
	log.Printf("✅ Update Berhasil:\n%s\n", string(output))
	jsonResponse(w, 200, map[string]interface{}{"ok": true, "output": string(output)})
}

// ===== Broadcast Excel =====
func formatExcelNumber(val string) string {
	re := regexp.MustCompile(`\D`)
	s := re.ReplaceAllString(val, "")
	if strings.HasPrefix(s, "08") {
		s = "628" + s[2:]
	} else if strings.HasPrefix(s, "8") {
		s = "628" + s[1:]
	}
	return s
}

func handleBroadcastExcel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionPhone := r.Header.Get("X-Session-Phone")
	ws := getOrCreateWaSession(sessionPhone)
	if ws == nil || !ws.IsReady {
		jsonError(w, 400, "WhatsApp Client belum ready.")
		return
	}

	var body struct {
		Payload []struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		} `json:"payload"`
		MinDelay int `json:"minDelay"`
		MaxDelay int `json:"maxDelay"`
	}
	if err := parseBody(r, &body); err != nil {
		jsonError(w, 400, "Bad request")
		return
	}

	if len(body.Payload) == 0 {
		jsonError(w, 400, "Payload kosong.")
		return
	}

	userName := "Unknown"
	if sessionPhone != "" {
		usersMu.RLock()
		if u, ok := users[sessionPhone]; ok {
			userName = u.Name
		}
		usersMu.RUnlock()
	}

	payloadWithConfig := map[string]interface{}{
		"items": body.Payload,
		"minDelay": body.MinDelay,
		"maxDelay": body.MaxDelay,
		"userName": userName,
		"sessionPhone": sessionPhone,
	}

	newSchedule := Schedule{
		ID:            strconv.FormatInt(time.Now().UnixMilli(), 10),
		ScheduleType:  "once",
		TimeToProcess: time.Now().UnixMilli(),
		Type:          "excel_broadcast",
		Payload:       payloadWithConfig,
		Status:        "pending",
		CreatedAt:     time.Now().Format("2/1/2006 15:04:05"),
		TotalTargets:  len(body.Payload),
		SentCount:     0,
		FailedCount:   0,
		CurrentTarget: "Menunggu giliran...",
		CurrentDelay:  "",
	}

	schedMu.Lock()
	schedules = append(schedules, newSchedule)
	saveSchedules(schedules)
	schedMu.Unlock()

	jsonResponse(w, 200, map[string]interface{}{
		"ok":      true,
		"message": "Broadcast masuk ke antrean Pesan Berjadwal dan akan diproses di latar belakang...",
		"total":   len(body.Payload),
	})
}

func handleGetBroadcastLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	broadcastLogsMu.RLock()
	defer broadcastLogsMu.RUnlock()
	jsonResponse(w, 200, broadcastLogs)
}

func sendDiscordWebhook(userName, userPhone string, bLog BroadcastLog) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	failedList := ""
	for _, d := range bLog.Details {
		if d.Status == "failed" {
			failedList += fmt.Sprintf("- **%s**: %s\n", d.Target, d.Message)
		}
	}
	if len(failedList) > 1000 {
		failedList = failedList[:1000] + "...\n(terpotong)"
	}

	embed := map[string]interface{}{
		"title":       "⚠️ Notifikasi Broadcast Gagal",
		"description": fmt.Sprintf("Terdapat pengiriman broadcast yang gagal.\n\n**Total:** %d\n**Sukses:** %d\n**Gagal:** %d\n\n**Daftar Gagal:**\n%s", bLog.Total, bLog.Success, bLog.Failed, failedList),
		"color":       16711680, // Red
		"fields": []map[string]interface{}{
			map[string]interface{}{"name": "Pengirim", "value": userName, "inline": true},
			map[string]interface{}{"name": "Nomor Sesi", "value": userPhone, "inline": true},
			map[string]interface{}{"name": "Log ID", "value": bLog.ID, "inline": true},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", webhookURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	client.Do(req)
}
