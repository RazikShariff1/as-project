// Package config reads the gateway's settings from the environment.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Backends maps the service names clients use in /api/{service}/... to the environment key holding each one's URL.
// The names are the services' own names, as in the reference gateway.
func Backends() map[string]string {
	return map[string]string{
		"account-svc":   "ACCOUNT_SVC_URL",
		"catalogue-svc": "CATALOGUE_SVC_URL",
		"inventory-svc": "INVENTORY_SVC_URL",
		"order-svc":     "ORDER_SVC_URL",
		"customer-svc":  "CUSTOMER_SVC_URL",
	}
}

// AccountService is the backend that authenticates and authorises every request on the team's /api realm.
const AccountService = "account-svc"

// CustomerService is the backend that authenticates shoppers on the customer-facing /store realm.
const CustomerService = "customer-svc"

// Settings are the gateway's runtime settings.
type Settings struct {
	Services       map[string]*url.URL
	ProxyTimeout   time.Duration
	MaxBodyBytes   int64
	LoginPerMinute int
	TrustForwarded bool
}

// Getter reads a setting with a default; GoFr's config satisfies it.
type Getter interface {
	GetOrDefault(key, def string) string
}

var defaultURLs = map[string]string{
	"ACCOUNT_SVC_URL":   "http://localhost:8083",
	"CATALOGUE_SVC_URL": "http://localhost:8081",
	"INVENTORY_SVC_URL": "http://localhost:8082",
	"ORDER_SVC_URL":     "http://localhost:8084",
	"CUSTOMER_SVC_URL":  "http://localhost:8085",
}

// Load reads and validates the settings.
func Load(g Getter) (Settings, error) {
	s := Settings{Services: map[string]*url.URL{}}

	for name, key := range Backends() {
		raw := g.GetOrDefault(key, defaultURLs[key])

		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return s, fmt.Errorf("%s must be a full URL such as http://host:port, got %q", key, raw)
		}

		s.Services[name] = u
	}

	secs, err := positive(g, "PROXY_TIMEOUT_SECONDS", "30")
	if err != nil {
		return s, err
	}

	maxBody, err := positive(g, "MAX_BODY_BYTES", "1048576")
	if err != nil {
		return s, err
	}

	rate, err := positive(g, "LOGIN_RATE_PER_MINUTE", "10")
	if err != nil {
		return s, err
	}

	trust, err := strconv.ParseBool(g.GetOrDefault("TRUST_FORWARDED_FOR", "false"))
	if err != nil {
		return s, fmt.Errorf("TRUST_FORWARDED_FOR must be true or false: %w", err)
	}

	s.ProxyTimeout, s.MaxBodyBytes, s.LoginPerMinute, s.TrustForwarded = time.Duration(secs)*time.Second, int64(maxBody), rate, trust

	return s, nil
}

func positive(g Getter, key, def string) (int, error) {
	n, err := strconv.Atoi(g.GetOrDefault(key, def))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive number", key)
	}

	return n, nil
}
