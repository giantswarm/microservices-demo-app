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
