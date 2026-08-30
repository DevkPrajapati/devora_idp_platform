#!/usr/bin/env bash
# End-to-end smoke test against a running backend.
#
# Exercises every read RPC the UI issues on page load and reports the HTTP
# status and wall time for each. Written as a script rather than a one-off
# command so a regression in latency or availability can be re-measured the
# same way later.
#
# Usage: scripts/e2e-smoke.sh [backend-url]
set -uo pipefail

BACKEND="${1:-http://localhost:8090}"
KEYCLOAK="${KEYCLOAK_URL:-http://localhost:8080}"
REALM="${KEYCLOAK_REALM:-idp}"
CLIENT="${KEYCLOAK_CLIENT:-idp-frontend}"
USERNAME="${IDP_USER:-admin}"
PASSWORD="${IDP_PASSWORD:-admin}"

pass=0
fail=0
slow=0
# Anything past this is slower than a user will tolerate on a page load.
SLOW_THRESHOLD_MS=3000

token=$(curl -s -m 20 -X POST \
  "${KEYCLOAK}/realms/${REALM}/protocol/openid-connect/token" \
  -d grant_type=password -d "client_id=${CLIENT}" \
  -d "username=${USERNAME}" -d "password=${PASSWORD}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null)

if [ -z "${token}" ]; then
  echo "FATAL: could not obtain a token from ${KEYCLOAK}/realms/${REALM}"
  exit 1
fi

printf '%-46s %-6s %10s\n' "RPC" "HTTP" "TIME"
printf '%.0s-' {1..64}; echo

# rpc <Service/Method> [json-body]
rpc() {
  local procedure="$1"
  local body="${2:-}"
  [ -n "${body}" ] || body='{}'
  local out status seconds ms

  out=$(curl -s -m 45 -o /tmp/e2e_body -w '%{http_code} %{time_total}' \
    -X POST "${BACKEND}/idp.v1.${procedure}" \
    -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' \
    -H 'Connect-Protocol-Version: 1' \
    -d "${body}")

  status=$(echo "${out}" | cut -d' ' -f1)
  seconds=$(echo "${out}" | cut -d' ' -f2)
  ms=$(python3 -c "print(int(float('${seconds}')*1000))")

  local mark=""
  if [ "${status}" = "200" ]; then
    pass=$((pass + 1))
    if [ "${ms}" -gt "${SLOW_THRESHOLD_MS}" ]; then
      mark="  <-- SLOW"
      slow=$((slow + 1))
    fi
  else
    fail=$((fail + 1))
    mark="  <-- $(head -c 160 /tmp/e2e_body)"
  fi
  printf '%-46s %-6s %8sms%s\n' "${procedure}" "${status}" "${ms}" "${mark}"
}

# Several RPCs are scoped to a project or an IDP-managed namespace, so they
# cannot be probed with a placeholder. Discover real ones first and skip those
# checks when the platform has none yet rather than reporting a false failure.
json_field() {
  python3 -c "
import json,sys
try:
    data = json.load(open('/tmp/e2e_body'))
except Exception:
    sys.exit(0)
items = data.get('$1') or []
print(items[0].get('$2', '') if items else '')
" 2>/dev/null
}

curl -s -m 20 -o /tmp/e2e_body -X POST "${BACKEND}/idp.v1.ProjectService/ListProjects" \
  -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
  -H 'Connect-Protocol-Version: 1' -d '{"page":{"page":1,"pageSize":1}}' >/dev/null
PROJECT_SLUG=$(json_field projects slug)

curl -s -m 20 -o /tmp/e2e_body -X POST "${BACKEND}/idp.v1.NamespaceService/ListNamespaces" \
  -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
  -H 'Connect-Protocol-Version: 1' -d '{"page":{"page":1,"pageSize":1}}' >/dev/null
IDP_NAMESPACE=$(json_field namespaces name)

rpc HealthService/Check
rpc ClusterService/GetOverview
rpc ClusterService/ListNodes
rpc ClusterService/ListPods '{"namespace":""}'
rpc ClusterService/ListServices '{"namespace":""}'
rpc ClusterService/ListEvents '{"namespace":"","limit":25}'
rpc ClusterService/GetResourceMetrics
rpc ClusterService/ListClusters
rpc ClusterService/ListClusterNamespaces
rpc ClusterService/GetNamespaceResources '{"name":"kube-system"}'
rpc NamespaceService/ListNamespaces '{"page":{"page":1,"pageSize":20}}'
rpc ProjectService/ListProjects '{"page":{"page":1,"pageSize":20}}'
rpc StorageService/GetStorageOverview
rpc StorageService/ListPersistentVolumeClaims '{"namespace":""}'
rpc StorageService/ListStorageClasses
rpc AuditService/ListAuditLogs '{"page":{"page":1,"pageSize":20}}'
rpc DatabaseService/ListDatabases

if [ -n "${IDP_NAMESPACE}" ]; then
  rpc DeploymentService/ListDeployments "{\"namespace\":\"${IDP_NAMESPACE}\"}"
else
  echo "skipped DeploymentService/ListDeployments (no IDP namespace registered)"
fi

if [ -n "${PROJECT_SLUG}" ]; then
  rpc RegistryService/ListRegistryCredentials "{\"projectSlug\":\"${PROJECT_SLUG}\"}"
  rpc BuildService/ListGitRepositories "{\"projectSlug\":\"${PROJECT_SLUG}\"}"
else
  echo "skipped registry/build checks (no project registered)"
fi

printf '%.0s-' {1..64}; echo
echo "passed=${pass} failed=${fail} slow(>${SLOW_THRESHOLD_MS}ms)=${slow}"
[ "${fail}" -eq 0 ] || exit 1
