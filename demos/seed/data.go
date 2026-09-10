// Package seed generates fictional Harchester United supporters and fixtures.
//
// DEMO ONLY. Passwords are plaintext by design so the agent launcher can use
// deterministic credentials. This schema must never be copied into production.
package seed

import (
	"fmt"
	"math/rand"
)

const (
	DefaultPassword  = "password"
	SupporterCount   = 10000
	FirstMembership  = 1000001
	AliceMembership  = "1001234"
	IneligibleMember = "1000002"
	HeadlineEventID  = "hfc-ars"
	HeadlineSeats    = 500
	ClubName         = "Harchester United"
	OpponentDefault  = "Arsenal"
	Stadium          = "Dragon's Lair"
	KickoffDisplay   = "Saturday 15:00"
)

type Supporter struct {
	CustomerID         string
	MembershipNumber   string
	FirstName          string
	LastName           string
	Email              string
	Password           string
	MembershipTier     string
	LoyaltyPoints      int
	SeasonTicket       bool
	EligibleForArsenal bool
}

type Event struct {
	ID          string
	Name        string
	Opponent    string
	Venue       string
	Kickoff     string
	Competition string
	Seats       int
	MembersOnly bool
	OnSale      bool
	SoldOut     bool
	Home        bool
}

type Block struct {
	ID         string
	EventID    string
	Name       string
	PriceBand  string
	PricePence int
	Capacity   int
}

func GenerateSupporters(n int) []Supporter {
	if n <= 0 {
		n = SupporterCount
	}
	rng := rand.New(rand.NewSource(33))
	out := make([]Supporter, n)
	for i := 0; i < n; i++ {
		num := FirstMembership + i
		mem := fmt.Sprintf("%07d", num)
		fn := firstNames[i%len(firstNames)]
		ln := lastNames[(i*7)%len(lastNames)]
		tier := tierFor(i)
		out[i] = Supporter{
			CustomerID:         fmt.Sprintf("cus_%07d", num),
			MembershipNumber:   mem,
			FirstName:          fn,
			LastName:           ln,
			Email:              fmt.Sprintf("%s.%s.%s@example.com", fn, ln, mem),
			Password:           DefaultPassword,
			MembershipTier:     tier,
			LoyaltyPoints:      100 + rng.Intn(9000),
			SeasonTicket:       tier == "Season Ticket" || tier == "Gold",
			EligibleForArsenal: i%20 != 0, // ~95%? wait 19/20 = 95%. Plan said ~85%. i%7 != 0 is ~86%
		}
		out[i].EligibleForArsenal = i%7 != 0
	}
	overlayNamed(out)
	return out
}

func overlayNamed(all []Supporter) {
	for i := range all {
		switch all[i].MembershipNumber {
		case AliceMembership:
			all[i].FirstName = "Alice"
			all[i].LastName = "Okafor"
			all[i].Email = "alice.okafor@example.com"
			all[i].MembershipTier = "Gold"
			all[i].SeasonTicket = true
			all[i].EligibleForArsenal = true
			all[i].LoyaltyPoints = 4820
		case IneligibleMember:
			all[i].FirstName = "Sam"
			all[i].LastName = "Quinn"
			all[i].Email = "sam.quinn@example.com"
			all[i].MembershipTier = "Junior"
			all[i].SeasonTicket = false
			all[i].EligibleForArsenal = false
			all[i].LoyaltyPoints = 120
		}
	}
}

func tierFor(i int) string {
	switch i % 10 {
	case 0:
		return "Junior"
	case 1, 2, 3:
		return "Bronze"
	case 4, 5, 6:
		return "Silver"
	case 7, 8:
		return "Gold"
	default:
		return "Season Ticket"
	}
}

func Events(opponent string) []Event {
	if opponent == "" {
		opponent = OpponentDefault
	}
	return []Event{
		{
			ID: HeadlineEventID, Name: ClubName + " vs " + opponent, Opponent: opponent,
			Venue: Stadium, Kickoff: KickoffDisplay, Competition: "Premier League",
			Seats: HeadlineSeats, MembersOnly: true, OnSale: true, Home: true,
		},
		{
			ID: "hfc-whu", Name: ClubName + " vs West Ham United", Opponent: "West Ham United",
			Venue: Stadium, Kickoff: "Saturday 12:30", Competition: "Premier League",
			Seats: 800, MembersOnly: true, OnSale: false, Home: true,
		},
		{
			ID: "nfo-hfc", Name: "Nottingham Forest vs " + ClubName, Opponent: "Nottingham Forest",
			Venue: "The City Ground", Kickoff: "Sunday 14:00", Competition: "Premier League",
			Seats: 2400, MembersOnly: false, OnSale: false, Home: false,
		},
		{
			ID: "hfc-cup", Name: ClubName + " vs Darlington", Opponent: "Darlington",
			Venue: Stadium, Kickoff: "Wednesday 19:45", Competition: "FA Cup",
			Seats: 500, MembersOnly: false, OnSale: false, SoldOut: true, Home: true,
		},
	}
}

func Blocks(eventID string) []Block {
	return []Block{
		{ID: "north-lower", EventID: eventID, Name: "North Stand Lower", PriceBand: "Category 1", PricePence: 6500, Capacity: 140},
		{ID: "east-family", EventID: eventID, Name: "East Stand Family", PriceBand: "Category 2", PricePence: 4500, Capacity: 120},
		{ID: "south-upper", EventID: eventID, Name: "South Stand Upper", PriceBand: "Category 2", PricePence: 4000, Capacity: 160},
		{ID: "west-paddock", EventID: eventID, Name: "West Paddock", PriceBand: "Category 3", PricePence: 3000, Capacity: 80},
	}
}

func Find(all []Supporter, membership string) (Supporter, bool) {
	for _, s := range all {
		if s.MembershipNumber == membership {
			return s, true
		}
	}
	return Supporter{}, false
}

var firstNames = []string{
	"James", "Olivia", "Amelia", "Noah", "Isla", "George", "Ava", "Leo", "Mia", "Arthur",
	"Sophia", "Harry", "Grace", "Oscar", "Lily", "Charlie", "Emily", "Jack", "Freya", "Muhammad",
	"Ivy", "Theo", "Poppy", "Archie", "Willow", "Henry", "Evie", "Thomas", "Ella", "Alexander",
	"Sofia", "William", "Elsie", "Joshua", "Rosie", "Oliver", "Daisy", "Daniel", "Charlotte", "Mohammed",
	"Alice", "Samuel", "Phoebe", "Joseph", "Harper", "Edward", "Ruby", "Adam", "Esme", "Isaac",
}

var lastNames = []string{
	"Smith", "Jones", "Williams", "Brown", "Taylor", "Wilson", "Johnson", "Davies", "Patel", "Wright",
	"Robinson", "Thompson", "Evans", "Walker", "White", "Roberts", "Green", "Hall", "Thomas", "Clarke",
	"Jackson", "Wood", "Harris", "Edwards", "Cooper", "Harrison", "Martin", "Hughes", "Ward", "Morgan",
	"Bailey", "Parker", "Bell", "Collins", "Price", "Bennett", "Young", "Griffiths", "Mitchell", "Kelly",
	"Cook", "Phillips", "Campbell", "Allen", "West", "Scott", "Murphy", "Richardson", "Khan", "Ahmed",
}
