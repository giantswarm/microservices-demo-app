# Deploying the WC with the microservices-demo-app

## Configuration

All tunables are in the shared `../config.env`. This directory is built as part of the
parent kustomization — it cannot be built standalone.

| Variable      | Default              | Description                          |
|---------------|----------------------|--------------------------------------|
| `WC`          | `envoyloadtesting`          | Workload cluster name                |
| `MC`          | `graveler`           | Management cluster name (deploy script) |
| `BASE_DOMAIN` | `gaws2.gigantic.io`  | Base DNS domain                      |
| `AZ`          | `eu-north-1a`        | AWS availability zone for node pools |
| `RELEASE`     | `34.1.0`             | Giant Swarm release version          |

## Usage

```bash
# Preview all rendered manifests (from envoy-loadtesting/)
kubectl kustomize ..

# Deploy WC resources only
./deploy-script.sh
```

## Structure

```
wc-deployment/
├── kustomization.yaml         # ConfigMap generators + resource references
├── cluster.yaml               # WC App CR with $(VAR) placeholders
├── additional-apps.yaml       # gateway-api, aws-lb, ingress-nginx App CRs
├── values/
│   ├── cluster-userconfig.yaml      # WC cluster config data
│   ├── gateway-api-bundle.yaml      # Gateway API bundle config data
│   ├── aws-lb-controller.yaml       # AWS LB controller config (static)
│   └── ingress-nginx.yaml           # Ingress NGINX config (static)
├── deploy-script.sh           # End-to-end deployment script
└── README.md
```
