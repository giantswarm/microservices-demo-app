# k6 Load Testing

k6 test scenario and TestRun CRD for comparing Envoy Gateway vs Nginx Ingress performance.

## Configuration

All tunables are in the shared `../config.env`. This directory is built as part of the
parent kustomization — it cannot be built standalone.

```bash
# Build (from envoy-loadtesting/)
kubectl kustomize ..

# Deploy k6 resources only
kubectl kustomize .. | yq 'select(.metadata.namespace == "gs-k6-operator")' | kubectl apply -f -

# Or use the deploy script
./deploy-test.sh
```

## Structure

- `test-scenario.js` — k6 test scenario (reads tunables from `__ENV` at runtime)
- `testrun.yaml` — k6-operator TestRun CRD with `$(VAR)` placeholders
- `kustomization.yaml` — generates the test script ConfigMap

## How it connects to wc-deployment

The k6 `BASE_DOMAIN` env var is composed as `$(WC).$(BASE_DOMAIN)` — so changing
`WC=mycluster` in `../config.env` automatically targets `mycluster.gaws2.gigantic.io`.
