@echo off
setlocal
set FIXTURE_DIR=%~dp0..

if "%~1"=="outdated" (
  if "%FIXTURE_MODE%"=="" set FIXTURE_MODE=outdated
  if "%FIXTURE_MODE%"=="outdated" (
    type "%FIXTURE_DIR%\outdated-10.json"
    goto exit_outdated
  )
  if "%FIXTURE_MODE%"=="error" (
    >&2 echo npm ERR! code ERESOLVE
    >&2 echo npm ERR! peer dep conflict
    goto exit_outdated
  )
  if "%FIXTURE_MODE%"=="ambiguous" goto exit_outdated
  if "%FIXTURE_MODE%"=="empty" goto exit_ok
  goto exit_bad
)

if "%~1"=="install" (
  if "%FIXTURE_NPM_EXIT%"=="" set FIXTURE_NPM_EXIT=0
  if not "%FIXTURE_NPM_STDERR%"=="" >&2 echo(%FIXTURE_NPM_STDERR%
  exit /b %FIXTURE_NPM_EXIT%
)

if "%~1"=="cache" goto exit_ok

goto exit_bad

:exit_outdated
exit /b 1

:exit_ok
exit /b 0

:exit_bad
exit /b 2
