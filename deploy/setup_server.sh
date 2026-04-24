#!/bin/bash
# =============================================
# Script Auto-Setup Server WhatsApp Bot di GCloud (Ubuntu)
# Jalankan: bash setup_server.sh
# =============================================

set -e

echo "============================================="
echo "  🚀 Setup Server WhatsApp Bot - Google Cloud"
echo "============================================="

# 1. Update sistem
echo "[1/6] Update sistem..."
sudo apt-get update -y && sudo apt-get upgrade -y

# 2. Install dependensi
echo "[2/6] Install dependensi (gcc, sqlite3, screen)..."
sudo apt-get install -y gcc libsqlite3-dev screen curl wget git

# 3. Install Go
echo "[3/6] Install Go 1.22..."
GO_VERSION="1.22.2"
wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
rm go${GO_VERSION}.linux-amd64.tar.gz

# Tambah Go ke PATH secara permanen
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin
echo "Go version: $(go version)"

# 4. Build aplikasi
echo "[4/6] Build aplikasi Go..."
cd ~/gowhatsappweb
go build -o wabot .
echo "Build selesai!"

# 5. Buat systemd service (agar auto-start saat reboot)
echo "[5/6] Membuat systemd service..."
sudo bash -c "cat > /etc/systemd/system/wabot.service << 'EOF'
[Unit]
Description=WhatsApp Bot Dashboard
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=/home/$USER/gowhatsappweb
ExecStart=/home/$USER/gowhatsappweb/wabot
Restart=always
RestartSec=5
Environment=PORT=3000

[Install]
WantedBy=multi-user.target
EOF"

sudo systemctl daemon-reload
sudo systemctl enable wabot
sudo systemctl start wabot

echo ""
echo "============================================="
echo "  ✅ SETUP SELESAI!"
echo "============================================="
echo "  Status bot: sudo systemctl status wabot"
echo "  Lihat log : sudo journalctl -u wabot -f"
echo "  Stop bot  : sudo systemctl stop wabot"
echo "  Restart   : sudo systemctl restart wabot"
echo ""
IP=$(curl -s ifconfig.me 2>/dev/null || echo "cek di GCloud Console")
echo "  🌐 Dashboard: http://${IP}:3000"
echo "============================================="
