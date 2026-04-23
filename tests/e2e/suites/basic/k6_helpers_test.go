package basic

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/gomega"

	"github.com/giantswarm/clustertest/v4/pkg/client"
	"github.com/giantswarm/clustertest/v4/pkg/logger"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cr "sigs.k8s.io/controller-runtime/pkg/client"
)

// testRunGone is the sentinel stage returned by getTestRunStage when the
// TestRun CR no longer exists (typically because k6-operator cleaned it up
// after completion with cleanup: post).
const testRunGone = "gone"

const defaultK6KubeconfigPath = "/etc/k6-kubeconfig"

var k6Client *client.Client

func getK6KubeconfigPath() string {
	if p := os.Getenv("E2E_K6_KUBECONFIG"); p != "" {
		return p
	}
	return defaultK6KubeconfigPath
}

// getK6Client returns a cached Kubernetes client for the k6 cluster.
// It reads the kubeconfig from E2E_K6_KUBECONFIG (default: /etc/k6-kubeconfig).
func getK6Client() *client.Client {
	if k6Client != nil {
		return k6Client
	}

	kubeconfigPath := getK6KubeconfigPath()
	logger.Log("Creating k6 cluster client from kubeconfig at %s", kubeconfigPath)

	var err error
	k6Client, err = client.New(kubeconfigPath)
	Expect(err).NotTo(HaveOccurred(), "failed to create k6 cluster client from %s", kubeconfigPath)

	return k6Client
}

func getK6Namespace() string {
	ns := os.Getenv("E2E_K6_NAMESPACE")
	if ns == "" {
		return "gs-k6-operator"
	}
	return ns
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildScenarioConfigMap(name, namespace string) *corev1.ConfigMap {
	_, thisFile, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue(), "failed to resolve test file path")

	scenarioPath := filepath.Join(filepath.Dir(thisFile), "test_data", "test-scenario.js")
	content, err := os.ReadFile(scenarioPath)
	Expect(err).NotTo(HaveOccurred(), "failed to read test-scenario.js at %s", scenarioPath)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"test-scenario.js": string(content),
		},
	}
}

func securityContext() map[string]any {
	return map[string]any{
		"fsGroup":    int64(1000),
		"runAsGroup": int64(1000),
		"runAsNonRoot": true,
		"runAsUser":  int64(1000),
		"seccompProfile": map[string]any{
			"type": "RuntimeDefault",
		},
	}
}

func containerSecurityContext() map[string]any {
	return map[string]any{
		"runAsNonRoot":             true,
		"runAsUser":               int64(1000),
		"runAsGroup":              int64(1000),
		"readOnlyRootFilesystem":  false,
		"allowPrivilegeEscalation": false,
		"capabilities": map[string]any{
			"drop": []any{"ALL"},
		},
		"seccompProfile": map[string]any{
			"type": "RuntimeDefault",
		},
	}
}

func buildTestRunUnstructured(name, namespace, configMapName, baseDomain string) *unstructured.Unstructured {
	image := envOrDefault("K6_IMAGE", "gsoci.azurecr.io/giantswarm/k6:1.6.0")

	env := []any{
		map[string]any{"name": "ENDPOINTS", "value": envOrDefault("K6_ENDPOINTS", "10")},
		map[string]any{"name": "BASE_DOMAIN", "value": baseDomain},
		map[string]any{"name": "SCENARIO_DURATION_SECONDS", "value": envOrDefault("K6_SCENARIO_DURATION_SECONDS", "1200")},
		map[string]any{"name": "WAIT_BETWEEN_SCENARIOS", "value": envOrDefault("K6_WAIT_BETWEEN_SCENARIOS", "300")},
		map[string]any{"name": "ARRIVAL_RATE", "value": envOrDefault("K6_ARRIVAL_RATE", "26")},
		map[string]any{"name": "PRE_ALLOCATED_VUS", "value": envOrDefault("K6_PRE_ALLOCATED_VUS", "50")},
		map[string]any{"name": "MAX_VUS", "value": envOrDefault("K6_MAX_VUS", "150")},
		map[string]any{"name": "GRACEFUL_STOP", "value": envOrDefault("K6_GRACEFUL_STOP", "30s")},
		map[string]any{"name": "SLO_P95_LATENCY_MS", "value": envOrDefault("K6_SLO_P95_LATENCY_MS", "500")},
		map[string]any{"name": "SLO_P99_LATENCY_MS", "value": envOrDefault("K6_SLO_P99_LATENCY_MS", "1000")},
		map[string]any{"name": "SLO_ERROR_RATE", "value": envOrDefault("K6_SLO_ERROR_RATE", "0.001")},
		map[string]any{"name": "SLO_CHECKS_RATE", "value": envOrDefault("K6_SLO_CHECKS_RATE", "0.95")},
	}

	testRun := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "k6.io/v1alpha1",
			"kind":       "TestRun",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"parallelism": int64(4),
				"quiet":       "false",
				"separate":    false,
				"script": map[string]any{
					"configMap": map[string]any{
						"name": configMapName,
						"file": "test-scenario.js",
					},
				},
				"initializer": map[string]any{
					"image":                    image,
					"containerSecurityContext": containerSecurityContext(),
					"securityContext":          securityContext(),
				},
				"starter": map[string]any{
					"containerSecurityContext": containerSecurityContext(),
					"securityContext":          securityContext(),
				},
				"runner": map[string]any{
					"image":                    image,
					"env":                      env,
					"containerSecurityContext": containerSecurityContext(),
					"securityContext":          securityContext(),
				},
			},
		},
	}

	testRun.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k6.io",
		Version: "v1alpha1",
		Kind:    "TestRun",
	})

	return testRun
}

func getTestRunStage(name, namespace string) (string, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k6.io",
		Version: "v1alpha1",
		Kind:    "TestRun",
	})

	err := getK6Client().Get(context.Background(), cr.ObjectKey{Name: name, Namespace: namespace}, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Log("TestRun %s/%s no longer exists (cleaned up by operator)", namespace, name)
			return testRunGone, nil
		}
		return "", err
	}

	stage, found, err := unstructured.NestedString(obj.Object, "status", "stage")
	if err != nil || !found {
		logger.Log("TestRun %s/%s stage not yet populated", namespace, name)
		return "", nil
	}

	logger.Log("TestRun %s/%s stage: %s", namespace, name, stage)
	return stage, nil
}

// assertTestRunSuccess verifies the TestRun finished successfully. If the
// TestRun CR has been deleted (operator cleanup after completion), it falls
// back to fallbackStage — the last stage observed during polling — so we can
// still distinguish a clean finish from a deletion after an error.
func assertTestRunSuccess(name, namespace, fallbackStage string) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k6.io",
		Version: "v1alpha1",
		Kind:    "TestRun",
	})

	err := getK6Client().Get(context.Background(), cr.ObjectKey{Name: name, Namespace: namespace}, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Log("TestRun %s/%s was cleaned up; asserting on last observed stage: %q", namespace, name, fallbackStage)
			Expect(fallbackStage).To(Equal("finished"), "TestRun did not finish successfully before cleanup, last observed stage: %q", fallbackStage)
			return
		}
		Expect(err).NotTo(HaveOccurred(), "failed to get TestRun for assertion")
	}

	stage, _, _ := unstructured.NestedString(obj.Object, "status", "stage")
	Expect(stage).To(Equal("finished"), "TestRun did not finish successfully, stage: %s", stage)

	// Log conditions for diagnostics
	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found {
		for _, c := range conditions {
			if cond, ok := c.(map[string]any); ok {
				logger.Log("TestRun condition: type=%v status=%v reason=%v message=%v",
					cond["type"], cond["status"], cond["reason"], cond["message"])
			}
		}
	}
}

func cleanupK6Resources(testRunName, configMapName, namespace string) {
	ctx := context.Background()
	k6 := getK6Client()

	// Delete TestRun
	testRun := &unstructured.Unstructured{}
	testRun.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k6.io",
		Version: "v1alpha1",
		Kind:    "TestRun",
	})
	testRun.SetName(testRunName)
	testRun.SetNamespace(namespace)
	if err := k6.Delete(ctx, testRun); err != nil {
		logger.Log("Cleanup: failed to delete TestRun %s/%s (may not exist): %v", namespace, testRunName, err)
	}

	// Delete ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}
	if err := k6.Delete(ctx, cm); err != nil {
		logger.Log("Cleanup: failed to delete ConfigMap %s/%s (may not exist): %v", namespace, configMapName, err)
	}

	// Give the operator a moment to process the deletion
	time.Sleep(5 * time.Second)
}
