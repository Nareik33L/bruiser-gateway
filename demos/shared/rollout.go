package shared

import "hash/fnv"

// Enforced reports whether a customer is in the enforcement bucket.
// bucket = fnv1a32(customerID) mod 100; enforced when bucket < percent.
func Enforced(customerID string, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(customerID))
	return int(h.Sum32()%100) < percent
}
