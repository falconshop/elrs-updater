@echo off
REM ELRS Updater build script. Run in a normal Command Prompt (Go must be installed).
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  set "PATH=%ProgramFiles%\Go\bin;%PATH%"
)

echo [1/2] Building customer app (ELRS-Updater.exe, no console window)...
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ELRS-Updater.exe .
if errorlevel 1 goto :err

echo [2/2] Building test CLI (elrs-updater-cli.exe, shows console)...
go build -trimpath -o elrs-updater-cli.exe .
if errorlevel 1 goto :err

echo.
echo DONE.
echo   ELRS-Updater.exe       -^> give this to customers
echo   elrs-updater-cli.exe   -^> for your own bench testing (elrs-updater-cli.exe firmware.bin)
goto :eof

:err
echo.
echo BUILD FAILED. Make sure Go is installed (https://go.dev/dl/).
exit /b 1
