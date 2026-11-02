@echo off
setlocal
cd /d "%~dp0\.."
if not exist "bin\gpt-robot.exe" (
  echo Missing bin\gpt-robot.exe. Download and extract the Windows release ZIP first.
  pause
  exit /b 1
)
"bin\gpt-robot.exe" serve --root "%CD%" --open
set "robot_exit=%ERRORLEVEL%"
echo.
echo GPT Robot has stopped. Press any key to close.
pause >nul
exit /b %robot_exit%
