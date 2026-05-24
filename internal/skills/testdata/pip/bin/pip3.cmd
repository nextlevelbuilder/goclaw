@echo off
setlocal enabledelayedexpansion
set FIXTURE_DIR=%~dp0..

if "%~1"=="list" if "%~2"=="--outdated" (
  set HAS_PRE=0
  for %%A in (%*) do (
    if "%%~A"=="--pre" set HAS_PRE=1
  )
  if "!HAS_PRE!"=="1" (
    if exist "%FIXTURE_DIR%\outdated-empty.json" (
      type "%FIXTURE_DIR%\outdated-empty.json"
    ) else (
      echo []
    )
  ) else (
    type "%FIXTURE_DIR%\outdated-23.3.json"
  )
  exit /b 0
)

if "%~1"=="install" (
  if "%FIXTURE_PIP_EXIT%"=="" set FIXTURE_PIP_EXIT=0
  if not "%FIXTURE_PIP_STDERR%"=="" >&2 echo %FIXTURE_PIP_STDERR%
  exit /b %FIXTURE_PIP_EXIT%
)

if "%~1"=="cache" exit /b 0

exit /b 2
