package merchant

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	MerchantID  string   `yaml:"merchant_id"`
	Name        string   `yaml:"name"`
	ProfileName string   `yaml:"profile"`
	Assumptions string   `yaml:"assumptions"`
	Identity    Identity `yaml:"identity"`
	Routes      []Route  `yaml:"routes"`
	Policy      Policy   `yaml:"policy"`
}

type Identity struct {
	Extractor     string `yaml:"extractor"`
	Cookie        string `yaml:"cookie"`
	SubjectClaim  string `yaml:"subject_claim"`
	HMACSecretEnv string `yaml:"hmac_secret_env"`
}

type Route struct {
	Match          Match  `yaml:"match"`
	Action         string `yaml:"action"`
	Resource       string `yaml:"resource"`
	ResourceFrom   string `yaml:"resource_from"`
	ResourcePrefix string `yaml:"resource_prefix"`
}

type Match struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

func LoadFile(path string) (Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	return Parse(b)
}

func Parse(b []byte) (Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if p.MerchantID == "" {
		return p, fmt.Errorf("merchant_id required")
	}
	if p.Policy.MaxActive < 1 {
		p.Policy.MaxActive = 1
	}
	if p.Policy.RuleName == "" {
		p.Policy.RuleName = "purchase-per-event"
	}
	if p.Identity.SubjectClaim == "" {
		p.Identity.SubjectClaim = "sub"
	}
	return p, nil
}

// MatchRoute returns the first matching route and path params.
func (p Profile) MatchRoute(method, path string) (Route, map[string]string, bool) {
	method = strings.ToUpper(method)
	for _, r := range p.Routes {
		if strings.ToUpper(r.Match.Method) != method {
			continue
		}
		params, ok := matchPath(r.Match.Path, path)
		if ok {
			return r, params, true
		}
	}
	return Route{}, nil, false
}

func matchPath(pattern, path string) (map[string]string, bool) {
	pattern = strings.TrimSuffix(pattern, "/")
	path = strings.TrimSuffix(path, "/")
	pp := strings.Split(pattern, "/")
	sp := strings.Split(path, "/")
	if len(pp) != len(sp) {
		return nil, false
	}
	params := map[string]string{}
	for i := range pp {
		if strings.HasPrefix(pp[i], "{") && strings.HasSuffix(pp[i], "}") {
			params[strings.TrimSuffix(strings.TrimPrefix(pp[i], "{"), "}")] = sp[i]
			continue
		}
		if pp[i] != sp[i] {
			return nil, false
		}
	}
	return params, true
}

func (r Route) ResourceFor(params map[string]string, bodyEventID string) string {
	if r.ResourceFrom == "event_id" && bodyEventID != "" {
		return r.ResourcePrefix + bodyEventID
	}
	res := r.Resource
	for k, v := range params {
		res = strings.ReplaceAll(res, "{"+k+"}", v)
	}
	return res
}

// Controlled is true when the route maps onto a scarcity action Bruiser must admit.
func (r Route) Controlled() bool {
	return r.Resource != "" || r.ResourceFrom != ""
}

func Empty(merchantID string) Profile {
	p, err := Parse([]byte("merchant_id: " + merchantID + "\n"))
	if err != nil {
		panic(err)
	}
	return p
}
