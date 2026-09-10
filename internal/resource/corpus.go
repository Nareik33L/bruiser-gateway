package resource

// VariantCorpusVersion identifies the nine-variant + Unicode set used by
// Authority Check and the failure-mode suite. Bump when the list changes.
const VariantCorpusVersion = "1"

// CosmeticVariants are the nine RC1 retest strings that minted nine ACTIVE
// domains for one customer. They must fold to one identifier.
func CosmeticVariants(base string) []string {
	return []string{
		base,
		base + ".",
		base + "-",
		base + "_",
		base + "!",
		base + "@",
		base + "   ",
		base + string(rune(0x2010)), // U+2010 hyphen
		base + string(rune(0x2013)), // U+2013 en dash
	}
}

// CupfinalCorpus is the Fix 1a resource-id corpus (`ticket:cupfinal` plus
// trailing punct and Unicode hyphens).
func CupfinalCorpus() []string {
	return CosmeticVariants("ticket:cupfinal")
}
