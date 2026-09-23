package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

type AuthService struct {
	mu            sync.RWMutex
	regTokens     map[string]string
	authTokens    map[string]string
	mqttPasswords map[string]string
}

func NewAuthService() AuthService {
	return AuthService{
		regTokens:     make(map[string]string),
		authTokens:    make(map[string]string),
		mqttPasswords: make(map[string]string),
	}
}

func (a *AuthService) AddRegToken(id, regToken string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regTokens[regToken] = id
}

// ResetRegTokens atomically replaces the whole registration-token table with
// regToken→id pairs. Already-issued auth tokens are left untouched, so agents
// with a live session stay connected until they reconnect. Used to apply live
// edits to the agents/missions config collections without a restart.
func (a *AuthService) ResetRegTokens(regTokens map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[string]string, len(regTokens))
	for regToken, id := range regTokens {
		next[regToken] = id
	}
	a.regTokens = next
}

// ExchangeRegToken will exchange the registration token for id and auth token
func (a *AuthService) ExchangeRegToken(regToken string) (string, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	id, ok := a.regTokens[regToken]
	if !ok {
		return "", "", errors.New("invalid registration token")
	}

	authToken, err := a.generateRandomHex(32)
	if err != nil {
		return "", "", err
	}
	a.authTokens[id] = authToken

	return id, authToken, nil
}

func (a *AuthService) Authenticate(id string, authToken string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	token, ok := a.authTokens[id]
	if !ok {
		return false
	}

	return token == authToken
}

// IssueMQTTCredentials mints a fresh random broker password for id,
// overwriting any previously issued one -- same re-issue-on-every-call
// behavior as ExchangeRegToken's authToken, so a stale password stops
// working once the agent re-registers (spec Step 15). The broker username
// is always id itself, never generated here: that is what makes the %u ACL
// substitution in specs/mqtt_telemetry.specs.md resolve.
func (a *AuthService) IssueMQTTCredentials(id string) (password string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	password, err = a.generateRandomHex(24)
	if err != nil {
		return "", err
	}
	// Stored (not just returned) so a future admin-facing lookup or
	// broker-provisioning integration has somewhere to read the
	// currently-issued password from -- core itself never authenticates
	// MQTT connections (the broker's own auth backend does, per the Ops
	// section), so nothing in this codebase reads mqttPasswords back yet.
	a.mqttPasswords[id] = password
	return password, nil
}

func (a *AuthService) generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
