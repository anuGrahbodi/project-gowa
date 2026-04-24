@echo off
REM =============================================
REM Upload file proyek ke Google Cloud VM
REM Jalankan dari folder D:\gowhatsappweb
REM Syarat: gcloud CLI sudah terinstall
REM =============================================

echo =============================================
echo   Upload ke Google Cloud VM
echo =============================================

REM === EDIT BAGIAN INI ===
set INSTANCE_NAME=wa-bot-server
set ZONE=asia-southeast2-a
set REMOTE_DIR=/home/[USERNAME]/gowhatsappweb
REM =======================

echo [1/3] Compress file proyek...
tar --exclude=".git" ^
    --exclude="scratch" ^
    --exclude="*.db" ^
    --exclude="*.log" ^
    --exclude="sessions.json" ^
    -czf wa_project.tar.gz ^
    -C D:\ gowhatsappweb

echo [2/3] Upload ke server...
gcloud compute scp wa_project.tar.gz %INSTANCE_NAME%:~/ --zone=%ZONE%

echo [3/3] Extract di server...
gcloud compute ssh %INSTANCE_NAME% --zone=%ZONE% --command="mkdir -p ~/gowhatsappweb && tar -xzf ~/wa_project.tar.gz -C ~/ && rm ~/wa_project.tar.gz"

del wa_project.tar.gz

echo =============================================
echo Upload selesai! Selanjutnya SSH ke server:
echo gcloud compute ssh %INSTANCE_NAME% --zone=%ZONE%
echo Lalu jalankan: bash ~/gowhatsappweb/deploy/setup_server.sh
echo =============================================
pause
