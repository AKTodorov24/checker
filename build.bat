@echo off
setlocal
title Lightning Checker - build
cd /d "%~dp0"

set "ICON=icon.ico"
if not "%~1"=="" set "ICON=%~1"
set "OUT=LightningChecker.exe"

where go >nul 2>nul
if errorlevel 1 goto nogo

for /f "delims=" %%i in ('go env GOPATH') do set "GOBIN_DIR=%%i\bin"
set "RSRC=%GOBIN_DIR%\rsrc.exe"

if not exist "%ICON%" goto noicon
if exist "%RSRC%" goto makeicon
echo [i] Installing rsrc (icon tool), one time only...
go install github.com/akavel/rsrc@latest
if errorlevel 1 goto noicon

:makeicon
echo [i] Embedding %ICON%
"%RSRC%" -ico "%ICON%" -arch amd64 -o rsrc_windows_amd64.syso
if errorlevel 1 goto noicon
goto build

:noicon
echo [!] No icon embedded. Put icon.ico next to build.bat, or run: build.bat path\to\file.ico

:build
echo [i] Building %OUT%
set "GOOS=windows"
set "GOARCH=amd64"
set "CGO_ENABLED=0"
go vet .
if errorlevel 1 goto failed
go build -trimpath -ldflags "-s -w" -o "%OUT%" .
if errorlevel 1 goto failed
if exist rsrc_windows_amd64.syso del /q rsrc_windows_amd64.syso
echo.
echo [+] Done: %CD%\%OUT%
pause
exit /b 0

:nogo
echo [x] Go is not installed. Get it from https://go.dev/dl/ then run build.bat again.
pause
exit /b 1

:failed
if exist rsrc_windows_amd64.syso del /q rsrc_windows_amd64.syso
echo [x] Build failed, see the errors above.
pause
exit /b 1
