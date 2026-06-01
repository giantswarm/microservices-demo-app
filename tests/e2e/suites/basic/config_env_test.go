package basic

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var configEnvOnce sync.Once

// infraConfigKeys are config.env entries that identify the manual
// load-testing pipeline's specific, long-lived cluster (management cluster,
// workload cluster name, base domain, kube contexts). The e2e suite
// provisions its own ephemeral workload cluster and discovers these at
// runtime, so importing them from config.env would point the suite at the
// wrong installation — e.g. a base domain with no matching Route 53 hosted
// zone, which leaves every Let's Encrypt certificate stuck on DNS-01.
var infraConfigKeys = map[string]bool{
	"WC":          true,
	"MC":          true,
	"BASE_DOMAIN": true,
	"MC_CONTEXT":  true,
	"K6_CONTEXT":  true,
}

// loadConfigEnv seeds the process environment from
// envoy-loadtesting/config.env on first call, so the e2e suite shares
// app-tuning defaults (PUBLIC_ENDPOINTS, HPA_*, k6 knobs) with the manual
// load-testing pipeline. Infrastructure-identity keys (see infraConfigKeys)
// are deliberately skipped — those are specific to the manual pipeline's
// cluster and must be discovered at runtime for the ephemeral e2e cluster.
// Real env vars already set by the Tekton pipeline (or a local override)
// always win — this only fills in what's missing.
//
// File format mirrors `set -a; source config.env; set +a`: KEY=value lines,
// '#' line comments, and ' #' inline comments stripped from values. Quotes
// are not interpreted (none of the keys in config.env use them).
func loadConfigEnv() {
	configEnvOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			return
		}
		path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "envoy-loadtesting", "config.env")
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			eq := strings.IndexByte(line, '=')
			if eq <= 0 {
				continue
			}
			key := strings.TrimSpace(line[:eq])
			if infraConfigKeys[key] {
				continue
			}
			value := line[eq+1:]
			if i := strings.Index(value, " #"); i >= 0 {
				value = value[:i]
			}
			value = strings.TrimSpace(value)
			if _, set := os.LookupEnv(key); set {
				continue
			}
			_ = os.Setenv(key, value)
		}
	})
}
