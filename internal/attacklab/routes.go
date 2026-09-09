package attacklab

import "github.com/Nareik33L/bruiser-gateway/internal/merchant"

// Routes are appended to the live merchant profile so /v1/authorize
// admits sandbox drop traffic with the same lease core as production.
func Routes() []merchant.Route {
	return []merchant.Route{
		{Match: merchant.Match{Method: "GET", Path: "/s/{store}"}, Action: "search"},
		{Match: merchant.Match{Method: "GET", Path: "/s/{store}/product"}, Action: "search"},
		{Match: merchant.Match{Method: "POST", Path: "/s/{store}/checkout"}, Resource: "drop:{store}", Action: "purchase"},
		{Match: merchant.Match{Method: "POST", Path: "/s/{store}/drops/{wave}/checkout"}, Resource: "drop:{store}:{wave}", Action: "purchase"},
	}
}
