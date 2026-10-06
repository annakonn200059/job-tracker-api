#!/usr/bin/env bash
set -euo pipefail

# Smoke test: exercises the API end to end against a running instance.
#   ./scripts/smoke.sh [base-url]
#
# Registers two throwaway users per run (there is no endpoint to delete a
# user, so their rows stay behind; vacancies are cleaned up). Needs curl and
# jq.

readonly BASE_URL="${1:-${API_BASE_URL:-http://localhost:8080}}"
readonly RUN_ID="$(date +%s)-${RANDOM}"
readonly PASSWORD="smoke-test-password"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

# Collects ids to delete, so a failure halfway doesn't leave rows behind.
created_ids=()
token_a=""

cleanup() {
    [[ -n "${token_a}" ]] || return 0
    local id
    for id in "${created_ids[@]}"; do
        curl -s -o /dev/null -X DELETE \
            -H "Authorization: Bearer ${token_a}" \
            "${BASE_URL}/vacancies/${id}" || true
    done
}
trap cleanup EXIT

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "  ok: $*"
}

# request METHOD PATH TOKEN [BODY] — prints the body, then the status code
# on its own last line.
request() {
    local method="$1" path="$2" token="$3" body="${4:-}"
    local args=(-s -w '\n%{http_code}' -X "${method}")
    [[ -n "${token}" ]] && args+=(-H "Authorization: Bearer ${token}")
    [[ -n "${body}" ]] && args+=(-H 'Content-Type: application/json' -d "${body}")
    curl "${args[@]}" "${BASE_URL}${path}"
}

# status METHOD PATH TOKEN [BODY]
status() {
    request "$@" | tail -n 1
}

# register LABEL — prints the new user's session token.
register() {
    local email="smoke-${RUN_ID}-$1@example.com" response code
    response=$(request POST /auth/register "" \
        "{\"email\":\"${email}\",\"password\":\"${PASSWORD}\"}")
    code=$(tail -n 1 <<<"${response}")
    [[ "${code}" == "201" ]] || fail "register ${email}: got ${code}: $(sed '$d' <<<"${response}")"
    sed '$d' <<<"${response}" | jq -r '.token'
}

echo "Smoke test against ${BASE_URL}"

echo "health"
[[ "$(status GET /healthz "")" == "200" ]] || fail "healthz not 200"
pass "liveness"
[[ "$(status GET /readyz "")" == "200" ]] || fail "readyz not 200"
pass "readiness"

echo "auth"
token_a=$(register a)
token_b=$(register b)
pass "registered two users"
[[ "$(status GET /auth/me "${token_a}")" == "200" ]] || fail "/auth/me with a session not 200"
code=$(status GET /vacancies "")
[[ "${code}" == "401" ]] || fail "GET /vacancies without a session: got ${code}, expected 401"
pass "no session gets 401"

echo "create"
response=$(request POST /vacancies "${token_a}" \
    '{"title":"Smoke Test Engineer","company_name":"SmokeCorp","location":"Berlin","description":"keep me"}')
code=$(tail -n 1 <<<"${response}")
body=$(sed '$d' <<<"${response}")
[[ "${code}" == "201" ]] || fail "create: got ${code}: ${body}"
vacancy_id=$(jq -r '.id' <<<"${body}")
[[ "${vacancy_id}" != "null" && -n "${vacancy_id}" ]] || fail "no id in response: ${body}"
created_ids+=("${vacancy_id}")
pass "created vacancy ${vacancy_id}"

echo "read"
[[ "$(status GET "/vacancies/${vacancy_id}" "${token_a}")" == "200" ]] \
    || fail "owner cannot read own vacancy"
pass "owner reads it"

echo "isolation"
code=$(status GET "/vacancies/${vacancy_id}" "${token_b}")
[[ "${code}" == "404" ]] || fail "user b got ${code} for user a's vacancy — expected 404"
code=$(status PATCH "/vacancies/${vacancy_id}" "${token_b}" '{"title":"hijacked"}')
[[ "${code}" == "404" ]] || fail "user b got ${code} patching user a's vacancy — expected 404"
pass "other user gets 404"

echo "partial update"
response=$(request PATCH "/vacancies/${vacancy_id}" "${token_a}" \
    '{"title":"Senior Smoke Test Engineer","location":null}')
code=$(tail -n 1 <<<"${response}")
body=$(sed '$d' <<<"${response}")
[[ "${code}" == "200" ]] || fail "patch: got ${code}: ${body}"
[[ "$(jq -r '.title' <<<"${body}")" == "Senior Smoke Test Engineer" ]] || fail "title not updated: ${body}"
[[ "$(jq -r '.location' <<<"${body}")" == "null" ]] || fail "location not cleared: ${body}"
[[ "$(jq -r '.description' <<<"${body}")" == "keep me" ]] || fail "description lost: ${body}"
[[ "$(jq -r '.company_id' <<<"${body}")" != "null" ]] || fail "company_id lost: ${body}"
pass "set, cleared and kept fields as sent"

echo "validation"
for body in \
    '{"company_name":"NoTitle"}' \
    '{"title":"X","salary_min":200000,"salary_max":100000}' \
    '{"title":"X","work_mode":"underwater"}'
do
    code=$(status POST /vacancies "${token_a}" "${body}")
    [[ "${code}" == "400" ]] || fail "expected 400 for ${body}, got ${code}"
done
code=$(status PATCH "/vacancies/${vacancy_id}" "${token_a}" '{"title":null}')
[[ "${code}" == "400" ]] || fail "expected 400 for null title, got ${code}"
pass "invalid payloads rejected"

echo
echo "All checks passed."
