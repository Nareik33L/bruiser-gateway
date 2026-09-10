package ops

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// Typical merchant rollout steps. Any 0–100 value is valid.
var RampPresets = []int{0, 10, 25, 50, 75, 100}

// RampScope limits who is eligible for active enforcement.
// Empty lists match any value. Specified dimensions are AND;
// values inside a dimension are OR. Unscoped traffic is still
// observed and recorded; it is not actively enforced.
type RampScope struct {
	Events       []string `json:"events,omitempty"`
	Routes       []string `json:"routes,omitempty"`
	Pools        []string `json:"pools,omitempty"`
	Cohorts      []string `json:"cohorts,omitempty"`
	Environments []string `json:"environments,omitempty"`
	Policies     []string `json:"policies,omitempty"`
}

type RampInput struct {
	CustomerID string
	Resource   string
	Action     string
	RuleName   string
	EventID    string
	Path       string
	Pool       string
	Cohort     string
	Env        string
}

func (s RampScope) Empty() bool {
	return len(s.Events)+len(s.Routes)+len(s.Pools)+len(s.Cohorts)+len(s.Environments)+len(s.Policies) == 0
}

func (s RampScope) Match(in RampInput) bool {
	if s.Empty() {
		return true
	}
	if len(s.Events) > 0 && !matchAny(s.Events, in.EventID, in.Resource) {
		return false
	}
	if len(s.Routes) > 0 && !matchAny(s.Routes, in.Action, in.Path) {
		return false
	}
	if len(s.Pools) > 0 && !matchAny(s.Pools, in.Pool, in.Resource) {
		return false
	}
	if len(s.Cohorts) > 0 && !matchAny(s.Cohorts, in.Cohort, in.CustomerID) {
		return false
	}
	if len(s.Environments) > 0 && !matchAny(s.Environments, in.Env) {
		return false
	}
	if len(s.Policies) > 0 && !matchAny(s.Policies, in.RuleName) {
		return false
	}
	return true
}

// Bucket is a stable 0–99 assignment for a customer. The same customer
// always lands in the same bucket for a given salt so agents do not flip
// between enforced and unenforced behaviour.
func Bucket(customerID, salt string) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(salt))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(customerID))
	return int(h.Sum64() % 100)
}

func InRamp(customerID string, percent int, salt string) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	return Bucket(customerID, salt) < percent
}

func ClampPercent(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func matchAny(list []string, vals ...string) bool {
	for _, want := range list {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		for _, got := range vals {
			if matchOne(want, got) {
				return true
			}
		}
	}
	return false
}

func matchOne(want, got string) bool {
	if got == "" {
		return false
	}
	if strings.EqualFold(want, got) {
		return true
	}
	if strings.HasSuffix(want, "*") {
		prefix := strings.TrimSuffix(want, "*")
		return strings.HasPrefix(strings.ToLower(got), strings.ToLower(prefix))
	}
	return strings.Contains(strings.ToLower(got), strings.ToLower(want))
}

func ParsePercent(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return ClampPercent(n), true
}
