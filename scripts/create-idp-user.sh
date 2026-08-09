#!/usr/bin/env bash
# Create (or reset) a Keycloak user that can log into the IDP immediately.
#
# Usage:
#   ./scripts/create-idp-user.sh <username> <password> <admin|developer|viewer> [email]
#
# Example:
#   ./scripts/create-idp-user.sh alice 'AlicePass1' developer alice@example.com

set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "Usage: $0 <username> <password> <admin|developer|viewer> [email]" >&2
  exit 1
fi

USERNAME="$1"
PASSWORD="$2"
ROLE="$3"
EMAIL="${4:-${USERNAME}@idp.local}"

case "${ROLE}" in
  admin|developer|viewer) ;;
  *)
    echo "Role must be admin, developer, or viewer (got: ${ROLE})" >&2
    exit 1
    ;;
esac

KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8080}"
KC_ADMIN="${KC_ADMIN:-admin}"
KC_ADMIN_PASSWORD="${KC_ADMIN_PASSWORD:-admin}"
REALM="${KEYCLOAK_REALM:-idp}"

echo "→ Getting Keycloak master admin token..."
ADMIN_TOKEN="$(
  curl -sf -X POST "${KEYCLOAK_URL}/realms/master/protocol/openid-connect/token" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    -d "grant_type=password&client_id=admin-cli&username=${KC_ADMIN}&password=${KC_ADMIN_PASSWORD}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])'
)"

auth=(-H "Authorization: Bearer ${ADMIN_TOKEN}" -H 'Content-Type: application/json')
ENC_USER="$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "${USERNAME}")"
FIRST="$(python3 -c "import sys; u=sys.argv[1]; print(u.split('@')[0].split('.')[0] or 'User')" "${USERNAME}")"

USER_BODY="$(
  USERNAME="${USERNAME}" EMAIL="${EMAIL}" FIRST="${FIRST}" PASSWORD="${PASSWORD}" python3 - <<'PY'
import json, os
print(json.dumps({
  "username": os.environ["USERNAME"],
  "email": os.environ["EMAIL"],
  "firstName": os.environ["FIRST"],
  "lastName": "User",
  "enabled": True,
  "emailVerified": True,
  "requiredActions": [],
  "credentials": [{
    "type": "password",
    "value": os.environ["PASSWORD"],
    "temporary": False,
  }],
}))
PY
)"

EXISTING="$(
  curl -sf "${KEYCLOAK_URL}/admin/realms/${REALM}/users?username=${ENC_USER}&exact=true" \
    -H "Authorization: Bearer ${ADMIN_TOKEN}"
)"
USER_ID="$(python3 -c 'import json,sys; u=json.load(sys.stdin); print(u[0]["id"] if u else "")' <<<"${EXISTING}")"

if [[ -z "${USER_ID}" ]]; then
  echo "→ Creating user '${USERNAME}' in realm '${REALM}'..."
  curl -sf -X POST "${KEYCLOAK_URL}/admin/realms/${REALM}/users" \
    "${auth[@]}" \
    -d "${USER_BODY}" >/dev/null
  USER_ID="$(
    curl -sf "${KEYCLOAK_URL}/admin/realms/${REALM}/users?username=${ENC_USER}&exact=true" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["id"])'
  )"
else
  echo "→ User exists (${USER_ID}); resetting password and clearing required actions..."
  RESET_BODY="$(
    USERNAME="${USERNAME}" EMAIL="${EMAIL}" FIRST="${FIRST}" python3 - <<'PY'
import json, os
print(json.dumps({
  "username": os.environ["USERNAME"],
  "email": os.environ["EMAIL"],
  "firstName": os.environ["FIRST"],
  "lastName": "User",
  "enabled": True,
  "emailVerified": True,
  "requiredActions": [],
}))
PY
  )"
  curl -sf -X PUT "${KEYCLOAK_URL}/admin/realms/${REALM}/users/${USER_ID}" \
    "${auth[@]}" \
    -d "${RESET_BODY}" >/dev/null
  PASS_BODY="$(
    PASSWORD="${PASSWORD}" python3 - <<'PY'
import json, os
print(json.dumps({
  "type": "password",
  "value": os.environ["PASSWORD"],
  "temporary": False,
}))
PY
  )"
  curl -sf -X PUT "${KEYCLOAK_URL}/admin/realms/${REALM}/users/${USER_ID}/reset-password" \
    "${auth[@]}" \
    -d "${PASS_BODY}" >/dev/null
fi

echo "→ Assigning realm role '${ROLE}'..."
ROLE_JSON="$(curl -sf "${KEYCLOAK_URL}/admin/realms/${REALM}/roles/${ROLE}" -H "Authorization: Bearer ${ADMIN_TOKEN}")"
curl -sf -X POST "${KEYCLOAK_URL}/admin/realms/${REALM}/users/${USER_ID}/role-mappings/realm" \
  "${auth[@]}" \
  -d "[${ROLE_JSON}]" >/dev/null

echo "→ Verifying password grant..."
curl -sf -X POST "${KEYCLOAK_URL}/realms/${REALM}/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d "grant_type=password&client_id=idp-frontend&username=${USERNAME}&password=${PASSWORD}" \
  | python3 -c '
import json, sys
d = json.load(sys.stdin)
if "access_token" not in d:
    raise SystemExit(f"login still failing: {d}")
print("  password grant OK")
'

echo ""
echo "Ready. Log into http://localhost:5173 with:"
echo "  username: ${USERNAME}"
echo "  password: ${PASSWORD}"
echo "  role:     ${ROLE}"
