package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// ===== Global State =====
var (
	waSessions   = make(map[string]*WaSession)
	waSessionsMu sync.RWMutex

	lists     map[string][]map[string]string
	targets   []Target
	schedules []Schedule
	listsMu   sync.RWMutex
	targetsMu sync.RWMutex
	schedMu   sync.RWMutex

	broadcastLogs     []BroadcastLog
	broadcastLogsMu   sync.RWMutex

	// Auth
	sessions     map[string]*Session
	sessionsMu   sync.RWMutex
	users        map[string]*User
	usersMu      sync.RWMutex

	// File paths
	dataDir           string
	listsFile         string
	targetsFile       string
	schedulesFile     string
	usersFile         string
	broadcastLogsFile string

)

const (
	MaxAttempts     = 5
	LockDurationMs  = 60000
	SessionDurationMs = 8 * 60 * 60 * 1000
	Port            = 3000
)

// ===== Data Structures =====
type WaSession struct {
	Client          *whatsmeow.Client
	Container       *sqlstore.Container
	IsReady         bool
	LatestQrDataUrl string
	PhoneLoginMode  bool
	MonitorOn       bool
	IncomingMsgs    []MonitorMessage
	MsgMu           sync.Mutex
	QrChan          <-chan whatsmeow.QRChannelItem
	QrChanMu        sync.Mutex
	HeartbeatStop   chan struct{}
	UserPhone       string
}

type Target struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Schedule struct {
	ID            string      `json:"id"`
	ScheduleType  string      `json:"scheduleType"` // "once" or "recurring"
	TimeToProcess int64       `json:"timeToProcess"`
	CronDays      []string    `json:"cronDays"`
	CronTime      string      `json:"cronTime"`
	Type          string      `json:"type"` // "grup", "pribadi", "excel_broadcast"
	Payload       interface{} `json:"payload"`
	Status        string      `json:"status"`
	CreatedAt     string      `json:"createdAt"`
	LastRunDate   *string     `json:"lastRunDate"`
	UserPhone     string      `json:"userPhone,omitempty"`

	// Progress Tracking
	TotalTargets  int         `json:"totalTargets,omitempty"`
	SentCount     int         `json:"sentCount,omitempty"`
	FailedCount   int         `json:"failedCount,omitempty"`
	CurrentTarget string      `json:"currentTarget,omitempty"`
	CurrentDelay  string      `json:"currentDelay,omitempty"`
}

type MonitorMessage struct {
	Time    string `json:"time"`
	From    string `json:"from"`
	Body    string `json:"body"`
	IsGroup bool   `json:"isGroup"`
}

type Session struct {
	Token   string `json:"token"`
	Type    string `json:"type"` // "admin", "user", "guest"
	Phone   string `json:"phone,omitempty"`
	Issued  int64  `json:"issued"`
	Expires int64  `json:"expires"`
}

type User struct {
	Phone          string `json:"phone"`
	Name           string `json:"name"`
	PinHash        string `json:"pinHash"`
	FailedAttempts int    `json:"failedAttempts"`
	Locked         bool   `json:"locked"`
	ResetOTP       string `json:"resetOTP"`
	OtpExpiry      int64  `json:"otpExpiry"`
	CreatedAt      string `json:"createdAt"`
}

type BroadcastLogDetail struct {
	Target      string `json:"target"`
	Status      string `json:"status"` // "success" or "failed"
	Message     string `json:"message"` // Error message if failed
	SentMessage string `json:"sentMessage,omitempty"` // The actual WA message content
}

type BroadcastLog struct {
	ID        string               `json:"id"`
	Timestamp string               `json:"timestamp"`
	Total     int                  `json:"total"`
	Success   int                  `json:"success"`
	Failed    int                  `json:"failed"`
	Details   []BroadcastLogDetail `json:"details"`
}

func main() {
	// Load .env
	godotenv.Load()

	// Init paths
	dataDir, _ = os.Getwd()
	listsFile = filepath.Join(dataDir, "lists.json")
	targetsFile = filepath.Join(dataDir, "targets.json")
	schedulesFile = filepath.Join(dataDir, "schedules.json")
	usersFile = filepath.Join(dataDir, "users.json")
	broadcastLogsFile = filepath.Join(dataDir, "broadcast_logs.json")

	// Init in-memory stores
	sessions = make(map[string]*Session)
	users = loadUsers()

	// Load data from JSON files
	lists = loadLists()
	targets = loadTargets()
	schedules = loadSchedules()
	broadcastLogs = loadBroadcastLogs()

	// Init WhatsApp for all users
	for phone := range users {
		go getOrCreateWaSession(phone)
	}

	// Start background schedule processor
	go scheduleProcessor()

	// Start session cleanup
	go sessionCleaner()

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Serve static files
	publicDir := filepath.Join(dataDir, "public")
	fileServer := http.FileServer(http.Dir(publicDir))

	// Auth API
	mux.HandleFunc("/api/auth/status", handleAuthStatus)
	mux.HandleFunc("/api/auth/setup", handleAuthSetup)
	mux.HandleFunc("/api/auth/login", handleAuthLogin)
	mux.HandleFunc("/api/auth/guest", handleAuthGuest)
	mux.HandleFunc("/api/auth/logout", handleAuthLogout)
	mux.HandleFunc("/api/auth/request-reset", handleAuthRequestReset)
	mux.HandleFunc("/api/auth/reset-pin", handleAuthResetPin)

	// Status & Login
	mux.HandleFunc("/api/status", requireAuth(handleStatus))
	mux.HandleFunc("/api/login/mode", requireAuth(handleLoginMode))
	mux.HandleFunc("/api/login/pair", requireAuth(handleLoginPair))

	// Lists
	mux.HandleFunc("/api/lists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAuth(handleGetLists)(w, r)
		} else if r.Method == http.MethodPost {
			requireAuth(handlePostList)(w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/lists/", requireAuth(handleDeleteList))

	// Targets
	mux.HandleFunc("/api/targets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAuth(handleGetTargets)(w, r)
		} else if r.Method == http.MethodPost {
			requireAuth(handlePostTarget)(w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/targets/", requireAuth(handleDeleteTarget))

	// Schedules
	mux.HandleFunc("/api/schedules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAuth(handleGetSchedules)(w, r)
		} else if r.Method == http.MethodPost {
			requireAuth(handlePostSchedule)(w, r)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/schedules/", requireAuth(handleScheduleByID))

	// Messaging
	mux.HandleFunc("/api/send", requireAuth(handleSendGroup))
	mux.HandleFunc("/api/send-private", requireAuth(handleSendPrivate))
	mux.HandleFunc("/api/default-message", requireAuth(handleDefaultMessage))
	mux.HandleFunc("/api/broadcast-excel", requireAuth(handleBroadcastExcel))
	mux.HandleFunc("/api/broadcast-logs", requireAuth(handleGetBroadcastLogs))

	// Group members
	mux.HandleFunc("/api/group-members/", requireAuth(handleGroupMembers))

	// Monitor
	mux.HandleFunc("/api/monitor", requireAuth(handleMonitor))
	mux.HandleFunc("/api/monitor/toggle", requireAuth(handleMonitorToggle))

	// Logout WA
	mux.HandleFunc("/api/logout", requireAuth(handleWALogout))

	// System update
	mux.HandleFunc("/api/system-update", requireAuth(handleSystemUpdate))

	// Static files (fallback)
	mux.Handle("/", fileServer)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", Port),
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("🛑 Shutting down...")
		waSessionsMu.RLock()
		for _, ws := range waSessions {
			if ws.Client != nil {
				ws.Client.Disconnect()
			}
		}
		waSessionsMu.RUnlock()
		server.Shutdown(context.Background())
	}()

	log.Printf("🌐 Web interface berjalan di http://localhost:%d\n", Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal("Server error:", err)
	}
}

// ===== Data Persistence =====
func loadLists() map[string][]map[string]string {
	data := make(map[string][]map[string]string)
	b, err := os.ReadFile(listsFile)
	if err != nil {
		return data
	}
	json.Unmarshal(b, &data)
	return data
}

func saveLists(data map[string][]map[string]string) {
	b, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile(listsFile, b, 0644)
}

func loadTargets() []Target {
	var data []Target
	b, err := os.ReadFile(targetsFile)
	if err != nil {
		return data
	}
	json.Unmarshal(b, &data)
	return data
}

func saveTargets(data []Target) {
	b, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile(targetsFile, b, 0644)
}

func loadSchedules() []Schedule {
	var data []Schedule
	b, err := os.ReadFile(schedulesFile)
	if err != nil {
		return data
	}
	json.Unmarshal(b, &data)
	return data
}

func saveSchedules(data []Schedule) {
	b, _ := json.MarshalIndent(data, "", "  ")
	os.WriteFile(schedulesFile, b, 0644)
}

func loadUsers() map[string]*User {
	data := make(map[string]*User)
	b, err := os.ReadFile(usersFile)
	if err != nil {
		return data
	}
	json.Unmarshal(b, &data)
	return data
}

func saveUsers() {
	b, _ := json.MarshalIndent(users, "", "  ")
	os.WriteFile(usersFile, b, 0644)
}

func loadBroadcastLogs() []BroadcastLog {
	var data []BroadcastLog
	b, err := os.ReadFile(broadcastLogsFile)
	if err != nil {
		return data
	}
	json.Unmarshal(b, &data)
	return data
}

func saveBroadcastLogs() {
	b, _ := json.MarshalIndent(broadcastLogs, "", "  ")
	os.WriteFile(broadcastLogsFile, b, 0644)
}

// ===== Schedule Processor =====
func scheduleProcessor() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		processSchedules()
	}
}

func updateScheduleField(id string, updater func(s *Schedule)) {
	schedMu.Lock()
	defer schedMu.Unlock()
	for i := range schedules {
		if schedules[i].ID == id {
			updater(&schedules[i])
			saveSchedules(schedules)
			break
		}
	}
}

func processSchedules() {
	schedMu.Lock()

	now := time.Now()
	nowMs := now.UnixMilli()
	currentDay := int(now.Weekday())
	if currentDay == 0 {
		currentDay = 7
	}
	currentDayStr := strconv.Itoa(currentDay)
	currentHM := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	todayStr := now.Format("2006-01-02")

	var jobsToRun []Schedule
	for i := range schedules {
		s := &schedules[i]
		if s.Status != "pending" {
			continue
		}
		if s.ScheduleType == "recurring" {
			found := false
			for _, d := range s.CronDays {
				if d == currentDayStr {
					found = true
					break
				}
			}
			if !found || s.CronTime != currentHM {
				continue
			}
			if s.LastRunDate != nil && *s.LastRunDate == todayStr {
				continue
			}
			s.Status = "processing"
			jobsToRun = append(jobsToRun, *s)
		} else {
			if s.TimeToProcess <= nowMs {
				s.Status = "processing"
				jobsToRun = append(jobsToRun, *s)
			}
		}
	}
	
	if len(jobsToRun) > 0 {
		saveSchedules(schedules)
	}
	schedMu.Unlock()

	for _, job := range jobsToRun {
		// Run asynchronously!
		go func(j Schedule) {
			executeSchedule(&j)

			// Clean up after completion
			if j.ScheduleType == "recurring" {
				updateScheduleField(j.ID, func(s *Schedule) {
					s.Status = "pending"
					s.LastRunDate = &todayStr
				})
			} else {
				if j.Type == "excel_broadcast" {
					updateScheduleField(j.ID, func(s *Schedule) {
						s.Status = "completed"
						s.CurrentTarget = "Selesai"
						s.CurrentDelay = ""
					})
				} else {
					// Remove one-time schedule
					schedMu.Lock()
					var newSchedules []Schedule
					for _, s := range schedules {
						if s.ID != j.ID {
							newSchedules = append(newSchedules, s)
						}
					}
					schedules = newSchedules
					saveSchedules(schedules)
					schedMu.Unlock()
				}
			}
		}(job)
	}
}

func executeSchedule(job *Schedule) {
	log.Printf("⏰ Menjalankan pesan berjadwal [%s] tipe %s...\n", job.ID, job.Type)

	if job.Type == "grup" {
		executeGroupSchedule(job)
	} else if job.Type == "pribadi" {
		executePrivateSchedule(job)
	} else if job.Type == "excel_broadcast" {
		executeExcelBroadcastSchedule(job)
	}
}

// ===== Session Cleaner =====
func sessionCleaner() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().UnixMilli()
		sessionsMu.Lock()
		for k, s := range sessions {
			if now > s.Expires {
				delete(sessions, k)
			}
		}
		sessionsMu.Unlock()
	}
}

// ===== Helper: Build Default Message =====
func buildDefaultMessage() string {
	listsMu.RLock()
	defer listsMu.RUnlock()
	msg := "tes:\n"
	i := 0
	for key := range lists {
		i++
		msg += fmt.Sprintf("\n*List %d:*\n${%s}\n", i, key)
	}
	msg += "hi tes"
	return msg
}

// ===== Helper: Get Phone Key =====
func getPhoneKey(obj map[string]string) string {
	phoneKeys := []string{"nomor", "notelp", "no", "phone", "hp", "telepon", "nohp", "nomer"}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	for _, pk := range phoneKeys {
		for _, k := range keys {
			if strings.EqualFold(k, pk) {
				return k
			}
		}
	}
	if len(keys) > 1 {
		return keys[1]
	}
	return keys[0]
}

// ===== Helper: Process List for Mentions =====
func processList(list []map[string]string) string {
	if len(list) == 0 {
		return ""
	}
	phoneKey := getPhoneKey(list[0])
	nameKey := ""
	for k := range list[0] {
		if k != phoneKey {
			nameKey = k
			break
		}
	}
	if nameKey == "" {
		nameKey = phoneKey
	}
	var sb strings.Builder
	for _, person := range list {
		sb.WriteString(fmt.Sprintf("%s, @%s\n", person[nameKey], person[phoneKey]))
	}
	return sb.String()
}

func getMentionsList(list []map[string]string) []string {
	if len(list) == 0 {
		return nil
	}
	phoneKey := getPhoneKey(list[0])
	var mentions []string
	for _, person := range list {
		mentions = append(mentions, person[phoneKey]+"@s.whatsapp.net")
	}
	return mentions
}

// ===== Helper: JSON Response =====
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]interface{}{"error": msg})
}

// ===== Helper: Parse Request Body =====
func parseBody(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

