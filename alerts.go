package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

func sendLogoutAlert(userName string, userPhone string) {
	publicUrl := os.Getenv("PUBLIC_URL")
	if publicUrl == "" {
		publicUrl = "http://localhost:3000"
	}

	// === 1. Discord Webhook ===
	webhookUrl := strings.TrimSpace(os.Getenv("DISCORD_WEBHOOK_URL"))
	if webhookUrl != "" {
		payload := map[string]interface{}{
			"content":    "🚨 **WHATSAPP DISCONNECTED** 🚨\n\nMesin menyadari bahwa sesi WhatsApp Anda terlempar atau kehilangan koneksi. Segera lakukan pengecekan.\n\n@everyone",
			"username":   "WhatsApp Bot Monitor",
			"avatar_url": "https://upload.wikimedia.org/wikipedia/commons/thumb/6/6b/WhatsApp.svg/512px-WhatsApp.svg.png",
			"embeds": []map[string]interface{}{
				{
					"title":       "⚠️ Peringatan: Bot WhatsApp Terputus (Logout)",
					"description": fmt.Sprintf("**Status:** Disconnected / Auth Failure\n\nSistem mendeteksi bahwa Bot WhatsApp Anda telah terputus (logout). Jadwal pesan otomatis Anda mungkin gagal terkirim.\n\nSilakan segera hubungkan kembali nomor WhatsApp Anda dengan mengklik tautan Login di bawah ini dan memindai QR Code."),
					"color":       15158332,
					"fields": []map[string]interface{}{
						{"name": "🔗 Tautan Buka Dashboard", "value": fmt.Sprintf("[Klik di sini untuk Buka System](%s)", publicUrl)},
						{"name": "Pengirim (User)", "value": userName, "inline": true},
						{"name": "Nomor Sesi", "value": userPhone, "inline": true},
					},
					"footer":    map[string]string{"text": "Diklaim dan dikirim secara otomatis oleh WhatsApp Bot Security."},
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(webhookUrl, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Println("❌ Gagal mengirim Discord alert:", err)
		} else {
			resp.Body.Close()
			log.Println("💬 Berhasil mengirim peringatan logout ke Discord!")
		}
	} else {
		log.Println("⚠️ Discord alert dilewati karena DISCORD_WEBHOOK_URL belum diatur.")
	}

	// === 2. Email Alert ===
	emailUser := strings.TrimSpace(os.Getenv("EMAIL_USER"))
	emailPass := strings.TrimSpace(os.Getenv("EMAIL_PASS"))
	if emailUser != "" && emailPass != "" {
		subject := "🚨 URGENT: Bot WhatsApp Terputus (Logout)!"
		htmlBody := fmt.Sprintf(`
			<div style="font-family: Arial, sans-serif; max-width: 600px; margin: auto; padding: 20px; border: 1px solid #ddd; border-radius: 8px;">
				<h2 style="color: #d9534f; text-align: center;">Peringatan: WhatsApp Disconnected</h2>
				<hr>
				<p><strong>Status:</strong> Disconnected / Auth Failure</p>
				<p>Sistem memantau bahwa Bot WhatsApp Anda <strong>terputus</strong> dari VM. Semua jadwal otomatis dan API pengiriman pesan berhenti beroperasi saat ini.</p>
				<p>Silakan segera login ulang untuk mengaktifkan kembali bot.</p>
				<div style="text-align: center; margin-top: 30px; margin-bottom: 30px;">
					<a href="%s" style="background-color: #008b5e; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; font-size: 16px;">Buka Dashboard Web</a>
				</div>
				<hr>
				<p style="font-size: 12px; color: #888; text-align: center;">Dikirim secara otomatis oleh Sistem Keamanan Bot WhatsApp.</p>
			</div>
		`, publicUrl)

		msg := fmt.Sprintf("From: \"Bot Security\" <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/html; charset=\"utf-8\"\r\n"+
			"\r\n%s", emailUser, emailUser, subject, htmlBody)

		auth := smtp.PlainAuth("", emailUser, emailPass, "smtp.gmail.com")
		err := smtp.SendMail("smtp.gmail.com:587", auth, emailUser, []string{emailUser}, []byte(msg))
		if err != nil {
			log.Println("❌ Gagal mengirim Email alert:", err)
		} else {
			log.Println("📧 Berhasil mengirim peringatan logout ke Email!")
		}
	} else {
		log.Println("⚠️ Email alert dilewati karena EMAIL_USER/EMAIL_PASS belum diatur.")
	}
}

func sendOtpAlert(userName string, userPhone string, otp string) {
	publicUrl := os.Getenv("PUBLIC_URL")
	if publicUrl == "" {
		publicUrl = "http://localhost:3000"
	}

	// === 1. Discord Webhook ===
	webhookUrl := strings.TrimSpace(os.Getenv("DISCORD_WEBHOOK_URL"))
	if webhookUrl != "" {
		payload := map[string]interface{}{
			"content":    fmt.Sprintf("🔑 **PERMINTAAN RESET PIN** 🔑\n\nAda permintaan reset PIN untuk akun **%s** (%s).\n\nKode OTP (8-digit): **%s**\n\nBerlaku selama 30 menit.", userName, userPhone, otp),
			"username":   "WhatsApp Bot Monitor",
			"avatar_url": "https://upload.wikimedia.org/wikipedia/commons/thumb/6/6b/WhatsApp.svg/512px-WhatsApp.svg.png",
			"embeds": []map[string]interface{}{
				{
					"title":       "🔐 OTP Reset PIN",
					"description": "Berikan kode OTP ini jika pengguna memintanya. Jangan bagikan kode ini ke pihak yang tidak dikenal.",
					"color":       3066993, // Greenish blue
					"fields": []map[string]interface{}{
						{"name": "Pengirim (User)", "value": userName, "inline": true},
						{"name": "Nomor Sesi", "value": userPhone, "inline": true},
						{"name": "OTP Code", "value": otp, "inline": false},
					},
					"footer":    map[string]string{"text": "Dikirim secara otomatis oleh WhatsApp Bot Security."},
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(webhookUrl, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Println("❌ Gagal mengirim OTP ke Discord:", err)
		} else {
			resp.Body.Close()
			log.Println("💬 Berhasil mengirim OTP ke Discord!")
		}
	} else {
		log.Println("⚠️ Discord OTP alert dilewati karena DISCORD_WEBHOOK_URL belum diatur.")
	}

	// === 2. Email Alert ===
	emailUser := strings.TrimSpace(os.Getenv("EMAIL_USER"))
	emailPass := strings.TrimSpace(os.Getenv("EMAIL_PASS"))
	if emailUser != "" && emailPass != "" {
		subject := "🔑 OTP Reset PIN Dashboard"
		htmlBody := fmt.Sprintf(`
			<div style="font-family: Arial, sans-serif; max-width: 600px; margin: auto; padding: 20px; border: 1px solid #ddd; border-radius: 8px;">
				<h2 style="color: #0ea5e9; text-align: center;">Permintaan Reset PIN</h2>
				<hr>
				<p>Ada permintaan untuk mereset PIN untuk akun <strong>%s</strong> (%s).</p>
				<p>Gunakan kode OTP berikut untuk mereset PIN:</p>
				<div style="text-align: center; margin-top: 20px; margin-bottom: 20px;">
					<span style="background-color: #f1f5f9; color: #333; padding: 12px 24px; border-radius: 6px; font-weight: bold; font-size: 24px; letter-spacing: 4px;">%s</span>
				</div>
				<p><em>Kode OTP ini berlaku selama 30 menit.</em></p>
				<hr>
				<p style="font-size: 12px; color: #888; text-align: center;">Dikirim secara otomatis oleh Sistem Keamanan Bot WhatsApp.</p>
			</div>
		`, userName, userPhone, otp)

		msg := fmt.Sprintf("From: \"Bot Security\" <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/html; charset=\"utf-8\"\r\n"+
			"\r\n%s", emailUser, emailUser, subject, htmlBody)

		auth := smtp.PlainAuth("", emailUser, emailPass, "smtp.gmail.com")
		err := smtp.SendMail("smtp.gmail.com:587", auth, emailUser, []string{emailUser}, []byte(msg))
		if err != nil {
			log.Println("❌ Gagal mengirim Email OTP:", err)
		} else {
			log.Println("📧 Berhasil mengirim Email OTP!")
		}
	} else {
		log.Println("⚠️ Email OTP alert dilewati karena EMAIL_USER/EMAIL_PASS belum diatur.")
	}
}

func sendFailedBroadcastAlert(userName string, userPhone string, target string, errMsg string) {
	// === 1. Discord Webhook ===
	webhookUrl := strings.TrimSpace(os.Getenv("DISCORD_WEBHOOK_URL"))
	if webhookUrl != "" {
		payload := map[string]interface{}{
			"content":    fmt.Sprintf("⚠️ **BROADCAST GAGAL** ⚠️\n\nPesan gagal dikirim ke target **%s**.\n\nError: %s", target, errMsg),
			"username":   "WhatsApp Bot Monitor",
			"avatar_url": "https://upload.wikimedia.org/wikipedia/commons/thumb/6/6b/WhatsApp.svg/512px-WhatsApp.svg.png",
			"embeds": []map[string]interface{}{
				{
					"title":       "❌ Pesan Gagal Terkirim",
					"color":       15158332, // Red
					"fields": []map[string]interface{}{
						{"name": "Pengirim (User)", "value": userName, "inline": true},
						{"name": "Nomor Sesi", "value": userPhone, "inline": true},
						{"name": "Target Tujuan", "value": target, "inline": false},
						{"name": "Detail Error", "value": errMsg, "inline": false},
					},
					"footer":    map[string]string{"text": "Dikirim secara otomatis oleh WhatsApp Bot Security."},
					"timestamp": time.Now().Format(time.RFC3339),
				},
			},
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(webhookUrl, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Println("❌ Gagal mengirim Failed Broadcast alert ke Discord:", err)
		} else {
			resp.Body.Close()
		}
	}

	// === 2. Email Alert ===
	emailUser := strings.TrimSpace(os.Getenv("EMAIL_USER"))
	emailPass := strings.TrimSpace(os.Getenv("EMAIL_PASS"))
	if emailUser != "" && emailPass != "" {
		subject := "⚠️ Broadcast Gagal Terkirim"
		htmlBody := fmt.Sprintf(`
			<div style="font-family: Arial, sans-serif; max-width: 600px; margin: auto; padding: 20px; border: 1px solid #ddd; border-radius: 8px;">
				<h2 style="color: #d9534f; text-align: center;">Pesan Gagal Terkirim</h2>
				<hr>
				<p>Pesan broadcast dari akun <strong>%s</strong> (%s) gagal dikirim ke target tujuan.</p>
				<table style="width: 100%%; border-collapse: collapse; margin-top: 15px;">
					<tr>
						<td style="padding: 8px; border-bottom: 1px solid #ddd;"><strong>Target Tujuan:</strong></td>
						<td style="padding: 8px; border-bottom: 1px solid #ddd;">%s</td>
					</tr>
					<tr>
						<td style="padding: 8px; border-bottom: 1px solid #ddd;"><strong>Detail Error:</strong></td>
						<td style="padding: 8px; border-bottom: 1px solid #ddd; color: #d9534f;">%s</td>
					</tr>
				</table>
				<hr>
				<p style="font-size: 12px; color: #888; text-align: center;">Dikirim secara otomatis oleh Sistem Keamanan Bot WhatsApp.</p>
			</div>
		`, userName, userPhone, target, errMsg)

		msg := fmt.Sprintf("From: \"Bot Security\" <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/html; charset=\"utf-8\"\r\n"+
			"\r\n%s", emailUser, emailUser, subject, htmlBody)

		auth := smtp.PlainAuth("", emailUser, emailPass, "smtp.gmail.com")
		err := smtp.SendMail("smtp.gmail.com:587", auth, emailUser, []string{emailUser}, []byte(msg))
		if err != nil {
			log.Println("❌ Gagal mengirim Email Failed Broadcast:", err)
		}
	}
}
