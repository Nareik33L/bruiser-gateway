package seed_test

import (
	"testing"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func TestGenerateSupporters(t *testing.T) {
	all := seed.GenerateSupporters(seed.SupporterCount)
	if len(all) != seed.SupporterCount {
		t.Fatalf("got %d", len(all))
	}
	alice, ok := seed.Find(all, seed.AliceMembership)
	if !ok || alice.FirstName != "Alice" || !alice.EligibleForArsenal || alice.Password != seed.DefaultPassword {
		t.Fatalf("alice %+v ok=%v", alice, ok)
	}
	sam, ok := seed.Find(all, seed.IneligibleMember)
	if !ok || sam.EligibleForArsenal || sam.MembershipTier != "Junior" {
		t.Fatalf("sam %+v", sam)
	}
	seen := map[string]bool{}
	for _, s := range all {
		if s.Password != seed.DefaultPassword {
			t.Fatalf("password %s", s.Password)
		}
		if seen[s.MembershipNumber] {
			t.Fatalf("dup %s", s.MembershipNumber)
		}
		seen[s.MembershipNumber] = true
	}
}

func TestTenMemberships(t *testing.T) {
	ids := seed.TenMemberships()
	if len(ids) != seed.TenCustomerCount {
		t.Fatalf("got %d", len(ids))
	}
	if ids[0] != seed.AliceMembership {
		t.Fatalf("alice first, got %s", ids[0])
	}
	all := seed.GenerateSupporters(2000)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("dup %s", id)
		}
		seen[id] = true
		s, ok := seed.Find(all, id)
		if !ok || !s.EligibleForArsenal {
			t.Fatalf("not an eligible seeded member: %s ok=%v", id, ok)
		}
		if id == seed.IneligibleMember {
			t.Fatal("Sam must not be in the 10×N preset")
		}
	}
}

func TestEligibleMembershipNumber(t *testing.T) {
	if seed.EligibleMembershipNumber(1000002) {
		t.Fatal("Sam must be ineligible")
	}
	if !seed.EligibleMembershipNumber(1001234) {
		t.Fatal("Alice must be eligible")
	}
	if seed.EligibleMembershipNumber(1000001) {
		t.Fatal("index 0 is ineligible")
	}
}

func TestHeadlineEventSeats(t *testing.T) {
	ev := seed.Events("")
	if ev[0].ID != seed.HeadlineEventID || ev[0].Seats != seed.HeadlineSeats {
		t.Fatalf("%+v", ev[0])
	}
	sum := 0
	for _, b := range seed.Blocks(seed.HeadlineEventID) {
		sum += b.Capacity
	}
	if sum != seed.HeadlineSeats {
		t.Fatalf("blocks %d want %d", sum, seed.HeadlineSeats)
	}
}
