package basic

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/giantswarm/apptest-framework/v4/pkg/state"
	"github.com/giantswarm/clustertest/v4/pkg/application"
	"github.com/giantswarm/clustertest/v4/pkg/wait"
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
