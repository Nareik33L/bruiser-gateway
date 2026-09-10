package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// FailOpenReasons lists document-level fail-open behaviour. Per-rule
// control: none (discovery/search) is intentional and is not listed.
func FailOpenReasons(d Document) []string {
	var reasons []string
	if strings.EqualFold(strings.TrimSpace(d.Fallback), FallbackAllowUncontrolled) {
		reasons = append(reasons, "fallback: allow-uncontrolled")
	}
	return reasons
}

// CheckProductionSafety rejects a fail-open compiled policy in production
// unless BRUISER_ALLOW_UNSAFE_MODES acknowledged the risk.
func CheckProductionSafety(d Document, allowUnsafe bool) error {
	reasons := FailOpenReasons(d)
	if len(reasons) == 0 {
		return nil
	}
	if allowUnsafe {
		return nil
	}
	return fmt.Errorf("production policy is fail-open (%s); set BRUISER_ALLOW_UNSAFE_MODES=1 to acknowledge", strings.Join(reasons, ", "))
}

// HashYAML is the canonical policy-document digest used in admin audit.
func HashYAML(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
