package basic

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/giantswarm/apptest-framework/v5/pkg/state"
	"github.com/giantswarm/apptest-framework/v5/pkg/suite"
	"github.com/giantswarm/clustertest/v5/pkg/application"
	"github.com/giantswarm/clustertest/v5/pkg/logger"
	"github.com/giantswarm/clustertest/v5/pkg/wait"
)

const (
	isUpgrade = false

	proxyControllerNginx = "nginx"
	proxyControllerKong  = "kong"

	// proxyControllerEnvVar selects which ingress controller is deployed
	// alongside Envoy Gateway. Mirrors PROXY_CONTROLLER in
	// envoy-loadtesting/config.env so the e2e suite and the manual
	// load-testing pipeline exercise the same single-controller setup.
	proxyControllerEnvVar = "PROXY_CONTROLLER"
)

// proxyController is the ingress controller that this suite will install,
// resolved once at package init from the PROXY_CONTROLLER env var.
// Default: nginx.
var proxyController = resolveProxyController()

func resolveProxyController() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(proxyControllerEnvVar)))
	switch v {
	case "":
		return proxyControllerNginx
	case proxyControllerNginx, proxyControllerKong:
		return v
	default:
		panic(fmt.Sprintf("%s must be %q or %q (got: %q)", proxyControllerEnvVar, proxyControllerNginx, proxyControllerKong, v))
	}
}

// buildAppValues returns the per-controller values overlay applied to the
// microservices-demo-app HelmRelease. Only the chosen controller's routing
// path is enabled, matching envoy-loadtesting/wc-deployment/loadtesting-app.yaml.
func buildAppValues(baseDomain string) string {
	switch proxyController {
	case proxyControllerKong:
		return fmt.Sprintf(`
ingress:
  enabled: false
kong:
  enabled: true
  base: %s
  ingressCname: kong-ingress.%s
httproute:
  base: %s
`, baseDomain, baseDomain, baseDomain)
	default:
		return fmt.Sprintf(`
ingress:
  enabled: true
  base: %s
kong:
  enabled: false
httproute:
  base: %s
`, baseDomain, baseDomain)
	}
}

func TestBasic(t *testing.T) {
	suite.New().
		WithInstallNamespace("loadtesting").
		WithIsUpgrade(isUpgrade).
		WithValuesFile("./values.yaml").
		AfterClusterReady(func() {
			var (
				awsLBApp        *application.Application
				ingressNginxApp *application.Application
				gatewayAPIApp   *application.Application
				kongApp         *application.Application
			)

			It("should configure app values", FlakeAttempts(3), func() {
				baseDomain := getWorkloadClusterBaseDomain()
				state.SetApplication(
					state.GetApplication().MustWithValues(buildAppValues(baseDomain), nil),
				)
			})

			It("should create the loadtesting namespace", FlakeAttempts(3), func() {
				createWorkloadClusterNamespace("loadtesting")
			})

			It("should install aws-load-balancer-controller", FlakeAttempts(3), func() {
				mcName := state.GetFramework().MC().GetClusterName()
				clusterName := state.GetCluster().Name
				awsLBApp = deployDependency("aws-lb-controller-bundle", fmt.Sprintf(awsLBControllerBundleValues, mcName, clusterName, clusterName))
			})

			if proxyController == proxyControllerNginx {
				It("should install ingress-nginx", FlakeAttempts(3), func() {
					ingressNginxApp = deployDependency("ingress-nginx", ingressNginxValues)
				})
			}

			It("should wait for aws-load-balancer-controller to be ready", FlakeAttempts(3), func() {
				waitForDependency(awsLBApp)
			})

			It("should install gateway-api-bundle", FlakeAttempts(3), func() {
				clusterName := state.GetCluster().Name
				gatewayAPIApp = deployDependency("gateway-api-bundle", fmt.Sprintf(gatewayApiBundleValues, clusterName))
				waitForDependency(gatewayAPIApp)
			})

			It("should have gateway api CRDs registered", FlakeAttempts(3), func() {
				for _, crd := range []string{
					"gateways.gateway.networking.k8s.io",
					"httproutes.gateway.networking.k8s.io",
					"xlistenersets.gateway.networking.x-k8s.io",
				} {
					Eventually(func() (bool, error) {
						return crdExists(crd)
					}).
						WithTimeout(5 * time.Minute).
						WithPolling(10 * time.Second).
						Should(BeTrue())
				}
			})

			if proxyController == proxyControllerNginx {
				It("should wait for ingress-nginx to be ready", FlakeAttempts(3), func() {
					waitForDependency(ingressNginxApp)
				})
			}

			if proxyController == proxyControllerKong {
				It("should install kong-app", FlakeAttempts(3), func() {
					baseDomain := getWorkloadClusterBaseDomain()
					kongApp = deployDependency("kong-app", fmt.Sprintf(kongAppValues, baseDomain), "kong")
					waitForDependency(kongApp)
				})
			}

			It("should have ready dependency deployments on the workload cluster", FlakeAttempts(3), func() {
				namespaces := []string{"aws-load-balancer-controller", "envoy-gateway-system", "default"}
				if proxyController == proxyControllerKong {
					namespaces = append(namespaces, "kong")
				}
				for _, ns := range namespaces {
					Eventually(func() (bool, error) {
						return deploymentReadyInNamespace(ns)
					}).
						WithTimeout(10 * time.Minute).
						WithPolling(5 * time.Second).
						Should(BeTrue())
				}
			})

			It("should have ready LoadBalancer services on the workload cluster", FlakeAttempts(3), func() {
				namespaces := []string{"default", "envoy-gateway-system"}
				if proxyController == proxyControllerKong {
					namespaces = append(namespaces, "kong")
				}
				for _, ns := range namespaces {
					Eventually(func() (bool, error) {
						return loadBalancerServiceReadyInNamespace(ns)
					}).
						WithTimeout(10 * time.Minute).
						WithPolling(5 * time.Second).
						Should(BeTrue())
				}
			})

			if proxyController == proxyControllerKong {
				It("should configure kong prometheus plugin", FlakeAttempts(3), func() {
					By("Waiting for KongClusterPlugin CRD to be registered")
					Eventually(func() (bool, error) {
						return crdExists("kongclusterplugins.configuration.konghq.com")
					}).
						WithTimeout(5 * time.Minute).
						WithPolling(10 * time.Second).
						Should(BeTrue())

					By("Adding extraObjects config to kong-app via spec.extraConfigs")
					clusterName := state.GetCluster().Name
					addExtraConfigToApp(
						fmt.Sprintf("%s-kong-app", clusterName),
						fmt.Sprintf("%s-kong-extra-objects", clusterName),
						kongExtraObjectsValues,
					)
				})
			}
		}).
		Tests(func() {
			var (
				nginxUrl string
				envoyUrl string
				kongUrl  string
			)
			BeforeEach(func() {
				nginxUrl = fmt.Sprintf("https://nginx-onlineboutique-0.loadtesting.%s", getWorkloadClusterBaseDomain())
				envoyUrl = fmt.Sprintf("https://onlineboutique.loadtesting-0.%s", getWorkloadClusterBaseDomain())
				kongUrl = fmt.Sprintf("https://kong-onlineboutique-0.loadtesting.%s", getWorkloadClusterBaseDomain())
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
			It("should have ready certificates on the workload cluster", func() {
				expected := []types.NamespacedName{
					{Namespace: "loadtesting-0", Name: "gateway-0-https"},
				}
				switch proxyController {
				case proxyControllerNginx:
					expected = append(expected, types.NamespacedName{Namespace: "loadtesting", Name: "frontend-nginx-wildcard"})
				case proxyControllerKong:
					expected = append(expected, types.NamespacedName{Namespace: "loadtesting", Name: "frontend-kong-wildcard"})
				}

				Eventually(func() (bool, error) {
					return allCertificatesReady(expected)
				}).
					WithTimeout(10 * time.Minute).
					WithPolling(5 * time.Second).
					Should(BeTrue())
			})
			if proxyController == proxyControllerNginx {
				It("should serve traffic from ingress-nginx", func() {
					DeferCleanup(func() {
						if CurrentSpecReport().Failed() {
							AbortSuite("ingress-nginx failed to serve traffic, aborting remaining tests")
						}
					})
					expectEndpointServesTraffic(nginxUrl)
				})
			}
			It("should serve traffic from envoy gateway", func() {
				DeferCleanup(func() {
					if CurrentSpecReport().Failed() {
						AbortSuite("envoy gateway failed to serve traffic, aborting remaining tests")
					}
				})
				expectEndpointServesTraffic(envoyUrl)
			})
			if proxyController == proxyControllerKong {
				It("should serve traffic from kong", func() {
					DeferCleanup(func() {
						if CurrentSpecReport().Failed() {
							AbortSuite("kong failed to serve traffic, aborting remaining tests")
						}
					})
					expectEndpointServesTraffic(kongUrl)
				})
			}
			It("should run k6 load tests successfully", func() {
				k6Namespace := getK6Namespace()
				baseDomain := getWorkloadClusterBaseDomain()
				testRunName := fmt.Sprintf("e2e-load-test-%s", state.GetCluster().Name)
				configMapName := fmt.Sprintf("e2e-load-test-scenario-%s", state.GetCluster().Name)
				testID := envOrDefault("K6_TEST_ID", testRunName)

				// Clean up any stale resources from a previous interrupted run
				cleanupK6Resources(testRunName, configMapName, k6Namespace)

				if prometheusEnabled() {
					By("Mirroring alloy-metrics credentials into the k6 namespace")
					mirrorPrometheusCredentials(k6Namespace)
				}

				By("Creating test scenario ConfigMap on the MC")
				cm := buildScenarioConfigMap(configMapName, k6Namespace)
				err := state.GetFramework().MC().Create(state.GetContext(), cm)
				Expect(err).NotTo(HaveOccurred())

				By("Creating TestRun on the MC")
				testRun := buildTestRunUnstructured(testRunName, k6Namespace, configMapName, baseDomain, testID)
				err = state.GetFramework().MC().Create(state.GetContext(), testRun)
				Expect(err).NotTo(HaveOccurred())

				By("Waiting for TestRun to complete")
				var lastStage string
				Eventually(func() (string, error) {
					stage, err := getTestRunStage(testRunName, k6Namespace)
					if err != nil {
						return "", err
					}
					if stage != "" && stage != testRunGone {
						lastStage = stage
					}
					return stage, nil
				}).
					WithTimeout(120 * time.Minute).
					WithPolling(30 * time.Second).
					Should(BeElementOf("finished", "error", testRunGone))

				By("Asserting TestRun succeeded")
				assertTestRunSuccess(testRunName, k6Namespace, lastStage)

				By("Cleaning up k6 resources")
				cleanupK6Resources(testRunName, configMapName, k6Namespace)
			})
		}).
		AfterSuite(func() {
			k6Namespace := getK6Namespace()
			testRunName := fmt.Sprintf("e2e-load-test-%s", state.GetCluster().Name)
			configMapName := fmt.Sprintf("e2e-load-test-scenario-%s", state.GetCluster().Name)
			cleanupK6Resources(testRunName, configMapName, k6Namespace)
		}).
		Run(t, "Basic Test")
}
