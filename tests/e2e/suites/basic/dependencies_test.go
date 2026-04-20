package basic

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	applicationv1alpha1 "github.com/giantswarm/apiextensions-application/api/v1alpha1"
	"github.com/giantswarm/apptest-framework/v4/pkg/state"
	"github.com/giantswarm/clustertest/v4/pkg/application"
	"github.com/giantswarm/clustertest/v4/pkg/logger"
	"github.com/giantswarm/clustertest/v4/pkg/wait"

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
  createIngressClass: true
  watchNamespaces:
    - loadtesting
    - kong
  rbac:
    gatewayAPI:
      enabled: false
proxy:
  annotations:
    giantswarm.io/external-dns: managed
    external-dns.alpha.kubernetes.io/hostname: kong-ingress.%s
`

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
`

const ingressNginxValues = `
controller:
  extraArgs:
    update-status: "true"

`

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

	clusterName := state.GetCluster().Name
	app := application.New(fmt.Sprintf("%s-%s", clusterName, depName), depName).
		WithCatalog("giantswarm").
		WithOrganization(*org).
		WithVersion("latest").
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
