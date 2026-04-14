package basic

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/giantswarm/apptest-framework/v4/pkg/state"
	"github.com/giantswarm/clustertest/v4/pkg/application"
	"github.com/giantswarm/clustertest/v4/pkg/logger"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getWorkloadClusterBaseDomain() string {
	values := &application.ClusterValues{}
	err := state.GetFramework().MC().GetHelmValues(state.GetCluster().Name, state.GetCluster().GetNamespace(), values)
	Expect(err).NotTo(HaveOccurred())

	if values.BaseDomain == "" {
		Fail("baseDomain field missing from cluster helm values")
	}

	return fmt.Sprintf("%s.%s", state.GetCluster().Name, values.BaseDomain)
}

func newHttpClientWithProxy() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Resolver: &net.Resolver{
					PreferGo: true,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						if os.Getenv("HTTP_PROXY") != "" {
							u, err := url.Parse(os.Getenv("HTTP_PROXY"))
							if err != nil {
								logger.Log("Error parsing HTTP_PROXY as a URL %s", os.Getenv("HTTP_PROXY"))
							} else {
								if addr == u.Host {
									// always use coredns for proxy address resolution.
									var d net.Dialer
									return d.Dial(network, address)
								}
							}
						}
						d := net.Dialer{
							Timeout: time.Millisecond * time.Duration(10000),
						}
						return d.DialContext(ctx, "udp", "8.8.4.4:53")
					},
				},
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}

	if os.Getenv("HTTP_PROXY") != "" {
		logger.Log("Detected need to use PROXY as HTTP_PROXY env var was set to %s", os.Getenv("HTTP_PROXY"))
		transport.Proxy = http.ProxyFromEnvironment
	}

	httpClient := &http.Client{
		Transport: transport,
	}
	return httpClient
}

func deploymentIsReady(namespace, name string) (bool, error) {
	wcClient, err := state.GetFramework().WC(state.GetCluster().Name)
	if err != nil {
		return false, err
	}

	logger.Log("Checking if deployment %s/%s is ready", namespace, name)
	deployment := appsv1.Deployment{}
	err = wcClient.Get(state.GetContext(), types.NamespacedName{Name: name, Namespace: namespace}, &deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Log("Deployment %s/%s not found yet", namespace, name)
			return false, nil
		}
		return false, err
	}

	if deployment.Spec.Replicas == nil {
		return false, nil
	}

	desired := *deployment.Spec.Replicas
	if desired > 0 && deployment.Status.ReadyReplicas == desired && deployment.Status.AvailableReplicas == desired {
		logger.Log("Deployment %s/%s is ready (%d/%d replicas)", namespace, name, deployment.Status.ReadyReplicas, desired)
		return true, nil
	}

	logger.Log("Deployment %s/%s not ready yet (ready: %d/%d, available: %d/%d)",
		namespace, name, deployment.Status.ReadyReplicas, desired, deployment.Status.AvailableReplicas, desired)
	return false, nil
}

func serviceHasLoadBalancer(namespace, name string) (bool, error) {
	wcClient, err := state.GetFramework().WC(state.GetCluster().Name)
	if err != nil {
		return false, err
	}

	logger.Log("Checking if service %s/%s has load balancer address", namespace, name)
	svc := corev1.Service{}
	err = wcClient.Get(state.GetContext(), types.NamespacedName{Name: name, Namespace: namespace}, &svc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Log("Service %s/%s not found yet", namespace, name)
			return false, nil
		}
		return false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) > 0 &&
		(svc.Status.LoadBalancer.Ingress[0].Hostname != "" || svc.Status.LoadBalancer.Ingress[0].IP != "") {
		logger.Log("LoadBalancer address found for service %s/%s: %s%s",
			namespace, name, svc.Status.LoadBalancer.Ingress[0].Hostname, svc.Status.LoadBalancer.Ingress[0].IP)
		return true, nil
	}

	return false, nil
}

func loadBalancerServiceReadyInNamespace(namespace string) (bool, error) {
	wcClient, err := state.GetFramework().WC(state.GetCluster().Name)
	if err != nil {
		return false, err
	}

	logger.Log("Checking for ready LoadBalancer services in namespace %s", namespace)
	services := &corev1.ServiceList{}
	err = wcClient.List(state.GetContext(), services, client.InNamespace(namespace))
	if err != nil {
		return false, err
	}

	for i := range services.Items {
		svc := &services.Items[i]
		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if len(svc.Status.LoadBalancer.Ingress) > 0 &&
				(svc.Status.LoadBalancer.Ingress[0].Hostname != "" || svc.Status.LoadBalancer.Ingress[0].IP != "") {
				logger.Log("LoadBalancer service %s/%s is ready with address: %s%s",
					namespace, svc.Name, svc.Status.LoadBalancer.Ingress[0].Hostname, svc.Status.LoadBalancer.Ingress[0].IP)
				return true, nil
			}
		}
	}

	logger.Log("No ready LoadBalancer service found in namespace %s", namespace)
	return false, nil
}
