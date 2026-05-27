package basic

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	applicationv1alpha1 "github.com/giantswarm/apiextensions-application/api/v1alpha1"
	"github.com/giantswarm/apptest-framework/v5/pkg/state"
	"github.com/giantswarm/clustertest/v5/pkg/application"
	"github.com/giantswarm/clustertest/v5/pkg/logger"
	"github.com/giantswarm/clustertest/v5/pkg/wait"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const awsLBControllerBundleValues = `
managementCluster:
  name: %s
  namespace: org-giantswarm

clusterName: %s
clusterID: %s

provider: aws

global:
  podSecurityStandards:
    enforced: true

enableServiceMutatorWebhook: false
`

const gatewayApiBundleValues = `
apps:
  envoyGateway:
    enabled: true
    userConfig:
      configMap:
        values: |
          config:
            envoyGateway:
              gatewayAPI:
                enabled:
                  - XListenerSet
  gatewayApiConfig:
    enabled: true
    userConfig:
      configMap:
        values: |
          gateways:
            default:
              envoyProxy:
                enabled: false
              allowedListeners:
                enabled: true
                namespaces:
                  from: All
              listeners:
                http:
                  httpsRedirectEnabled: true
                  allowedRoutes:
                    namespaces:
                      from: All
                https:
                  subdomains:
                    - onlineboutique
  gatewayApiCrds:
    enabled: true
    userConfig:
      configMap:
        values: |
          install:
            xlistenersets: "experimental"
            gateways: "experimental"
clusterID: %s
`

const kongAppValues = `
ingressController:
  enabled: true
  createIngressClass: false
  watchNamespaces:
    - loadtesting
    - kong
  rbac:
    gatewayAPI:
      enabled: true
  ingressClass: none  # Prevents KIC from reconciling Ingress resources; Gateway API only
proxy:
  annotations:
    giantswarm.io/external-dns: managed
    external-dns.alpha.kubernetes.io/hostname: kong-ingress.%s
`

// kongExtraObjectsValues is applied as an extraConfig overlay AFTER kong-app is
// deployed and its KongClusterPlugin CRD is registered. Inlining these into the
// initial kongAppValues fails on first install because Helm renders
// extraObjects before the chart's own CRDs are applied.
const kongExtraObjectsValues = `
extraObjects:
  - apiVersion: configuration.konghq.com/v1
    kind: KongClusterPlugin
    metadata:
      name: prometheus
      annotations:
        kubernetes.io/ingress.class: kong
      labels:
        global: "true"
    plugin: prometheus
    config:
      status_code_metrics: true
      latency_metrics: true
      bandwidth_metrics: true
      upstream_health_metrics: true
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: kong-app-kong-app-ingressclass-reader
    rules:
      - apiGroups: ["networking.k8s.io"]
        resources: ["ingressclasses"]
        verbs: ["get", "list", "watch"]
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: kong-app-kong-app-ingressclass-reader
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: kong-app-kong-app-ingressclass-reader
    subjects:
      - kind: ServiceAccount
        name: kong-app-kong-app
        namespace: kong
`

const ingressNginxValues = `
controller:
  ingressClassResource:
    enabled: true
  service:
    externalDNS:
      enabled: true
  extraArgs:
    update-status: "true"
`

// dependencyVersions pins the version of each dependency app to match the
// manual load-testing pipeline (envoy-loadtesting/wc-deployment/additional-apps.yaml).
// Keep in sync with that file so the e2e suite exercises the same versions the
// benchmark uses.
var dependencyVersions = map[string]string{
	"aws-lb-controller-bundle": "5.1.0",
	"ingress-nginx":            "4.2.5",
	"gateway-api-bundle":       "1.15.0",
	"kong-app":                 "5.2.2",
}

func deployDependency(depName, depValues string, installNs ...string) *application.Application {
	By(fmt.Sprintf("deploying %s", depName))

	org := state.GetCluster().Organization

	isBundle := strings.Contains(depName, "bundle")
	installNamespace := org.GetNamespace()
	if !isBundle {
		installNamespace = "default"
	}
	if len(installNs) > 0 && installNs[0] != "" {
		installNamespace = installNs[0]
	}

	version, ok := dependencyVersions[depName]
	if !ok {
		Fail(fmt.Sprintf("no version pin defined for dependency %q — add it to dependencyVersions", depName))
	}

	clusterName := state.GetCluster().Name
	app := application.New(fmt.Sprintf("%s-%s", clusterName, depName), depName).
		WithCatalog("giantswarm").
		WithOrganization(*org).
		WithVersion(version).
		WithClusterName(clusterName).
		WithInCluster(isBundle).
		WithInstallNamespace(installNamespace).
		MustWithValues(depValues, nil)

	err := state.GetFramework().MC().DeployApp(state.GetContext(), *app)
	Expect(err).NotTo(HaveOccurred())

	return app
}

func waitForDependency(app *application.Application) {
	By(fmt.Sprintf("waiting for %s to be deployed", app.InstallName))

	org := state.GetCluster().Organization
	Eventually(wait.IsAppDeployed(state.GetContext(), state.GetFramework().MC(), app.InstallName, org.GetNamespace())).
		WithTimeout(10 * time.Minute).
		WithPolling(5 * time.Second).
		Should(BeTrue())
}

// addExtraConfigToApp creates a ConfigMap with the given values and adds it
// to the App CR's spec.extraConfigs list. Idempotent — safe to call on retry.
func addExtraConfigToApp(appName, configMapName, values string) {
	ctx := state.GetContext()
	mc := state.GetFramework().MC()
	org := state.GetCluster().Organization
	namespace := org.GetNamespace()

	logger.Log("Ensuring extra config ConfigMap %s/%s for App %s", namespace, configMapName, appName)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"values": values,
		},
	}
	err := mc.Create(ctx, cm)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "failed to create extra config ConfigMap %s/%s", namespace, configMapName)
	}

	// Read the current App CR to preserve existing extraConfigs
	appCR := &applicationv1alpha1.App{}
	err = mc.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, appCR)
	Expect(err).NotTo(HaveOccurred(), "failed to get App CR %s/%s", namespace, appName)

	// Check if the extra config is already present
	for _, ec := range appCR.Spec.ExtraConfigs {
		if ec.Name == configMapName && ec.Namespace == namespace {
			logger.Log("Extra config %s already present on App %s/%s", configMapName, namespace, appName)
			return
		}
	}

	appCR.Spec.ExtraConfigs = append(appCR.Spec.ExtraConfigs, applicationv1alpha1.AppExtraConfig{
		Kind:      "configMap",
		Name:      configMapName,
		Namespace: namespace,
		Priority:  25,
	})

	err = mc.Update(ctx, appCR)
	Expect(err).NotTo(HaveOccurred(), "failed to update App CR %s/%s with extraConfigs", namespace, appName)

	logger.Log("Added extra config %s to App %s/%s", configMapName, namespace, appName)
}
