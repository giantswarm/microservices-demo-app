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

// loadConfigEnv seeds the process environment from
// envoy-loadtesting/config.env on first call, so the e2e suite shares
// defaults with the manual load-testing pipeline. Real env vars already set
// by the Tekton pipeline (or a local override) always win — this only fills
// in what's missing.
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
