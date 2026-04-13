package basic

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/giantswarm/apptest-framework/v4/pkg/state"
	"github.com/giantswarm/apptest-framework/v4/pkg/suite"
	"github.com/giantswarm/clustertest/v4/pkg/logger"
	"github.com/giantswarm/clustertest/v4/pkg/wait"
)

const (
	isUpgrade = false
)

const additionalAppConfig = `
ingress:
  base: %s
httproute:
  base: %s
`

func TestBasic(t *testing.T) {
	suite.New().
		WithInstallNamespace("loadtesting").
		WithIsUpgrade(isUpgrade).
		WithValuesFile("./values.yaml").
		AfterClusterReady(func() {
			It("should configure app values", func() {
				baseDomain := getWorkloadClusterBaseDomain()
				state.SetApplication(
					state.GetApplication().MustWithValues(fmt.Sprintf(additionalAppConfig, baseDomain, baseDomain), nil),
				)
			})

			It("should install dependencies", func() {
				mcName := state.GetFramework().MC().GetClusterName()
				clusterName := state.GetCluster().Name

				// Deploy all apps
				ingressNginx := deployDependency("ingress-nginx", ingressNginxValues)
				awsLB := deployDependency("aws-lb-controller-bundle", fmt.Sprintf(awsLBControllerBundleValues, mcName, clusterName, clusterName))
				gatewayAPI := deployDependency("gateway-api-bundle", fmt.Sprintf(gatewayApiBundleValues, clusterName))

				// Wait for all
				waitForDependency(awsLB)
				waitForDependency(gatewayAPI)
				waitForDependency(ingressNginx)
			})
		}).
		Tests(func() {
			var (
				nginxUrl string
				envoyUrl string
			)
			BeforeEach(func() {
				nginxUrl = fmt.Sprintf("https://nginx-onlineboutique-0.loadtesting.%s", getWorkloadClusterBaseDomain())
				envoyUrl = fmt.Sprintf("https://onlineboutique.loadtesting-0.%s", getWorkloadClusterBaseDomain())
			})
			It("should have deployed the test app", func() {
				Eventually(func() (bool, error) {
					done, err := wait.IsAppDeployed(state.GetContext(), state.GetFramework().MC(), state.GetApplication().InstallName, state.GetApplication().Organization.GetNamespace())()
					if err != nil {
						if errors.IsNotFound(err) {
							logger.Log("App '%s/%s' doesn't exist yet", state.GetApplication().Organization.GetNamespace(), state.GetApplication().InstallName)
							return false, nil
						}
						return false, err
					}

					return done, nil
				}).
					WithTimeout(5 * time.Minute).
					WithPolling(5 * time.Second).
					Should(BeTrue())
			})
			It("should serve traffic from ingress-nginx", func() {
				httpClient := newHttpClientWithProxy()
				Eventually(func() (string, error) {
					logger.Log("Trying to get a successful response from %s", nginxUrl)
					resp, err := httpClient.Get(nginxUrl)
					if err != nil {
						return "", err
					}
					defer resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						logger.Log("Was expecting status code '%d' but actually got '%d'", http.StatusOK, resp.StatusCode)
						return "", err
					}

					bodyBytes, err := io.ReadAll(resp.Body)
					if err != nil {
						logger.Log("Was not expecting the response body to be empty")
						return "", err
					}

					return string(bodyBytes), nil
				}).
					WithTimeout(15 * time.Minute).
					WithPolling(5 * time.Second).
					Should(ContainSubstring("Online Boutique"))
			})
			It("should serve traffic from envoy gateway", func() {
				httpClient := newHttpClientWithProxy()
				Eventually(func() (string, error) {
					logger.Log("Trying to get a successful response from %s", envoyUrl)
					resp, err := httpClient.Get(envoyUrl)
					if err != nil {
						return "", err
					}
					defer resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						logger.Log("Was expecting status code '%d' but actually got '%d'", http.StatusOK, resp.StatusCode)
						return "", err
					}

					bodyBytes, err := io.ReadAll(resp.Body)
					if err != nil {
						logger.Log("Was not expecting the response body to be empty")
						return "", err
					}

					return string(bodyBytes), nil
				}).
					WithTimeout(15 * time.Minute).
					WithPolling(5 * time.Second).
					Should(ContainSubstring("Online Boutique"))
			})
		}).
		Run(t, "Basic Test")
}
