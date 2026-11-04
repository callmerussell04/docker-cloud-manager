// Package testenv decides whether an environment-gated test runs, skips or
// fails. Every test that depends on an integration service or host capability
// reads its gate through this package.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// RequireIntegration names the flag that turns a missing integration
// environment from a skip into a failure. make test sets it to "1".
const RequireIntegration = "DCM_TEST_REQUIRE_INTEGRATION"

// exempt lists the gates that skip even when integration is required. The
// host-firewall network-policy test changes host iptables, so it stays a
// manual deployment check.
var exempt = map[string]bool{
	"CORE_TEST_NETWORK_POLICY": true,
}

// Lookup returns the trimmed value of key. When it is empty, t skips, or fails
// when integration is required and key is not exempt.
func Lookup(t testing.TB, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		unavailable(t, key, "set "+key+" to run this test")
	}
	return value
}

// Enabled lets t run only when key is "1". Otherwise t skips, or fails when
// integration is required and key is not exempt.
func Enabled(t testing.TB, key string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(key)) != "1" {
		unavailable(t, key, "set "+key+"=1 to run this test")
	}
}

func unavailable(t testing.TB, key, hint string) {
	t.Helper()
	if os.Getenv(RequireIntegration) == "1" && !exempt[key] {
		t.Fatal(RequireIntegration + "=1 requires " + key + ": " + hint)
		return
	}
	t.Skip(hint)
}
