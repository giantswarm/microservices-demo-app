package basic

import (
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
kong:
  base: %s
  ingressCname: kong-ingress.%s
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
					state.GetApplication().MustWithValues(fmt.Sprintf(additionalAppConfig, baseDomain, baseDomain, baseDomain, baseDomain), nil),
				)
			})

			It("should create the loadtesting namespace", func() {
				wcClient, err := state.GetFramework().WC(state.GetCluster().Name)
				Expect(err).NotTo(HaveOccurred())

				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "loadtesting",
					},
				}
				err = wcClient.Create(state.GetContext(), ns)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should install dependencies", func() {
				mcName := state.GetFramework().MC().GetClusterName()
				clusterName := state.GetCluster().Name
				baseDomain := getWorkloadClusterBaseDomain()

				// Deploy all apps
				ingressNginx := deployDependency("ingress-nginx", ingressNginxValues)
				awsLB := deployDependency("aws-lb-controller-bundle", fmt.Sprintf(awsLBControllerBundleValues, mcName, clusterName, clusterName))
				gatewayAPI := deployDependency("gateway-api-bundle", fmt.Sprintf(gatewayApiBundleValues, clusterName))
				kong := deployDependency("kong-app", fmt.Sprintf(kongAppValues, baseDomain), "kong")

				// Wait for all
				waitForDependency(awsLB)
				waitForDependency(gatewayAPI)
				waitForDependency(ingressNginx)
				waitForDependency(kong)
			})

			It("should have ready dependency deployments on the workload cluster", func() {
				for _, ns := range []string{"aws-load-balancer-controller", "envoy-gateway-system", "default", "kong"} {
					Eventually(func() (bool, error) {
						return deploymentReadyInNamespace(ns)
					}).
						WithTimeout(10 * time.Minute).
						WithPolling(5 * time.Second).
						Should(BeTrue())
				}
			})

			It("should have ready LoadBalancer services on the workload cluster", func() {
				for _, ns := range []string{"default", "envoy-gateway-system", "kong"} {
					Eventually(func() (bool, error) {
						return loadBalancerServiceReadyInNamespace(ns)
					}).
						WithTimeout(10 * time.Minute).
						WithPolling(5 * time.Second).
						Should(BeTrue())
				}
			})

			It("should configure kong prometheus plugin", func() {
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
				Eventually(func() (bool, error) {
					return certificateIsReady("loadtesting", "frontend-nginx-wildcard")
				}).
					WithTimeout(10 * time.Minute).
					WithPolling(5 * time.Second).
					Should(BeTrue())

				Eventually(func() (bool, error) {
					return certificateIsReady("loadtesting-0", "gateway-0-https")
				}).
					WithTimeout(10 * time.Minute).
					WithPolling(5 * time.Second).
					Should(BeTrue())

				Eventually(func() (bool, error) {
					return certificateIsReady("loadtesting", "frontend-kong-wildcard")
				}).
					WithTimeout(10 * time.Minute).
					WithPolling(5 * time.Second).
					Should(BeTrue())
			})
			It("should serve traffic from ingress-nginx", func() {
				expectEndpointServesTraffic(nginxUrl)
			})
			It("should serve traffic from envoy gateway", func() {
				expectEndpointServesTraffic(envoyUrl)
			})
			It("should serve traffic from kong", func() {
				expectEndpointServesTraffic(kongUrl)
			})
			It("should run k6 load tests successfully", func() {
				k6Namespace := getK6Namespace()
				baseDomain := getWorkloadClusterBaseDomain()
				testRunName := fmt.Sprintf("e2e-load-test-%s", state.GetCluster().Name)
				configMapName := fmt.Sprintf("e2e-load-test-scenario-%s", state.GetCluster().Name)

				// Clean up any stale resources from a previous interrupted run
				cleanupK6Resources(testRunName, configMapName, k6Namespace)

				By("Creating test scenario ConfigMap on k6 cluster")
				cm := buildScenarioConfigMap(configMapName, k6Namespace)
				err := getK6Client().Create(state.GetContext(), cm)
				Expect(err).NotTo(HaveOccurred())

				By("Creating TestRun on k6 cluster")
				testRun := buildTestRunUnstructured(testRunName, k6Namespace, configMapName, baseDomain)
				err = getK6Client().Create(state.GetContext(), testRun)
				Expect(err).NotTo(HaveOccurred())

				By("Waiting for TestRun to complete")
				Eventually(func() (string, error) {
					return getTestRunStage(testRunName, k6Namespace)
				}).
					WithTimeout(70 * time.Minute).
					WithPolling(30 * time.Second).
					Should(BeElementOf("finished", "error"))

				By("Asserting TestRun succeeded")
				assertTestRunSuccess(testRunName, k6Namespace)

				By("Cleaning up k6 resources")
				cleanupK6Resources(testRunName, configMapName, k6Namespace)
			})
		}).
		AfterSuite(func() {
			kubeconfigPath := getK6KubeconfigPath()
			if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
				return
			}
			k6Namespace := getK6Namespace()
			testRunName := fmt.Sprintf("e2e-load-test-%s", state.GetCluster().Name)
			configMapName := fmt.Sprintf("e2e-load-test-scenario-%s", state.GetCluster().Name)
			cleanupK6Resources(testRunName, configMapName, k6Namespace)
		}).
		Run(t, "Basic Test")
}
