package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type env map[string]string

func (e env) GetOrDefault(key, def string) string {
	if v, ok := e[key]; ok {
		return v
	}

	return def
}

func TestLoadDefaults(t *testing.T) {
	s, err := Load(env{})
	assert.NoError(t, err)
	assert.Equal(t, "localhost:8081", s.Services["catalogue-svc"].Host)
	assert.Equal(t, "localhost:8082", s.Services["inventory-svc"].Host)
	assert.Equal(t, "localhost:8083", s.Services["account-svc"].Host)
	assert.Equal(t, 30*time.Second, s.ProxyTimeout)
	assert.Equal(t, int64(1<<20), s.MaxBodyBytes)
	assert.Equal(t, 10, s.LoginPerMinute)
	assert.False(t, s.TrustForwarded, "X-Forwarded-For is not trusted unless asked for")
}

func TestLoadRejectsBadSettings(t *testing.T) {
	for name, e := range map[string]env{
		"service url without a scheme": {"CATALOGUE_SVC_URL": "localhost:8081"},
		"service url that is a word":   {"INVENTORY_SVC_URL": "inventory"},
		"zero timeout":                 {"PROXY_TIMEOUT_SECONDS": "0"},
		"non-numeric body limit":       {"MAX_BODY_BYTES": "lots"},
		"negative login rate":          {"LOGIN_RATE_PER_MINUTE": "-1"},
		"bad bool":                     {"TRUST_FORWARDED_FOR": "maybe"},
	} {
		_, err := Load(e)
		assert.Error(t, err, name)
	}
}
