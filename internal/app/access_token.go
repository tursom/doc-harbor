package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	accessTokenCapabilityAIHistoryRead     = "ai.history.read"
	accessTokenCapabilityAIDiagnosticsRead = "ai.diagnostics.read"
	accessTokenDefaultTTL                  = time.Hour
	accessTokenMinTTL                      = 5 * time.Minute
	accessTokenMaxTTL                      = 24 * time.Hour
)

var allowedAccessTokenCapabilities = map[string]struct{}{
	accessTokenCapabilityAIHistoryRead:     {},
	accessTokenCapabilityAIDiagnosticsRead: {},
}

type accessTokenRequest struct {
	TTLSeconds   int              `json:"ttl_seconds"`
	Capabilities []string         `json:"capabilities"`
	Scope        accessTokenScope `json:"scope"`
}

type accessTokenScope struct {
	ViewerKey string `json:"viewer_key,omitempty"`
}

type accessTokenResponse struct {
	Token        string           `json:"token"`
	ExpiresAt    string           `json:"expires_at"`
	Capabilities []string         `json:"capabilities"`
	Scope        accessTokenScope `json:"scope"`
}

type accessTokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type accessTokenPayload struct {
	IssuedAt     int64            `json:"iat"`
	ExpiresAt    int64            `json:"exp"`
	Capabilities []string         `json:"capabilities"`
	Scope        accessTokenScope `json:"scope"`
	JTI          string           `json:"jti"`
}

func (s *Server) handleAccessTokens(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/tokens" {
		writeError(w, errNotFound("not found"))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req accessTokenRequest
	if err := decodeBody(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	resp, err := s.issueAccessToken(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAccessSubroutes(w http.ResponseWriter, r *http.Request) {
	payload, err := s.verifyAccessTokenBearer(r)
	if err != nil {
		writeError(w, err)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/access")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		writeError(w, errNotFound("not found"))
		return
	}
	parts := strings.Split(trimmed, "/")
	switch parts[0] {
	case "ai":
		if len(parts) >= 2 && parts[1] == "history" {
			s.handleAccessAIHistory(w, r, payload, parts[2:])
			return
		}
		if len(parts) >= 2 && parts[1] == "diagnostics" {
			s.handleAccessAIDiagnostics(w, r, payload, parts[2:])
			return
		}
	}
	writeError(w, errNotFound("not found"))
}

func (s *Server) accessTokenSigningKey() ([]byte, error) {
	keyPath := filepath.Join(s.cfg.DataDir, "secrets", "access-token.key")
	raw, err := os.ReadFile(keyPath)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != 32 {
			return nil, errUnavailable("access token signing key is invalid")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Server) issueAccessToken(req accessTokenRequest) (accessTokenResponse, error) {
	ttl := accessTokenDefaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if ttl < accessTokenMinTTL || ttl > accessTokenMaxTTL {
		return accessTokenResponse{}, errBadRequest("ttl_seconds must be between 300 and 86400")
	}
	capabilities, err := normalizeAccessTokenCapabilities(req.Capabilities)
	if err != nil {
		return accessTokenResponse{}, err
	}
	now := time.Now().UTC()
	scope := accessTokenScope{ViewerKey: strings.TrimSpace(req.Scope.ViewerKey)}
	payload := accessTokenPayload{
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
		Capabilities: capabilities,
		Scope:        scope,
	}
	token, err := s.signAccessToken(payload)
	if err != nil {
		return accessTokenResponse{}, err
	}
	return accessTokenResponse{
		Token:        token,
		ExpiresAt:    time.Unix(payload.ExpiresAt, 0).UTC().Format(timeLayout),
		Capabilities: capabilities,
		Scope:        scope,
	}, nil
}

func normalizeAccessTokenCapabilities(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := allowedAccessTokenCapabilities[value]; !ok {
			return nil, errBadRequest("unsupported capability: " + value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, errBadRequest("capabilities is required")
	}
	return out, nil
}

func randomAccessTokenID() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) signAccessToken(payload accessTokenPayload) (string, error) {
	if payload.JTI == "" {
		jti, err := randomAccessTokenID()
		if err != nil {
			return "", err
		}
		payload.JTI = jti
	}
	headerRaw, err := json.Marshal(accessTokenHeader{Alg: "HS256", Typ: "doc-harbor-access-token"})
	if err != nil {
		return "", err
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headerSegment := base64.RawURLEncoding.EncodeToString(headerRaw)
	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadRaw)
	signingInput := headerSegment + "." + payloadSegment
	key, err := s.accessTokenSigningKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

func (s *Server) verifyAccessTokenBearer(r *http.Request) (accessTokenPayload, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return accessTokenPayload{}, errUnauthorized("missing access token")
	}
	return s.verifyAccessToken(parts[1])
}

func (s *Server) verifyAccessToken(token string) (accessTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	var header accessTokenHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "HS256" || header.Typ != "doc-harbor-access-token" {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	key, err := s.accessTokenSigningKey()
	if err != nil {
		return accessTokenPayload{}, err
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(signingInput))
	expectedSignature := mac.Sum(nil)
	if !hmac.Equal(signature, expectedSignature) || !hmac.Equal([]byte(parts[2]), []byte(base64.RawURLEncoding.EncodeToString(expectedSignature))) {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	var payload accessTokenPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return accessTokenPayload{}, errUnauthorized("invalid access token")
	}
	if payload.ExpiresAt <= 0 || payload.JTI == "" || time.Now().UTC().Unix() >= payload.ExpiresAt {
		return accessTokenPayload{}, errUnauthorized("access token expired")
	}
	payload.Scope.ViewerKey = strings.TrimSpace(payload.Scope.ViewerKey)
	return payload, nil
}

func (payload accessTokenPayload) hasCapability(capability string) bool {
	for _, value := range payload.Capabilities {
		if value == capability {
			return true
		}
	}
	return false
}
