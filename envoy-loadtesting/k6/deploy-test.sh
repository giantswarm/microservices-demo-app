#!/usr/bin/env bash

NAMESPACE="gs-k6-operator"

kubectl create cm envoy-load-testing-scenario -n "${NAMESPACE}" --from-file=test-scenario.js --dry-run=client -oyaml > configmap.yaml
kubectl apply -f configmap.yaml
kubectl apply -f test-run.yaml