#!/usr/bin/env bash
# credproxyd hook: serve AWS SSO temporary credentials.
# stdin: JSON {action, route, request, context}
# stdout: JSON {body_replace, expires_in_sec}
#
# Returns credentials in AWS_CONTAINER_CREDENTIALS_FULL_URI-compatible format
# (the IMDS/ECS credential provider JSON schema):
#   {"AccessKeyId":...,"SecretAccessKey":...,"Token":...,"Expiration":...}
#
# Requires: aws CLI, jq
set -euo pipefail

# Discard stdin.
cat > /dev/null

# Read context from stdin (already consumed above, so we use env if needed).
# Try aws sts get-caller-identity first; if it works, credentials are active.
SSO_CACHE_DIR="${HOME}/.aws/sso/cache"

# Find the most recent valid SSO token cache file.
token_file=""
for f in "${SSO_CACHE_DIR}"/*.json; do
    [[ -f "${f}" ]] || continue
    exp=$(jq -r '.expiresAt // empty' "${f}" 2>/dev/null)
    [[ -n "${exp}" ]] || continue
    # Skip if expired.
    if date --date="${exp}" +%s > /dev/null 2>&1; then
        exp_ts=$(date --date="${exp}" +%s)
        now_ts=$(date +%s)
        (( exp_ts > now_ts )) && token_file="${f}" && break
    fi
done

if [[ -z "${token_file}" ]]; then
    # Fallback: try aws configure list-profiles and use the default profile.
    creds=$(aws configure export-credentials --format process 2>/dev/null || true)
else
    profile="${AWS_PROFILE:-default}"
    # Use aws sso get-role-credentials via the active token.
    account_id=$(jq -r '.accountId // empty' "${token_file}")
    role_name=$(jq -r '.roleName // empty' "${token_file}")
    region=$(aws configure get region --profile "${profile}" 2>/dev/null || echo "us-east-1")
    access_token=$(jq -r '.accessToken // empty' "${token_file}")

    if [[ -n "${account_id}" && -n "${role_name}" && -n "${access_token}" ]]; then
        role_creds=$(aws sso get-role-credentials \
            --account-id "${account_id}" \
            --role-name "${role_name}" \
            --access-token "${access_token}" \
            --region "${region}" \
            --output json 2>/dev/null)
        creds=$(echo "${role_creds}" | jq -r '.roleCredentials')
    fi
fi

if [[ -z "${creds:-}" ]]; then
    echo "aws-sso-get: failed to obtain credentials" >&2
    exit 1
fi

# Normalize to IMDS container credential format.
access_key=$(echo "${creds}" | jq -r '.accessKeyId // .AccessKeyId // empty')
secret_key=$(echo "${creds}" | jq -r '.secretAccessKey // .SecretAccessKey // empty')
session_token=$(echo "${creds}" | jq -r '.sessionToken // .SessionToken // empty')
expiration=$(echo "${creds}" | jq -r '.expiration // .Expiration // "9999-12-31T23:59:59Z"' 2>/dev/null)

if [[ -z "${access_key}" || -z "${secret_key}" ]]; then
    echo "aws-sso-get: credentials missing required fields" >&2
    exit 1
fi

# expires_in_sec: seconds until credential expiry (best-effort).
expires_in_sec=3600
if exp_ts=$(date --date="${expiration}" +%s 2>/dev/null); then
    expires_in_sec=$(( exp_ts - $(date +%s) ))
    (( expires_in_sec < 0 )) && expires_in_sec=0
fi

jq -n \
    --arg ak "${access_key}" \
    --arg sk "${secret_key}" \
    --arg st "${session_token}" \
    --arg exp "${expiration}" \
    --argjson ttl "${expires_in_sec}" \
    '{
        "body_replace": {
            "AccessKeyId": $ak,
            "SecretAccessKey": $sk,
            "Token": $st,
            "Expiration": $exp
        },
        "expires_in_sec": $ttl
    }'
