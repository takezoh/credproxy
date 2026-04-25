#!/usr/bin/env bash
# credproxyd hook: refresh Anthropic OAuth access token.
# stdin: JSON {action, route, request, context}
# stdout: JSON {headers, expires_in_sec}
#
# Uses the refresh_token in ~/.claude/.credentials.json to obtain a new
# access token and updates the credentials file atomically.
set -euo pipefail

CREDS="${HOME}/.claude/.credentials.json"
CREDS_TMP="${CREDS}.credproxyd.tmp"

if [[ ! -f "${CREDS}" ]]; then
    echo "anthropic-refresh: ${CREDS} not found" >&2
    exit 1
fi

# Discard stdin.
cat > /dev/null

refresh_token=$(jq -r '.claudeAiOauth.refreshToken // empty' "${CREDS}")
if [[ -z "${refresh_token}" ]]; then
    echo "anthropic-refresh: no refresh token found" >&2
    exit 1
fi

# POST to Anthropic OAuth token endpoint.
# Reference: https://github.com/achetronic/claude-oauth-proxy
response=$(curl -fsSL \
    -X POST \
    -H "Content-Type: application/json" \
    -H "anthropic-beta: oauth-2025-04-20" \
    "https://console.anthropic.com/v1/oauth/token" \
    -d "$(jq -n --arg rt "${refresh_token}" \
        '{"grant_type":"refresh_token","refresh_token":$rt}')")

access_token=$(echo "${response}" | jq -r '.access_token // empty')
if [[ -z "${access_token}" ]]; then
    echo "anthropic-refresh: token endpoint returned no access_token" >&2
    echo "${response}" >&2
    exit 1
fi

# expires_in is in seconds; convert to absolute epoch-ms for storage.
expires_in=$(echo "${response}" | jq -r '.expires_in // 3600')
now_ms=$(date +%s%3N)
expires_at=$(( now_ms + expires_in * 1000 ))

# Atomic update: write to temp, rename.
jq --arg at "${access_token}" \
   --argjson ea "${expires_at}" \
   '.claudeAiOauth.accessToken = $at | .claudeAiOauth.expiresAt = $ea' \
   "${CREDS}" > "${CREDS_TMP}"
mv "${CREDS_TMP}" "${CREDS}"

jq -n \
    --arg token "${access_token}" \
    --argjson exp "${expires_in}" \
    '{"headers":{"Authorization":("Bearer " + $token)},"expires_in_sec":$exp}'
