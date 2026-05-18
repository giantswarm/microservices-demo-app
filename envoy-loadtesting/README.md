# Envoy Gateway Load Testing

End-to-end infrastructure for comparing Envoy Gateway against a chosen reverse proxy
controller (Nginx Ingress *or* Kong) on Giant Swarm workload clusters, using
Google's Online Boutique as the target workload.

The controller is selected via `PROXY_CONTROLLER` in `config.env`. Only the
chosen controller's App is deployed alongside Envoy Gateway, and the k6 run
compares `envoy_simulation` against the matching `<controller>_simulation`.
To compare both, run the pipeline twice — once per setting.

Related issue: [giantswarm/giantswarm#35147](https://github.com/giantswarm/giantswarm/issues/35147)

## Quick start

```bash
# 1. Edit config.env with your cluster details
vim config.env

# 2. Run the full pipeline (deploy WC → install app → start k6 tests)
./deploy.sh all
```

Or run each step individually:

```bash
./deploy.sh wc        # Create workload cluster + gateway-api + ingress-nginx
./deploy.sh app       # Install microservices-demo Helm chart into the WC
./deploy.sh k6        # Deploy and start the k6 load test
./deploy.sh status    # Check state of cluster, apps, and test runs
./deploy.sh teardown  # Delete everything (with confirmation)
./deploy.sh preview   # Render all Kustomize manifests (dry-run)
```

## Configuration

All tunables live in a single file: **`config.env`**.

### Cluster contexts

Three separate clusters are involved. Each gets its own kubectl context:

| Variable      | Default                | Target cluster | What runs there |
|---------------|------------------------|----------------|-----------------|
| `MC_CONTEXT`  | `teleport.giantswarm.io-graveler`             | Management cluster | WC App CRs, ConfigMaps (`org-giantswarm` namespace) |
| `K6_CONTEXT`  | `teleport.giantswarm.io-alba-seu01`           | k6 cluster         | k6-operator TestRun + scenario ConfigMap |

```
  MC (graveler)                 WC (graveler-envoyloadtesting)          k6 (alba-seu01)
  ─────────────                 ───────────────────────          ───────────────
  org-giantswarm namespace:     loadtesting namespace:           gs-k6-operator namespace:
  ├─ cluster-aws App CR         ├─ microservices-demo Helm       ├─ TestRun CR
  ├─ gateway-api-bundle App     ├─ envoy-gateway                 └─ test scenario ConfigMap
  ├─ aws-lb-controller App      └─ ingress-nginx
  └─ ingress-nginx App
```

### Shared infrastructure

| Variable      | Default              | Description                          |
|---------------|----------------------|--------------------------------------|
| `WC`          | `envoyloadtesting`          | Workload cluster name                |
| `MC`          | `graveler`           | Management cluster name              |
| `BASE_DOMAIN` | `gaws2.gigantic.io`  | Base DNS domain                      |
| `AZ`          | `eu-north-1a`        | AWS availability zone for node pools |
| `RELEASE`     | `34.1.0`             | Giant Swarm release version          |
| `PROXY_CONTROLLER` | `nginx`       | Ingress controller to compare against Envoy. One of: `nginx`, `kong`. Drives which App CR is deployed, which side of the demo-app Helm values is enabled, and which k6 scenario runs. |

### k6 load testing

| Variable                     | Default    | Description                           |
|------------------------------|------------|---------------------------------------|
| `K6_NAMESPACE`               | `gs-k6-operator` | Namespace for k6-operator       |
| `K6_IMAGE`                   | `gsoci.azurecr.io/giantswarm/k6:1.6.0` | k6 container image |
| `TEST_ID`                    | `envoy-load-testing` | k6 test id used in grafana |
| `ENDPOINTS`                  | `10`       | Number of test endpoint replicas      |
| `SCENARIO_DURATION_SECONDS`  | `1200`     | Duration per scenario (20 min)        |
| `WAIT_BETWEEN_SCENARIOS`     | `300`      | Pause between Envoy and Nginx (5 min) |
| `ARRIVAL_RATE`               | `26`       | Requests per second (~50 HTTP req/s)  |
| `PRE_ALLOCATED_VUS`          | `50`       | Pre-allocated virtual users           |
| `MAX_VUS`                    | `150`      | Maximum virtual users                 |
| `GRACEFUL_STOP`              | `30s`      | Graceful shutdown period              |
| `PROMETHEUS_RW_URL`          | `http://mimir-gateway.mimir.svc.cluster.local/api/v1/push`      | prometheus remote-write target for pushing metrics             |
| `PROMETHEUS_RW_HEADERS_X_SCOPE_ORGID`              | `giantswarm`      | organisation name in grafana              |
| `PROMETHEUS_RW_PUSH_INTERVAL`              | `5s`      | Metrics push interval              |
| `SLO_P95_LATENCY_MS`        | `500`      | p95 latency threshold (ms)            |
| `SLO_P99_LATENCY_MS`        | `1000`     | p99 latency threshold (ms)            |
| `SLO_ERROR_RATE`             | `0.001`    | Max error rate (0.1%)                 |
| `SLO_CHECKS_RATE`            | `0.95`     | Min check pass rate (95%)             |

## Architecture

```
envoy-loadtesting/
├── config.env                 # Single source of truth for all variables
├── deploy.sh                  # Orchestration script (all / wc / app / k6 / status / teardown)
├── kustomization.yaml         # Top-level: vars from config.env, includes both sub-dirs
├── var-reference.yaml         # Tells Kustomize which fields to substitute $(VAR) in
│
├── wc-deployment/             # Workload cluster infrastructure
│   ├── kustomization.yaml     #   ConfigMap generators + App CR resources
│   ├── cluster.yaml           #   WC cluster-aws App CR
│   ├── additional-apps.yaml   #   gateway-api-bundle, aws-lb-controller, ingress-nginx
│   └── values/                #   ConfigMap data for each app
│       ├── cluster-userconfig.yaml
│       ├── gateway-api-bundle.yaml
│       ├── aws-lb-controller.yaml
│       └── ingress-nginx.yaml
│
└── k6/                        # k6 load testing
    ├── kustomization.yaml     #   ConfigMap generator (test script) + TestRun resource
    ├── test-scenario.js       #   k6 test scenario (Envoy vs Nginx comparison)
    └── testrun.yaml           #   k6-operator TestRun CRD
```

## How it works

### Kustomize vars

All `$(VAR)` placeholders across both sub-directories are resolved by the top-level
kustomization using Kustomize `vars`. The variable values come from `config.env`,
which is loaded into a non-deployed `cluster-variables` ConfigMap at build time.

The custom `var-reference.yaml` tells Kustomize which fields to scan for placeholders.
This includes metadata fields, labels, ConfigMap data (embedded YAML strings), App CR
spec references, and TestRun env values.

### Variable composition

The k6 test targets are derived from the shared infrastructure variables:

- WC endpoints: `https://onlineboutique.loadtesting-{0..N}.$(WC).$(BASE_DOMAIN)/`
- Nginx endpoints: `https://nginx-onlineboutique-{0..N}.loadtesting.$(WC).$(BASE_DOMAIN)/`

Changing `WC` in `config.env` updates both the cluster deployment and the test targets.

### Deployment pipeline

```
  deploy.sh wc                     deploy.sh app                   deploy.sh k6
  ─────────────                    ──────────────                  ─────────────
  ┌──────────────┐                 ┌──────────────┐               ┌──────────────┐
  │ Create WC    │                 │ tsh kube     │               │ Apply k6     │
  │ cluster-aws  │                 │ login MC-WC  │               │ ConfigMap +  │
  │ App CR       │                 │              │               │ TestRun CRD  │
  ├──────────────┤                 ├──────────────┤               ├──────────────┤
  │ Wait for WC  │ ─── ~5 min ──▶ │ Patch CTP    │               │ k6-operator  │
  │ Available    │                 │ Create ns    │               │ runs test    │
  ├──────────────┤                 │ Wait nginx   │               │ (4 runners)  │
  │ Deploy       │                 ├──────────────┤               └──────────────┘
  │ gateway-api  │                 │ helm install │
  │ aws-lb       │                 │ onlineboutique│
  │ ingress-nginx│                 ├──────────────┤
  ├──────────────┤                 │ Wait for     │
  │ Wait for     │                 │ endpoint     │
  │ apps ready   │                 │ reachable    │
  └──────────────┘                 └──────────────┘
```

## Prerequisites

- `kubectl` with access to the management cluster
- `tsh` (Teleport) for workload cluster login
- `helm` for installing the microservices-demo chart
- `yq` for filtering Kustomize output by namespace
- k6-operator running in the management cluster (`gs-k6-operator` namespace)
