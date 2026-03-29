#!/usr/bin/env bash
# Generates a minimal kubeconfig for the kube-risk service account.
# Run this after: kubectl apply -f rbac.yaml
#
# Usage:
#   bash get-kubeconfig.sh
#   bash get-kubeconfig.sh > kubeconfig.yaml   # save to file
#
# Then paste the output as the KUBECONFIG secret in your GitHub repo:
#   Settings → Secrets and variables → Actions → New repository secret
#   Name: KUBECONFIG

set -euo pipefail

NAMESPACE="kube-risk"
SA="kube-risk"
SECRET="kube-risk-token"

# Wait for the token to be populated (can take a second)
for i in $(seq 1 10); do
  TOKEN=$(kubectl get secret "$SECRET" -n "$NAMESPACE" \
    -o jsonpath='{.data.token}' 2>/dev/null | base64 -d)
  if [[ -n "$TOKEN" ]]; then break; fi
  sleep 1
done

if [[ -z "$TOKEN" ]]; then
  echo "Error: token not found in secret $SECRET. Did you run 'kubectl apply -f rbac.yaml'?" >&2
  exit 1
fi

CA=$(kubectl get secret "$SECRET" -n "$NAMESPACE" \
  -o jsonpath='{.data.ca\.crt}')

SERVER=$(kubectl config view \
  --minify -o jsonpath='{.clusters[0].cluster.server}')

CLUSTER_NAME=$(kubectl config view \
  --minify -o jsonpath='{.clusters[0].name}')

cat <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA}
    server: ${SERVER}
  name: ${CLUSTER_NAME}
contexts:
- context:
    cluster: ${CLUSTER_NAME}
    user: kube-risk
  name: kube-risk
current-context: kube-risk
users:
- name: kube-risk
  user:
    token: ${TOKEN}
EOF
