#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
NAMESPACE="${NAMESPACE:-e2e-$(date +%s)}"
NAMESPACE_CREATED=""
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURES_DIR="${SCRIPT_DIR}/fixtures"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASS=0
FAIL=0

cleanup() {
  if [ -n "$NAMESPACE_CREATED" ]; then
    echo -e "\n${YELLOW}▶ Cleanup${NC}"
    kubectl delete namespace "$NAMESPACE" --wait=false 2>/dev/null || true
    echo "  namespace ${NAMESPACE} scheduled for deletion"
  fi
}
trap cleanup EXIT

if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
  kubectl create namespace "$NAMESPACE"
  NAMESPACE_CREATED="true"
  echo "namespace ${NAMESPACE} created"
else
  echo "namespace ${NAMESPACE} already exists"
fi

pass() { echo -e "  ${GREEN}PASS${NC}  $1"; PASS=$((PASS + 1)); }
fail() { echo -e "  ${RED}FAIL${NC}  $1"; FAIL=$((FAIL + 1)); }
section() { echo -e "\n${YELLOW}▶ $1${NC}"; }

# Sends a request and returns the HTTP status code; body written to /tmp/bs_body.
# Usage: do_request <method> <path> [body]
do_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  if [ -n "$body" ]; then
    curl -s -o /tmp/bs_body -w "%{http_code}" \
      -X "$method" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "${BASE_URL}${path}" || echo "000"
  else
    curl -s -o /tmp/bs_body -w "%{http_code}" \
      -X "$method" \
      "${BASE_URL}${path}" || echo "000"
  fi
}

check() {
  local desc="$1"
  local expected_status="$2"
  local actual_status="$3"
  local extra="${4:-}"

  if [ "$actual_status" -eq "$expected_status" ]; then
    pass "$desc (HTTP $actual_status)${extra:+ — $extra}"
  else
    fail "$desc: expected HTTP $expected_status, got $actual_status — $(cat /tmp/bs_body)"
  fi
}

check_json_field() {
  local desc="$1"
  local field="$2"
  local expected="$3"
  local actual
  actual=$(jq -r "$field" /tmp/bs_body 2>/dev/null || echo "")

  if [ "$actual" = "$expected" ]; then
    pass "$desc (${field}=${actual})"
  else
    fail "$desc: expected ${field}=${expected}, got ${field}=${actual} — body: $(cat /tmp/bs_body)"
  fi
}

check_not_null() {
  local desc="$1"
  local field="$2"
  local actual
  actual=$(jq -r "$field" /tmp/bs_body 2>/dev/null || echo "null")

  if [ "$actual" != "null" ] && [ -n "$actual" ]; then
    pass "$desc (${field}=${actual})"
  else
    fail "$desc: ${field} is null or empty — body: $(cat /tmp/bs_body)"
  fi
}

check_array_length() {
  local desc="$1"
  local field="$2"
  local expected="$3"
  local actual
  actual=$(jq "${field} | length" /tmp/bs_body 2>/dev/null || echo "-1")

  if [ "$actual" -eq "$expected" ]; then
    pass "$desc (length=$actual)"
  else
    fail "$desc: expected length=$expected, got length=$actual — body: $(cat /tmp/bs_body)"
  fi
}

# Waits up to <timeout> seconds for at least one SSE data line to appear in <file>.
wait_for_sse_event() {
  local file="$1"
  local timeout="${2:-5}"
  for i in $(seq 1 "$timeout"); do
    grep -q '^data:' "$file" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

# Checks that the first SSE event in <file> has the expected value at <jq-field>.
# SSE lines look like:  data: <json>
check_sse_event() {
  local desc="$1"
  local file="$2"
  local field="$3"
  local expected="$4"
  local actual
  actual=$(grep '^data:' "$file" 2>/dev/null \
    | sed 's/^data: //' \
    | jq -r "$field" 2>/dev/null \
    | head -1 || echo "")

  if [ "$actual" = "$expected" ]; then
    pass "$desc (${field}=${actual})"
  else
    fail "$desc: expected ${field}=${expected}, got ${field}=${actual}"
  fi
}

# ---------------------------------------------------------------------------

section "Health"

status=$(do_request GET /healthz)
check "GET /healthz" 200 "$status"

# ---------------------------------------------------------------------------

section "BrowserConfig — list empty"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browserconfigs")
check "GET /browserconfigs (empty)" 200 "$status"
check_array_length "response is empty array, not null" "." 0

# ---------------------------------------------------------------------------

section "BrowserConfig — events stream"

> /tmp/bs_cfg_sse_events
curl -sN --max-time 10 \
  -H "Accept: text/event-stream" \
  "${BASE_URL}/api/v1/namespaces/${NAMESPACE}/browserconfigs/events" \
  > /tmp/bs_cfg_sse_events &
SSE_CFG_PID=$!
sleep 0.3  # let subscription register

# ---------------------------------------------------------------------------

section "BrowserConfig — create"

status=$(do_request POST "/api/v1/namespaces/${NAMESPACE}/browserconfigs" "$(cat "${FIXTURES_DIR}/browserconfig.json")")
check "POST /browserconfigs" 200 "$status"
check_not_null "browserconfig has name" ".metadata.name"

CONFIG_NAME=$(jq -r '.metadata.name' /tmp/bs_body)

# ---------------------------------------------------------------------------

section "BrowserConfig — events stream — receive event"

if wait_for_sse_event /tmp/bs_cfg_sse_events 5; then
  pass "GET /browserconfigs/events received event"
  check_sse_event "event has correct name"      /tmp/bs_cfg_sse_events ".browserConfig.metadata.name" "$CONFIG_NAME"
  check_sse_event "event type is ADDED"         /tmp/bs_cfg_sse_events ".eventType"                   "ADDED"
else
  fail "GET /browserconfigs/events: no event received within 5s"
  fail "event has correct name (skipped)"
  fail "event type is ADDED (skipped)"
fi

kill "$SSE_CFG_PID" 2>/dev/null || true
wait "$SSE_CFG_PID" 2>/dev/null || true

# ---------------------------------------------------------------------------

section "BrowserConfig — get"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browserconfigs/${CONFIG_NAME}")
check "GET /browserconfigs/${CONFIG_NAME}" 200 "$status"
check_json_field "get returns correct name" ".metadata.name" "$CONFIG_NAME"

# ---------------------------------------------------------------------------

section "BrowserConfig — list with one item"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browserconfigs")
check "GET /browserconfigs (one item)" 200 "$status"
check_array_length "response contains 1 config" "." 1

# ---------------------------------------------------------------------------

section "Browser — list empty"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browsers")
check "GET /browsers (empty)" 200 "$status"
check_array_length "response is empty array, not null" "." 0

# ---------------------------------------------------------------------------

# Generate UUID now so the name-filtered SSE stream can be started before create.
BROWSER_UUID=$(uuidgen 2>/dev/null | tr '[:upper:]' '[:lower:]' || python3 -c "import uuid; print(uuid.uuid4())")
BROWSER_BODY=$(jq --arg name "$BROWSER_UUID" '.metadata.name = $name' "${FIXTURES_DIR}/browser.json")

section "Browser — events stream"

> /tmp/bs_browser_sse_events
> /tmp/bs_browser_sse_filtered
curl -sN --max-time 10 \
  -H "Accept: text/event-stream" \
  "${BASE_URL}/api/v1/namespaces/${NAMESPACE}/browsers/events" \
  > /tmp/bs_browser_sse_events &
SSE_BROWSER_PID=$!

curl -sN --max-time 10 \
  -H "Accept: text/event-stream" \
  "${BASE_URL}/api/v1/namespaces/${NAMESPACE}/browsers/events?name=${BROWSER_UUID}" \
  > /tmp/bs_browser_sse_filtered &
SSE_BROWSER_FILTER_PID=$!

sleep 0.3  # let subscriptions register

# ---------------------------------------------------------------------------

section "Browser — create"

status=$(do_request POST "/api/v1/namespaces/${NAMESPACE}/browsers" "$BROWSER_BODY")
check "POST /browsers" 200 "$status"
check_not_null "browser has name" ".metadata.name"
check_json_field "browserName matches" ".spec.browserName" "chrome"
check_json_field "browserVersion matches" ".spec.browserVersion" "146.0"

BROWSER_NAME=$(jq -r '.metadata.name' /tmp/bs_body)

# ---------------------------------------------------------------------------

section "Browser — events stream — receive event"

if wait_for_sse_event /tmp/bs_browser_sse_events 5; then
  pass "GET /browsers/events received event"
  check_sse_event "event has correct name"  /tmp/bs_browser_sse_events ".browser.metadata.name" "$BROWSER_NAME"
  check_sse_event "event type is ADDED"     /tmp/bs_browser_sse_events ".eventType"             "ADDED"
else
  fail "GET /browsers/events: no event received within 5s"
  fail "event has correct name (skipped)"
  fail "event type is ADDED (skipped)"
fi

if wait_for_sse_event /tmp/bs_browser_sse_filtered 5; then
  pass "GET /browsers/events?name=${BROWSER_NAME} received event"
  check_sse_event "filtered event has correct name" /tmp/bs_browser_sse_filtered ".browser.metadata.name" "$BROWSER_NAME"
else
  fail "GET /browsers/events?name=${BROWSER_NAME}: no event received within 5s"
  fail "filtered event has correct name (skipped)"
fi

kill "$SSE_BROWSER_PID"        2>/dev/null || true
kill "$SSE_BROWSER_FILTER_PID" 2>/dev/null || true
wait "$SSE_BROWSER_PID"        2>/dev/null || true
wait "$SSE_BROWSER_FILTER_PID" 2>/dev/null || true

# ---------------------------------------------------------------------------

section "Browser — get"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browsers/${BROWSER_NAME}")
check "GET /browsers/${BROWSER_NAME}" 200 "$status"
check_json_field "get returns correct name" ".metadata.name" "$BROWSER_NAME"
check_json_field "get returns correct browserName" ".spec.browserName" "chrome"

# ---------------------------------------------------------------------------

section "Browser — list with one item"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browsers")
check "GET /browsers (one item)" 200 "$status"
check_array_length "response contains 1 browser" "." 1

# ---------------------------------------------------------------------------

section "Browser — delete"

status=$(do_request DELETE "/api/v1/namespaces/${NAMESPACE}/browsers/${BROWSER_NAME}")
check "DELETE /browsers/${BROWSER_NAME}" 204 "$status"

# ---------------------------------------------------------------------------

section "Browser — list empty after delete"

# Browser has a finalizer; wait for the controller to remove it (up to 30s).
for i in $(seq 1 30); do
  status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browsers")
  count=$(jq '. | length' /tmp/bs_body 2>/dev/null || echo "-1")
  [ "$count" -eq 0 ] && break
  sleep 1
done
check "GET /browsers (empty after delete)" 200 "$status"
check_array_length "response is empty array after delete" "." 0

# ---------------------------------------------------------------------------

section "BrowserConfig — delete"

status=$(do_request DELETE "/api/v1/namespaces/${NAMESPACE}/browserconfigs/${CONFIG_NAME}")
check "DELETE /browserconfigs/${CONFIG_NAME}" 204 "$status"

# ---------------------------------------------------------------------------

section "BrowserConfig — list empty after delete"

status=$(do_request GET "/api/v1/namespaces/${NAMESPACE}/browserconfigs")
check "GET /browserconfigs (empty after delete)" 200 "$status"
check_array_length "response is empty array after delete" "." 0

# ---------------------------------------------------------------------------

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "  ${GREEN}PASS${NC} ${PASS}   ${RED}FAIL${NC} ${FAIL}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[ "$FAIL" -eq 0 ]
