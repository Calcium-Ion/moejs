package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Host is the deterministic stand-in for new-api's `utils` globals (see
// pkg/jsplugin/utils.go). Every helper is pure Go so all four engines expose
// byte-identical behaviour; time and identity are fixed so recorded fixtures
// replay exactly.
type Host struct {
	UnixNow int64
	UUID    string
}

// DefaultHost is the clock and identity the recorded fixtures were made with.
var DefaultHost = Host{UnixNow: 1700000000, UUID: "00000000-0000-4000-8000-000000000000"}

// Capabilities the host advertises through utils.hasCapability.
var capabilities = map[string]bool{"json-clone@1": true, "submit-sse-delta@1": true}

func (Host) HasCapability(name string) bool { return capabilities[name] }

func (Host) HmacSHA256(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func (Host) Base64(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }

func (Host) Base64URL(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func (Host) Base64URLDecode(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// JWTSignHS256 is a minimal HS256 signer equivalent to golang-jwt's
// jwt.NewWithClaims(jwt.SigningMethodHS256, MapClaims(claims)).SignedString:
// base64url(header).base64url(claims).base64url(HMAC-SHA256(secret)).
func (Host) JWTSignHS256(claims map[string]any, secret string) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VolcSignV4 is stubbed: the real signer needs network-shaped inputs and its
// output is opaque to the plugins, which only spread the returned headers.
func (Host) VolcSignV4(map[string]any) (map[string]string, error) {
	return map[string]string{
		"Authorization":    "HMAC-SHA256 Credential=AKSTUB/20231114/cn-north-1/cv/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=stub",
		"X-Date":           "20231114T221320Z",
		"X-Content-Sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}, nil
}

// JSONClone deep-copies a JSON-shaped Go value through encoding/json, which
// is the observable behaviour of new-api's utils.json.clone.
func (Host) JSONClone(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("json.clone: %w", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("json.clone: %w", err)
	}
	return out, nil
}

var errJSONCloneUndefined = errors.New("json.clone requires a JSON value")
