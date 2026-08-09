#!/usr/bin/env bash
# Make Keycloak password-grant login work for admin-created users.
#
# Keycloak 26 marks email/firstName/lastName as required on the user profile.
# Users created in the Admin UI without those fields (or with Temporary password
# / UPDATE_PASSWORD) get "Account is not fully set up" from the token endpoint —
# which the IDP login page surfaces as a failed login.
#
# This script:
#   1. Relaxes the idp-realm user profile so those fields are optional
#   2. Clears pending required actions on every idp-realm user
#   3. Fills missing first/last name from the username when empty
#
# Usage:
#   ./scripts/fix-keycloak-login.sh
#   KEYCLOAK_URL=http://localhost:8080 KC_ADMIN=admin KC_ADMIN_PASSWORD=admin ./scripts/fix-keycloak-login.sh

set -euo pipefail

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

echo "→ Relaxing user profile required fields in realm '${REALM}'..."
curl -sf "${KEYCLOAK_URL}/admin/realms/${REALM}/users/profile" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  | python3 -c '
import json, sys
profile = json.load(sys.stdin)
for attr in profile.get("attributes", []):
    if attr.get("name") in ("email", "firstName", "lastName"):
        attr.pop("required", None)
json.dump(profile, sys.stdout)
' \
  | curl -sf -X PUT "${KEYCLOAK_URL}/admin/realms/${REALM}/users/profile" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H 'Content-Type: application/json' \
      --data-binary @-

echo "→ Clearing required actions / filling names on existing users..."
curl -sf "${KEYCLOAK_URL}/admin/realms/${REALM}/users?max=500" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  | KEYCLOAK_URL="${KEYCLOAK_URL}" REALM="${REALM}" ADMIN_TOKEN="${ADMIN_TOKEN}" python3 -c '
import json, os, sys, urllib.request

keycloak_url = os.environ["KEYCLOAK_URL"]
realm = os.environ["REALM"]
token = os.environ["ADMIN_TOKEN"]
users = json.load(sys.stdin)
fixed = 0
for u in users:
    if u.get("serviceAccountClientId"):
        continue
    uid = u["id"]
    username = u.get("username") or "user"
    first = (u.get("firstName") or "").strip() or username.split("@")[0].split(".")[0] or "User"
    last = (u.get("lastName") or "").strip() or "User"
    body = {
        "username": username,
        "email": u.get("email") or f"{username}@idp.local",
        "firstName": first,
        "lastName": last,
        "enabled": True,
        "emailVerified": True,
        "requiredActions": [],
    }
    req = urllib.request.Request(
        f"{keycloak_url}/admin/realms/{realm}/users/{uid}",
        data=json.dumps(body).encode(),
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
        method="PUT",
    )
    with urllib.request.urlopen(req) as resp:
        resp.read()
    fixed += 1
print(f"  updated {fixed} user(s)")
'

echo ""
echo "Done. Users need a permanent password + realm role (admin|developer|viewer)."
echo "Create one with:"
echo "  ./scripts/create-idp-user.sh <username> <password> <admin|developer|viewer>"
