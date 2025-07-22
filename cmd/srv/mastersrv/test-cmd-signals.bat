@echo off
echo =========================================
echo Testing Windows Signal Handling - CMD.exe
echo =========================================
echo.
echo This test uses the simplified Windows process creation:
echo - Only CREATE_NEW_PROCESS_GROUP (no DETACHED_PROCESS)
echo - Should preserve master's signal handling in cmd.exe
echo - Child processes can still be terminated gracefully
echo.
echo TEST PROCEDURE:
echo 1. Master starts with failing worker (no --port arg)
echo 2. Worker fails and restarts 2 times (circuit breaker)
echo 3. After circuit breaker opens, try Ctrl+C
echo 4. Should see "Received signal: interrupt" message
echo.
echo Expected behavior:
echo ✅ Circuit breaker stops after 2 restart attempts
echo ✅ Ctrl+C works IMMEDIATELY at any time
echo ✅ Shows "Received signal: interrupt" message
echo ✅ Clean shutdown of master and any running processes
echo.
pause
echo.
echo Starting test in cmd.exe environment...
mastersrv.exe --config test-circuit-breaker.yaml 