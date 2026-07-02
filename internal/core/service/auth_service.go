package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

type AuthService struct {
	mu         sync.RWMutex
	regTokens  map[string]string
	authTokens map[string]string
}

func NewAuthService() AuthService {
	return AuthService{
		regTokens:  make(map[string]string),
		authTokens: make(map[string]string),
	}
}

func (a *AuthService) AddRegToken(id, regToken string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.regTokens[regToken] = id
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

func (a *AuthService) generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
