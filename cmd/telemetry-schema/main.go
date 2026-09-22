// Command telemetry-schema derives a JSON Schema from a Go telemetry struct
// and prints it, ready to paste into a group's telemetry_schema field in the
// admin UI. With -push, it writes the schema straight to the group's
// PocketBase record instead.
//
// The group's telemetry_schema is the authoritative validation contract
// (see specs/mqtt_telemetry.specs.md); this tool exists so authoring that
// contract can still start from a Go struct rather than a hand-written JSON
// Schema textarea.
//
// Go generics are resolved at compile time, so this command cannot take an
// arbitrary type name as a runtime flag. ExampleTelemetry below is the
// customization point: copy this file, replace ExampleTelemetry with your
// own agent's telemetry struct, and run it.
//
//	go run ./cmd/telemetry-schema
//	go run ./cmd/telemetry-schema -push -group "Warehouse Fleet" \
//		-pb-url http://127.0.0.1:8090 -identity admin -password changeme123
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"vantageos-core/pkg/agentsdk"
)

// ExampleTelemetry is the customization point described above.
type ExampleTelemetry struct {
	BatteryPercent float64 `json:"battery_percent"`
	PoseX          float64 `json:"x"`
	PoseY          float64 `json:"y"`
	Status         string  `json:"status"`
}

func main() {
	push := flag.Bool("push", false, "write the derived schema to the group's telemetry_schema field")
	pbURL := flag.String("pb-url", "http://127.0.0.1:8090", "PocketBase base URL")
	group := flag.String("group", "", "agent_groups record id, or its name (with -push)")
	identity := flag.String("identity", "", "username or email to authenticate with (with -push)")
	password := flag.String("password", "", "password to authenticate with (with -push)")
	token := flag.String("token", "", "an existing PocketBase auth token, instead of -identity/-password (with -push)")
	flag.Parse()

	schema, err := agentsdk.DeriveTelemetrySchema[ExampleTelemetry]()
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry-schema: %v\n", err)
		os.Exit(1)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, schema.Schema, "", "  "); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry-schema: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("schema_hash: %s\n\n%s\n", schema.Hash, pretty.String())

	if !*push {
		return
	}

	if *group == "" {
		fmt.Fprintln(os.Stderr, "telemetry-schema: -push requires -group")
		os.Exit(1)
	}

	c := &pbClient{baseURL: strings.TrimRight(*pbURL, "/"), token: *token}
	if c.token == "" {
		if *identity == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "telemetry-schema: -push requires -token, or both -identity and -password")
			os.Exit(1)
		}
		if err := c.authenticate(*identity, *password); err != nil {
			fmt.Fprintf(os.Stderr, "telemetry-schema: authenticate: %v\n", err)
			os.Exit(1)
		}
	}

	groupID, err := c.resolveGroupID(*group)
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry-schema: resolve group %q: %v\n", *group, err)
		os.Exit(1)
	}

	if err := c.pushSchema(groupID, schema.Schema); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry-schema: push: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("pushed schema_hash %s to agent_groups/%s\n", schema.Hash, groupID)
}

// pbClient is a minimal PocketBase REST client covering only what this
// command needs — authenticating as an existing user and patching one
// agent_groups record. It intentionally does not depend on a PocketBase SDK.
type pbClient struct {
	baseURL string
	token   string
}

func (c *pbClient) authenticate(identity, password string) error {
	body, err := json.Marshal(map[string]string{"identity": identity, "password": password})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth-with-password: %s: %s", resp.Status, respBody)
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}
	if parsed.Token == "" {
		return fmt.Errorf("auth response carried no token")
	}
	c.token = parsed.Token
	return nil
}

// resolveGroupID accepts either an agent_groups record ID directly, or a
// group name to look up — a CLI user is more likely to know the group's
// display name than its opaque record ID.
func (c *pbClient) resolveGroupID(idOrName string) (string, error) {
	if _, err := c.get("/api/collections/agent_groups/records/" + url.PathEscape(idOrName)); err == nil {
		return idOrName, nil
	}

	filter := fmt.Sprintf("name='%s'", strings.ReplaceAll(idOrName, "'", `\'`))
	q := url.Values{"filter": {filter}}
	respBody, err := c.get("/api/collections/agent_groups/records?" + q.Encode())
	if err != nil {
		return "", err
	}

	var parsed struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse list response: %w", err)
	}
	switch len(parsed.Items) {
	case 0:
		return "", fmt.Errorf("no agent group found with id or name %q", idOrName)
	case 1:
		return parsed.Items[0].ID, nil
	default:
		return "", fmt.Errorf("%d agent groups named %q — pass the record id instead", len(parsed.Items), idOrName)
	}
}

func (c *pbClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", resp.Status, body)
	}
	return body, nil
}

// pushSchema PATCHes telemetry_schema as a raw JSON value rather than a
// string — PocketBase's JSONField stores structured JSON, and encoding the
// schema as a quoted string field would store an escaped JSON string inside
// a JSON value instead of the schema object itself.
func (c *pbClient) pushSchema(groupID string, schema json.RawMessage) error {
	payload, err := json.Marshal(map[string]json.RawMessage{"telemetry_schema": schema})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPatch, c.baseURL+"/api/collections/agent_groups/records/"+url.PathEscape(groupID), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, body)
	}
	return nil
}
