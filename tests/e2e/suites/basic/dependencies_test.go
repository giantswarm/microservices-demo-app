package basic

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/giantswarm/apptest-framework/v3/pkg/state"
	"github.com/giantswarm/clustertest/v3/pkg/application"
	"github.com/giantswarm/clustertest/v3/pkg/wait"
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

const ingressNginxValues = `
controller:
  extraArgs:
    update-status: "true"

`

func installDependency(depName, depValues string) {
	It(fmt.Sprintf("should have %s deployed", depName), func() {
		org := state.GetCluster().Organization
		clusterName := state.GetCluster().Name
		app := application.New(fmt.Sprintf("%s-%s", clusterName, depName), depName).
			WithCatalog("giantswarm").
			WithOrganization(*org).
			WithVersion("latest").
			WithClusterName(clusterName).
			WithInCluster(true).
			WithInstallNamespace(org.GetNamespace()).
			MustWithValues(depValues, nil)

		err := state.GetFramework().MC().DeployApp(state.GetContext(), *app)
		Expect(err).NotTo(HaveOccurred())

		Eventually(wait.IsAppDeployed(state.GetContext(), state.GetFramework().MC(), app.InstallName, org.GetNamespace())).
			WithTimeout(10 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())
	})
}

func installDependencies() {
	mcName := state.GetFramework().MC().GetClusterName()
	clusterName := state.GetCluster().Name

	installDependency("aws-lb-controller-bundle", fmt.Sprintf(awsLBControllerBundleValues, mcName, clusterName, clusterName))
	installDependency("gateway-api-bundle", fmt.Sprintf(gatewayApiBundleValues, clusterName))
	installDependency("ingress-nginx", ingressNginxValues)
}
