package lease

// Default V1 precedence (ADR-024): a human-present browser outranks an agent.
// Equal ranks cannot preempt; they need a cooperative handoff. Full policy
// lists land in M4.

func Rank(principalType string, order []string) int {
	if len(order) == 0 {
		order = []string{"browser", "agent"}
	}
	for i, t := range order {
		if t == principalType {
			return len(order) - i
		}
	}
	return 0
}

func CanPreempt(caller, holder Principal) bool {
	return CanPreemptRanked(caller, holder, nil)
}

func CanPreemptRanked(caller, holder Principal, order []string) bool {
	return Rank(caller.Type, order) > Rank(holder.Type, order)
}

func SamePrincipal(a, b Principal) bool {
	return a.Type == b.Type && a.ID == b.ID
}
