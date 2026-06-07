@echo off
:: ─────────────────────────────────────────────
::  DistKV — Git Push Script (Windows)
:: ─────────────────────────────────────────────

setlocal EnableDelayedExpansion

:: ── Config ────────────────────────────────────
set BRANCH=%1
if "%BRANCH%"=="" set BRANCH=main

set COMMIT_MSG=%2
if "%COMMIT_MSG%"=="" (
  for /f "tokens=1-2 delims= " %%a in ('powershell -command "Get-Date -Format 'yyyy-MM-dd HH:mm:ss'"') do (
    set COMMIT_MSG=update: %%a %%b
  )
)

echo.
echo ^>^> DistKV Git Push
echo    Branch  : %BRANCH%
echo    Message : %COMMIT_MSG%
echo.

:: ── Safety check ──────────────────────────────
if not exist ".git" (
  echo [ERROR] Not a git repository. Run: git init
  exit /b 1
)

:: ── Stage all changes ─────────────────────────
echo [1/4] Staging changes...
git add .
git status --short

:: ── Commit ────────────────────────────────────
echo.
echo [2/4] Committing...
git diff --cached --quiet
if %errorlevel%==0 (
  echo   Nothing to commit, working tree clean.
) else (
  git commit -m "%COMMIT_MSG%"
  echo   [OK] Committed
)

:: ── Ensure branch exists ──────────────────────
echo.
echo [3/4] Checking branch '%BRANCH%'...
git show-ref --verify --quiet refs/heads/%BRANCH%
if %errorlevel%==0 (
  git checkout %BRANCH% 2>nul
) else (
  git checkout -b %BRANCH%
  echo   [OK] Created branch '%BRANCH%'
)

:: ── Push ──────────────────────────────────────
echo.
echo [4/4] Pushing to origin/%BRANCH%...
git push -u origin %BRANCH%
if %errorlevel%==0 (
  echo.
  echo [OK] Successfully pushed to origin/%BRANCH%
) else (
  echo.
  echo [ERROR] Push failed. Check your remote and credentials.
  exit /b 1
)

endlocal