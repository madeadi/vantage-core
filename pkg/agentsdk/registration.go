package agentsdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterResponse is core's response to POST /agents/register. Shared with
// mission registration (POST /missions/register); the broker fields are
// only populated for an agent registering against a core instance with MQTT
// enabled (see specs/mqtt_telemetry.specs.md Step 15) -- omitempty so a
// mission's response, or a gRPC-only agent's, carries no trace of them.
type RegisterResponse struct {
	Token    string `json:"token"`
	GRPCAddr string `json:"grpc_addr"`

	// AgentID is the id core resolved the device key to -- the same
	// server-side resolution ExchangeRegToken already does for the gRPC
	// path, just also handed back here so an MQTT agent can bootstrap from
	// nothing but a device key, without needing to already know (and
	// possibly mismatch) its own id.
	AgentID string `json:"agent_id,omitempty"`

	// MQTT broker credentials (spec Step 15). BrokerURL/TopicPrefix are the
	// same for every agent; Username is always AgentID (that is what makes
	// the %u ACL substitution in specs/mqtt_telemetry.specs.md resolve);
	// Password is minted fresh on every registration, mirroring Token's own
	// re-issuance-on-every-call behavior.
	BrokerURL   string `json:"broker_url,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	TopicPrefix string `json:"topic_prefix,omitempty"`
}

// Register exchanges deviceKey for a RegisterResponse by calling
// POST <coreURL>/agents/register -- the same HTTP call
// pkg/agentsdk/server.Server.Connect makes for the gRPC path, factored out
// here so the MQTT path (NewAgent) can bootstrap from a device key too
// instead of requiring the caller to already know its broker credentials.
func Register(coreURL, deviceKey string) (*RegisterResponse, error) {
	req, err := http.NewRequest(http.MethodPost, coreURL+"/agents/register", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+deviceKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agentsdk: registration rejected: status %d", resp.StatusCode)
	}

	var regResp RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, err
	}
	return &regResp, nil
}
