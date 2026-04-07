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

# Shorthand kubectl wrappers per cluster
kmc()  { kubectl --context="${MC_CONTEXT}" "$@"; }
kwc()  { kubectl --context="${WC_CONTEXT}" "$@"; }
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
  WC_CONTEXT=${WC_CONTEXT}    (workload cluster — Helm chart, ingress, envoy)
  K6_CONTEXT=${K6_CONTEXT}    (k6 cluster — TestRun + scenario)

Infrastructure:
  WC=${WC}  MC=${MC}  BASE_DOMAIN=${BASE_DOMAIN}  AZ=${AZ}  RELEASE=${RELEASE}
EOF
}

check_prerequisites() {
  local missing=()
  for cmd in kubectl yq helm tsh curl; do
    command -v "${cmd}" &>/dev/null || missing+=("${cmd}")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing required tools: ${missing[*]}"
    exit 1
  fi
}

render_manifests() {
  kubectl kustomize "${SCRIPT_DIR}"
}

render_mc_manifests() {
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

  log "Waiting for gateway-api apps..."
  sleep 60
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-crds --timeout=300s
  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-config --timeout=1200s
  log "gateway-api CRDs and config deployed."
}

cmd_app() {

  log "Patching ClientTrafficPolicy for optional proxy protocol... (context=${WC_CONTEXT})"
  kwc patch clienttrafficpolicy gateway-giantswarm-default -n envoy-gateway-system \
    --type merge -p '{"spec":{"proxyProtocol":{"optional":true}}}'

  log "Creating loadtesting namespace..."
  kwc create ns loadtesting --dry-run=client -o yaml | kwc apply -f -

  log "Waiting for nginx IngressClass..."
  until kwc get ingressclass nginx &>/dev/null; do
    sleep 60
  done
  log "nginx IngressClass is ready."

  kmc wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-config --timeout=1200s

  log "Waiting for the demo app endpoint to become reachable..."
  local url="https://onlineboutique.loadtesting-0.${WC}.${BASE_DOMAIN}/"
  local attempts=0
  until curl -sf --max-time 10 "${url}" | grep -q "</html>"; do
    ((attempts++))
    if [[ ${attempts} -ge 20 ]]; then
      err "Endpoint ${url} not reachable after ~60 min. Aborting."
      exit 1
    fi
    sleep 180
  done
  log "Demo app is reachable at ${url}"
}

cmd_k6() {
  log "Deploying k6 load test (context=${K6_CONTEXT}, namespace=${K6_NAMESPACE})..."
  render_k6_manifests | kk6 apply -f -
  log "TestRun deployed. Watch progress:"
  log "  kubectl --context=${K6_CONTEXT} get testrun -n ${K6_NAMESPACE} -w"
  log "  kubectl --context=${K6_CONTEXT} logs -n ${K6_NAMESPACE} -l k6_cr=envoy-gateway-load-test -f"
}

cmd_all() {
  log "=== Full pipeline: WC → App → k6 ==="
  log "  MC: ${MC_CONTEXT}  WC: ${WC_CONTEXT}  k6: ${K6_CONTEXT}"
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
  log "Helm releases on WC (${WC_CONTEXT}):"
  helm --kube-context="${WC_CONTEXT}" list -n loadtesting 2>/dev/null || warn "  Not reachable"

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

  log "Deleting WC resources (${MC_CONTEXT})..."
  render_mc_manifests | kmc delete -f - --ignore-not-found

  log "Teardown complete."
}

# ------------------------------------------------------------------
# Main
# ------------------------------------------------------------------

check_prerequisites

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
