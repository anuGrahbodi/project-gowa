@echo off
title WA Bot Server
set GOROOT=D:\go
set GOPATH=D:\gopath
set GOMODCACHE=D:\gopath\pkg\mod
set GOCACHE=D:\gopath\cache
set PATH=D:\go\bin;D:\gopath\bin;%PATH%

echo =======================================================
echo     Menjalankan Web Dashboard WhatsApp Bot (Go Version)
echo =======================================================
echo.

go run .
pause
