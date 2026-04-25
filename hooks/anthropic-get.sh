#!/usr/bin/env bash
# credproxyd hook: get Anthropic OAuth bearer token.
# stdin: JSON {action, route, request, context}
# stdout: JSON {headers, expires_in_sec}
#
# Reads ~/.claude/.credentials.json, triggers refresh if access token is near expiry.
set -euo pipefail

CREDS="${HOME}/.claude/.credentials.json"

if [[ ! -f "${CREDS}" ]]; then
    echo '{}' >&2
    echo "anthropic-get: ${CREDS} not found" >&2
    exit 1
fi

# Discard stdin (not used for get).
cat > /dev/null

access_token=$(jq -r '.claudeAiOauth.accessToken // empty' "${CREDS}")
expires_at=$(jq -r '.claudeAiOauth.expiresAt // 0' "${CREDS}")

if [[ -z "${access_token}" ]]; then
    echo "anthropic-get: no access token found" >&2
    exit 1
fi

# Refresh if token expires within 60 seconds.
now_ms=$(date +%s%3N)
if (( expires_at > 0 && expires_at - now_ms < 60000 )); then
    HOOKS_DIR="$(dirname "$0")"
    access_token=$(bash "${HOOKS_DIR}/anthropic-refresh.sh" | jq -r '.headers.Authorization // empty' | sed 's/Bearer //')
    if [[ -z "${access_token}" ]]; then
        echo "anthropic-get: refresh failed" >&2
        exit 1
    fi
    expires_at=$(jq -r '.claudeAiOauth.expiresAt // 0' "${CREDS}")
fi

# expires_in_sec: seconds until token expires (used for cache TTL).
expires_in_sec=0
if (( expires_at > 0 )); then
    expires_in_sec=$(( (expires_at - now_ms) / 1000 ))
fi

jq -n \
    --arg token "${access_token}" \
    --argjson exp "${expires_in_sec}" \
    '{"headers":{"Authorization":("Bearer " + $token)},"expires_in_sec":$exp}'
