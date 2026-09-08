package lease

// Default V1 precedence (ADR-024): a human-present browser outranks an agent.
// Equal ranks cannot preempt; they need a cooperative handoff. Full policy
// lists land in M4.

func Precedence(principalType string) int {
	switch principalType {
	case "browser":
		return 2
	case "agent":
		return 1
	default:
		return 0
	}
}

func CanPreempt(caller, holder Principal) bool {
	return Precedence(caller.Type) > Precedence(holder.Type)
}

func SamePrincipal(a, b Principal) bool {
	return a.Type == b.Type && a.ID == b.ID
}
