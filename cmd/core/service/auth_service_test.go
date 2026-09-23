package service

import "testing"

func TestIssueMQTTCredentialsReissuesOnEveryCall(t *testing.T) {
	a := NewAuthService()

	first, err := a.IssueMQTTCredentials("bot-1")
	if err != nil {
		t.Fatalf("IssueMQTTCredentials: %v", err)
	}
	if first == "" {
		t.Fatal("IssueMQTTCredentials returned an empty password")
	}

	second, err := a.IssueMQTTCredentials("bot-1")
	if err != nil {
		t.Fatalf("IssueMQTTCredentials (second call): %v", err)
	}
	if second == first {
		t.Error("second call returned the same password as the first -- want a fresh one on every call, same as ExchangeRegToken's authToken")
	}
}

func TestIssueMQTTCredentialsPerAgentIndependent(t *testing.T) {
	a := NewAuthService()

	p1, err := a.IssueMQTTCredentials("bot-1")
	if err != nil {
		t.Fatalf("IssueMQTTCredentials(bot-1): %v", err)
	}
	p2, err := a.IssueMQTTCredentials("bot-2")
	if err != nil {
		t.Fatalf("IssueMQTTCredentials(bot-2): %v", err)
	}
	if p1 == p2 {
		t.Error("two different agents got the same password")
	}
}
