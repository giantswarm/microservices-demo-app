#!/bin/bash

# The script returns a kubeconfig for the service account given
# you need to have kubectl on PATH with the context set to the cluster you want to create the config for

# Cosmetics for the created config
clusterName=alba-seu01
# your server address goes here get it via `kubectl cluster-info`
# Use a opsctl login address (teleport does not work for this)
server=https://api.seu01.capi.aws.k8s.3stripes.net:443
# the Namespace and ServiceAccount name that is used for the config
namespace=gs-k6-operator
serviceAccount=gs-k6-testrun-manager

######################
# actual script starts
set -o errexit

ca=$(kubectl get cm kube-root-ca.crt -o jsonpath="{['data']['ca\.crt']}" | base64 -w0)
token=$(kubectl --namespace $namespace create token $serviceAccount --duration=8766h)

echo "
---
apiVersion: v1
kind: Config
clusters:
  - name: ${clusterName}
    cluster:
      certificate-authority-data: ${ca}
      server: ${server}
contexts:
  - name: ${serviceAccount}@${clusterName}
    context:
      cluster: ${clusterName}
      namespace: ${namespace}
      user: ${serviceAccount}
users:
  - name: ${serviceAccount}
    user:
      token: ${token}
current-context: ${serviceAccount}@${clusterName}
"
