#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELM_CHART_DIR="${SCRIPT_DIR}/../helm/microservices-demo-app"

# Source configuration
set -a
# shellcheck source=config.env
source "${SCRIPT_DIR}/config.env"
set +a

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[$(date +%H:%M:%S)]${NC} $*"; }
warn() { echo -e "${YELLOW}[$(date +%H:%M:%S)]${NC} $*"; }
err()  { echo -e "${RED}[$(date +%H:%M:%S)]${NC} $*" >&2; }

# Resolve PROXY_CONTROLLER → per-controller artefacts. Drives:
#   - which App CR + values ConfigMap are kept in the rendered manifests
#   - which Helm values toggle is set on the demo-app HelmRelease
#   - which k6 scenario runs alongside envoy_simulation
case "${PROXY_CONTROLLER:-}" in
  nginx)
    INGRESS_NGINX_ENABLED=true
    KONG_ENABLED=false
    REVERSE_PROXY_APP_NAME="${WC}-ingress-nginx"
    REVERSE_PROXY_HOST="nginx-onlineboutique"
    EXCLUDE_NAME_PATTERN="kong"
    ;;
  kong)
    INGRESS_NGINX_ENABLED=false
    KONG_ENABLED=true
    REVERSE_PROXY_APP_NAME="${WC}-kong-app"
    REVERSE_PROXY_HOST="kong-onlineboutique"
    EXCLUDE_NAME_PATTERN="ingress-nginx"
    ;;
  *)
    err "PROXY_CONTROLLER must be 'nginx' or 'kong' (got: '${PROXY_CONTROLLER:-}'). Set it in config.env."
    exit 1
    ;;
esac
export INGRESS_NGINX_ENABLED KONG_ENABLED

# Shorthand kubectl wrappers per cluster
kmc()  { kubectl --context="${MC_CONTEXT}" "$@"; }
kk6()  { kubectl --context="${K6_CONTEXT}" "$@"; }

usage() {
  cat <<EOF
Envoy Gateway Load Testing — Orchestration Script

Usage: $(basename "$0") <command>

Commands:
  all           Run the full pipeline: deploy WC, wait, deploy app, run k6
  wc            Deploy the workload cluster and additional apps on the MC
  app           Deploy the microservices-demo Helm chart into the WC
  k6            Deploy and start the k6 load test on the k6 cluster
  preview       Render all Kustomize manifests to stdout (dry-run)
  teardown      Delete the workload cluster (prompts for confirmation)
  status        Show current state of WC, apps, and test runs

Clusters (from config.env):
  MC_CONTEXT=${MC_CONTEXT}    (management cluster — WC App CRs)
  K6_CONTEXT=${K6_CONTEXT}    (k6 cluster — TestRun + scenario)

Infrastructure:
  WC=${WC}  MC=${MC}  BASE_DOMAIN=${BASE_DOMAIN}  AZ=${AZ}  RELEASE=${RELEASE}
EOF
}

render_manifests() {
  local tmpdir
  tmpdir="$(mktemp -d)"

  # Copy kustomize tree to temp directory
  cp -a "${SCRIPT_DIR}/." "${tmpdir}/"

  # Substitute env vars in values templates (these contain ${VAR} refs
  # that kustomize replacements cannot reach inside opaque YAML strings)
  envsubst '${WC} ${AZ} ${RELEASE} ${MAX_NODES} ${MIN_NODES} ${INSTANCE_TYPE}' \
    < "${SCRIPT_DIR}/wc-deployment/values/cluster-userconfig.yaml" \
    > "${tmpdir}/wc-deployment/values/cluster-userconfig.yaml"
  envsubst '${WC} ${BASE_DOMAIN}' \
    < "${SCRIPT_DIR}/wc-deployment/values/gateway-api-bundle.yaml" \
    > "${tmpdir}/wc-deployment/values/gateway-api-bundle.yaml"
  envsubst '${INGRESS_NGINX_ENABLED} ${KONG_ENABLED} ${WC} ${BASE_DOMAIN} ${PUBLIC_ENDPOINTS} ${HPA_MIN_REPLICAS} ${HPA_MAX_REPLICAS}' \
    < "${SCRIPT_DIR}/wc-deployment/values/microservices-demo.yaml" \
    > "${tmpdir}/wc-deployment/values/microservices-demo.yaml"

  kubectl kustomize "${tmpdir}"
  rm -rf "${tmpdir}"
}

render_mc_manifests() {
  render_manifests \
    | yq 'select(.metadata.namespace == "org-giantswarm")' \
    | yq 'select(.metadata.name | test("'"${EXCLUDE_NAME_PATTERN}"'") | not)'
}

# Unfiltered MC manifests — used by teardown so we always clean up both ingress
# controllers regardless of the current PROXY_CONTROLLER setting (otherwise
# switching the var between deploy and teardown would orphan the unused App).
render_mc_manifests_all() {
  render_manifests | yq 'select(.metadata.namespace == "org-giantswarm")'
}

render_k6_manifests() {
  render_manifests | yq 'select(.metadata.namespace == "'"${K6_NAMESPACE}"'")'
}

# ------------------------------------------------------------------
# Commands
# ------------------------------------------------------------------

cmd_preview() {
  render_manifests
}

cmd_wc() {
  log "Deploying WC=${WC} on MC=${MC} (context=${MC_CONTEXT})"
  log "  AZ=${AZ}  RELEASE=${RELEASE}  BASE_DOMAIN=${BASE_DOMAIN}"

  log "Applying WC resources to management cluster..."
  render_mc_manifests | kmc apply -f -

  log "Waiting for workload cluster to become available (this takes ~5 min)..."
  sleep 300
  kmc wait --for=condition=Available -n org-giantswarm clusters.cluster.x-k8s.io "${WC}" --timeout=600s
  log "Workload cluster is ready."

  log "Checking aws-lb-controller-bundle deployment status..."
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-aws-lb-controller-bundle --timeout=600s
  log "aws-lb-controller-bundle is deployed."

  log "Waiting for gateway-api apps..."
  sleep 60
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-crds --timeout=300s
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-config --timeout=1200s
  log "gateway-api CRDs and config deployed."

  log "Checking ${PROXY_CONTROLLER} (${REVERSE_PROXY_APP_NAME}) deployment status..."
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${REVERSE_PROXY_APP_NAME}" --timeout=600s
  log "${PROXY_CONTROLLER} is deployed."
}

cmd_app() {
  log "Checking microservices-demo-app HelmRelease..."
  kmc wait --for=condition=Ready -n org-giantswarm helmrelease "${WC}"-microservices-demo-app --timeout=600s
  log "microservices-demo-app HelmRelease is ready."

  log "Checking demo app ${PROXY_CONTROLLER} ingress endpoint..."
  local ingress_url
  case "${PROXY_CONTROLLER}" in
    nginx) ingress_url="https://${REVERSE_PROXY_HOST}-0.${WC}.${BASE_DOMAIN}/" ;;
    kong)  ingress_url="https://${REVERSE_PROXY_HOST}.${WC}.${BASE_DOMAIN}/" ;;
  esac
  local attempts=0
  until curl -sf --max-time 10 "${ingress_url}" | grep -q "</html>"; do
    ((attempts++))
    if [[ ${attempts} -ge 20 ]]; then
      err "${PROXY_CONTROLLER} endpoint ${ingress_url} not reachable after ~60 min. Aborting."
      exit 1
    fi
    sleep 180
  done
  log "Demo app is reachable via ${PROXY_CONTROLLER} at ${ingress_url}"

  log "Checking demo app envoy gateway endpoint..."
  local envoy_url="https://onlineboutique.loadtesting-0.${WC}.${BASE_DOMAIN}/"
  attempts=0
  until curl -sf --max-time 10 "${envoy_url}" | grep -q "</html>"; do
    ((attempts++))
    if [[ ${attempts} -ge 20 ]]; then
      err "Envoy endpoint ${envoy_url} not reachable after ~60 min. Aborting."
      exit 1
    fi
    sleep 180
  done
  log "Demo app is reachable via envoy at ${envoy_url}"
}

cmd_k6() {
  log "Deploying k6 load test (context=${K6_CONTEXT}, namespace=${K6_NAMESPACE})..."

  # Mirror the prometheus remote-write credentials from alloy-metrics into the
  # k6 namespace. The TestRun runner consumes them via envFrom — the secret
  # must exist before the TestRun is applied or the runner pods won't start.
  log "Mirroring alloy-metrics credentials into ${K6_NAMESPACE}/k6-prometheus-rw-credentials..."
  local metrics_username metrics_password
  metrics_username="$(kk6 get secret alloy-metrics -n kube-system -o jsonpath='{.data.metrics-username}')"
  metrics_password="$(kk6 get secret alloy-metrics -n kube-system -o jsonpath='{.data.metrics-password}')"
  if [[ -z "${metrics_username}" || -z "${metrics_password}" ]]; then
    err "Could not read metrics-username/metrics-password from kube-system/alloy-metrics on ${K6_CONTEXT}."
    exit 1
  fi
  kk6 apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: k6-prometheus-rw-credentials
  namespace: ${K6_NAMESPACE}
type: Opaque
data:
  K6_PROMETHEUS_RW_USERNAME: ${metrics_username}
  K6_PROMETHEUS_RW_PASSWORD: ${metrics_password}
EOF

  render_k6_manifests | kk6 apply -f -
  log "TestRun deployed. Watch progress:"
  log "  kubectl --context=${K6_CONTEXT} get testrun -n ${K6_NAMESPACE} -w"
  log "  kubectl --context=${K6_CONTEXT} logs -n ${K6_NAMESPACE} -l k6_cr=envoy-gateway-load-test -f"
}

cmd_all() {
  log "=== Full pipeline: WC → App → k6 ==="
  log "  MC: ${MC_CONTEXT}  k6: ${K6_CONTEXT}"
  cmd_wc
  cmd_app
  cmd_k6
  log "=== Pipeline complete ==="
}

cmd_status() {
  echo ""
  log "Workload cluster (MC: ${MC_CONTEXT}):"
  kmc get clusters.cluster.x-k8s.io -n org-giantswarm "${WC}" 2>/dev/null || warn "  Not found"

  echo ""
  log "Apps on MC (${MC_CONTEXT}):"
  kmc get app -n org-giantswarm -l "giantswarm.io/cluster=${WC}" 2>/dev/null || warn "  None found"

  echo ""
  log "k6 TestRuns (${K6_CONTEXT}):"
  kk6 get testrun -n "${K6_NAMESPACE}" 2>/dev/null || warn "  Not found"
  echo ""
}

cmd_teardown() {
  warn "This will delete workload cluster '${WC}' and all associated resources."
  warn "  MC: ${MC_CONTEXT}  k6: ${K6_CONTEXT}"
  read -rp "Type the cluster name to confirm: " confirm
  if [[ "${confirm}" != "${WC}" ]]; then
    err "Confirmation did not match. Aborting."
    exit 1
  fi

  log "Deleting k6 resources (${K6_CONTEXT})..."
  render_k6_manifests | kk6 delete -f - --ignore-not-found 2>/dev/null || true
  kk6 delete secret k6-prometheus-rw-credentials -n "${K6_NAMESPACE}" --ignore-not-found 2>/dev/null || true

  log "Deleting WC resources (${MC_CONTEXT})..."
  render_mc_manifests_all | kmc delete -f - --ignore-not-found

  log "Teardown complete."
}

# ------------------------------------------------------------------
# Main
# ------------------------------------------------------------------

case "${1:-}" in
  all)      cmd_all ;;
  wc)       cmd_wc ;;
  app)      cmd_app ;;
  k6)       cmd_k6 ;;
  preview)  cmd_preview ;;
  status)   cmd_status ;;
  teardown) cmd_teardown ;;
  *)        usage; exit 1 ;;
esac
