#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="${ROOT_DIR}/.runtime"

BACKEND_ADDR="${A_STOCK_ADDR:-127.0.0.1:20081}"
BACKEND_URL="http://${BACKEND_ADDR}"
BACKEND_PORT="${BACKEND_ADDR##*:}"
FRONTEND_HOST="${A_STOCK_FRONTEND_HOST:-127.0.0.1}"
FRONTEND_PORT="${A_STOCK_FRONTEND_PORT:-20073}"
FRONTEND_URL="http://${FRONTEND_HOST}:${FRONTEND_PORT}"
TOKEN="${A_STOCK_TOKEN:-}"

BACKEND_PID_FILE="${RUNTIME_DIR}/backend.pid"
FRONTEND_PID_FILE="${RUNTIME_DIR}/frontend.pid"
BACKEND_LOG="${RUNTIME_DIR}/backend.log"
FRONTEND_LOG="${RUNTIME_DIR}/frontend.log"
BACKEND_SESSION="easy-stock-backend"
FRONTEND_SESSION="easy-stock-frontend"

log() {
  printf '[easy-stock] %s\n' "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

stop_pid_file() {
  local pid_file="$1"
  local name="$2"
  if [[ ! -f "${pid_file}" ]]; then
    return
  fi

  local pid
  pid="$(cat "${pid_file}" 2>/dev/null || true)"
  if [[ -z "${pid}" ]]; then
    rm -f "${pid_file}"
    return
  fi

  if kill -0 "${pid}" >/dev/null 2>&1; then
    log "stopping ${name} pid=${pid}"
    kill "${pid}" >/dev/null 2>&1 || true
    for _ in {1..20}; do
      if ! kill -0 "${pid}" >/dev/null 2>&1; then
        break
      fi
      sleep 0.2
    done
    if kill -0 "${pid}" >/dev/null 2>&1; then
      log "force stopping ${name} pid=${pid}"
      kill -9 "${pid}" >/dev/null 2>&1 || true
    fi
  fi
  rm -f "${pid_file}"
}

stop_port() {
  local port="$1"
  local name="$2"
  local pids
  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -z "${pids}" ]]; then
    return
  fi
  log "stopping ${name} listener(s) on port ${port}: ${pids}"
  kill ${pids} >/dev/null 2>&1 || true
  sleep 0.5
  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "${pids}" ]]; then
    kill -9 ${pids} >/dev/null 2>&1 || true
  fi
}

stop_screen_session() {
  local session="$1"
  if command -v screen >/dev/null 2>&1; then
    screen -S "${session}" -X quit >/dev/null 2>&1 || true
  fi
}

start_detached() {
  local session="$1"
  local command="$2"
  if command -v screen >/dev/null 2>&1; then
    screen -dmS "${session}" sh -c "${command}"
    return
  fi
  sh -c "nohup ${command} >/dev/null 2>&1 &"
}

wait_for_http() {
  local url="$1"
  local name="$2"
  for _ in {1..60}; do
    if curl --noproxy '*' -fsS "${url}" >/dev/null 2>&1; then
      log "${name} ready: ${url}"
      return
    fi
    sleep 0.5
  done
  printf '%s did not become ready: %s\n' "${name}" "${url}" >&2
  return 1
}

require_cmd go
require_cmd npm
require_cmd curl
require_cmd lsof

mkdir -p "${RUNTIME_DIR}"

cd "${ROOT_DIR}"

if [[ ! -d "${ROOT_DIR}/node_modules" ]]; then
  log "node_modules missing, running npm install --ignore-scripts"
  npm install --ignore-scripts
fi

log "building backend"
npm run build:backend

log "building frontend"
npm run build:frontend

stop_pid_file "${BACKEND_PID_FILE}" "backend"
stop_pid_file "${FRONTEND_PID_FILE}" "frontend"
stop_screen_session "${BACKEND_SESSION}"
stop_screen_session "${FRONTEND_SESSION}"
stop_port "${BACKEND_PORT}" "backend"
stop_port "${FRONTEND_PORT}" "frontend"

log "starting backend on ${BACKEND_ADDR}"
start_detached "${BACKEND_SESSION}" \
  "cd '${ROOT_DIR}' && echo \$\$ > '${BACKEND_PID_FILE}' && A_STOCK_ADDR='${BACKEND_ADDR}' A_STOCK_TOKEN='${TOKEN}' exec '${ROOT_DIR}/desktop/bin/easy-stock-backend' > '${BACKEND_LOG}' 2>&1"

wait_for_http "${BACKEND_URL}/api/health" "backend"

log "starting frontend on ${FRONTEND_URL}"
start_detached "${FRONTEND_SESSION}" \
  "cd '${ROOT_DIR}/frontend' && echo \$\$ > '${FRONTEND_PID_FILE}' && VITE_A_STOCK_BACKEND_URL='${BACKEND_URL}' VITE_A_STOCK_TOKEN='${TOKEN}' exec '${ROOT_DIR}/node_modules/.bin/vite' --host '${FRONTEND_HOST}' --port '${FRONTEND_PORT}' --strictPort > '${FRONTEND_LOG}' 2>&1"

wait_for_http "${FRONTEND_URL}" "frontend"

log "restart complete"
log "backend:  ${BACKEND_URL}"
log "frontend: ${FRONTEND_URL}"
log "logs:     ${RUNTIME_DIR}"
