#!/usr/bin/env bash
# Map Ingress hostnames to 127.0.0.1 for minikube tunnel (Docker driver on macOS).
set -euo pipefail

HOSTS=(
  user-web.user-mgmt.idp.local
  user-api.user-mgmt.idp.local
  mongodb.user-mgmt.idp.local
)

for h in "${HOSTS[@]}"; do
  if grep -qE "[[:space:]]${h}$" /etc/hosts; then
    echo "ok  $h"
  else
    echo "127.0.0.1  ${h}" | sudo tee -a /etc/hosts >/dev/null
    echo "add $h"
  fi
done

echo ""
echo "Option A (port 80 — needs Mac password):"
echo "  minikube tunnel"
echo "  then open http://user-web.user-mgmt.idp.local/"
echo ""
echo "Option B (no sudo — keep this running; 8080 is often Keycloak):"
echo "  kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller 18080:80"
echo "  then open http://user-web.user-mgmt.idp.local:18080/"
echo ""
echo "Easiest from IDP UI: Deployments → Open App (localhost, no DNS needed)."
