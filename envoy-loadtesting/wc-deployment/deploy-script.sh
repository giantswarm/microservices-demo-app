#!/usr/bin/env bash

AZ="eu-north-1a"
RELEASE="34.1.0"
MC="graveler"
WC="envoyloadtesting"

# Configure WC file

sed -i "26s/- .*/- ${AZ}/" graveler-loadtesting.yaml
sed -i "33s/version: .*/version: ${RELEASE}/g" graveler-loadtesting.yaml

# 1- Create the WC
echo "Deploying the workload cluster and waiting until it's ready..."
kubectl apply -f graveler-loadtesting.yaml

# 2- Wait for the WC to be ready
sleep 300
kubectl wait --for=condition=Available -n org-giantswarm clusters.cluster.x-k8s.io "${WC}"
echo "Workload cluster is ready."

# 3- Deploy additional apps
echo "Deploying gateway-api-bundle and aws-lb-controller-bundle..."
kubectl apply -f graveler-wc-additional-apps.yaml

# 4- Wait for the additional apps to be ready
sleep 60
kubectl wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-crds --timeout=300s
kubectl wait --for=jsonpath='{.status.release.status}'=deployed -n org-giantswarm app "${WC}"-gateway-api-config --timeout=1200s
echo "gateway-api CRDs deployed and gateway-api-config app is ready."

# 5- log into the WC and deploy the microservice-demo chart
echo "Logging into the workload cluster and deploying the microservice-demo chart..."
tsh kube login "${MC}"-"${WC}"
# set the proxy protocol to optional in the default CTP
kubectl patch clienttrafficpolicy gateway-giantswarm-default -n envoy-gateway-system --type merge -p '{"spec":{"proxyProtocol":{"optional":true}}}'
kubectl create ns loadtesting
helm install onlineboutique helm-chart -n loadtesting
echo "microservice-demo chart deployed."

# 6- Wait for the the demo-app public endpoint to be reachable
echo "Waiting for the demo app to be reachable..."
while ! curl -v https://onlineboutique.loadtesting-0."${WC}".gaws2.gigantic.io/ | grep "</html>"; do
  sleep 180
done

echo "Demo app is reachable and ready for load testing."