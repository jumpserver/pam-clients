#!/usr/bin/env bash
set -euo pipefail
: "${JMS_ENDPOINT:?Set JMS_ENDPOINT}"
: "${JMS_APP_ID:?Set JMS_APP_ID}"
: "${JMS_APP_SECRET:?Set JMS_APP_SECRET}"
: "${JMS_INSTANCE_ID:?Set JMS_INSTANCE_ID}"
: "${JMS_ACCOUNT_ID:?Set JMS_ACCOUNT_ID}"
JMS_ORG_ID=${JMS_ORG_ID:-00000000-0000-0000-0000-000000000002}
urlencode() {
  local value=$1 char index
  for ((index=0;index<${#value};index++)); do
    char=${value:index:1}
    case "$char" in [a-zA-Z0-9.~_-]) printf '%s' "$char";; *) printf '%%%02X' "'$char";; esac
  done
}
endpoint=${JMS_ENDPOINT%/}
server_path=${endpoint#*://}
if [[ "$server_path" == */* ]]; then prefix=/${server_path#*/}; else prefix=; fi
query="account_id=$(urlencode "$JMS_ACCOUNT_ID")&instance_id=$(urlencode "$JMS_INSTANCE_ID")"
target="$prefix/api/v1/accounts/credential-client/credential/?$query"
date_header=$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S GMT')
digest="SHA-256=$(printf '' | openssl dgst -sha256 -binary | base64 | tr -d '\n')"
request_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
headers='(request-target) accept date digest x-jms-request-id x-jms-org x-jms-client-version x-jms-protocol-version x-jms-config-schema-version'
signing=$(printf '(request-target): get %s\naccept: application/json\ndate: %s\ndigest: %s\nx-jms-request-id: %s\nx-jms-org: %s\nx-jms-client-version: 1.0.2\nx-jms-protocol-version: 1\nx-jms-config-schema-version: 0' "$target" "$date_header" "$digest" "$request_id" "$JMS_ORG_ID")
signature=$(printf '%s' "$signing" | openssl dgst -sha256 -hmac "$JMS_APP_SECRET" -binary | base64 | tr -d '\n')
curl --fail --silent --show-error "$endpoint/api/v1/accounts/credential-client/credential/?$query" \
 -H 'Accept: application/json' -H "Date: $date_header" -H "Digest: $digest" \
 -H "X-JMS-Request-ID: $request_id" -H "X-JMS-ORG: $JMS_ORG_ID" -H 'X-Source: jms-pam' \
 -H 'X-JMS-Client-Version: 1.0.2' -H 'X-JMS-Protocol-Version: 1' -H 'X-JMS-Config-Schema-Version: 0' \
 -H "Authorization: Signature keyId=\"$JMS_APP_ID\",algorithm=\"hmac-sha256\",headers=\"$headers\",signature=\"$signature\""
