@echo off
echo ========================================
echo Testing HSU Master Health Check Fix
echo ========================================
echo.
echo Starting master with managed worker...
echo Press Ctrl+C to stop when ready
echo.
echo Expected behavior:
echo - Echo server starts successfully
echo - Health checks PASS every 10 seconds  
echo - No false "Process not running" messages
echo - No unnecessary restarts
echo.
pause
mastersrv.exe --config config-managed.yaml 