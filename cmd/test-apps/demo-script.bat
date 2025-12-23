@echo off
echo === 批处理脚本测试 ===
echo Current PID cannot be easily retrieved in BAT, but I am running.
echo Working Directory: %cd%

:loop
echo [%time%] Script is alive...
timeout /t 3 >nul
goto loop