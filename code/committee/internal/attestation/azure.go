package attestation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultAzureAPIVersion = "2022-08-01-preview"

type AzureVerifier struct {
	Endpoint   string
	APIKey     string
	APIVersion string
	HTTPClient *http.Client
}

type azureAttestRequest struct {
	Quote string `json:"quote"`
}

type azureAttestResponse struct {
	Token string `json:"token"`
}

func (v AzureVerifier) Verify(ctx context.Context, quote []byte, policy Policy) (Result, error) {
	if len(quote) == 0 {
		return Result{Accepted: false, Reason: "empty quote"}, ErrQuoteRejected
	}
	endpoint := strings.TrimRight(strings.TrimSpace(v.Endpoint), "/")
	if endpoint == "" {
		return Result{}, fmt.Errorf("azure attestation endpoint is required")
	}
	apiVersion := strings.TrimSpace(v.APIVersion)
	if apiVersion == "" {
		apiVersion = defaultAzureAPIVersion
	}

	attestURL := endpoint + "/attest/SgxEnclave?api-version=" + url.QueryEscape(apiVersion)
	bodyBytes, err := json.Marshal(azureAttestRequest{Quote: base64.StdEncoding.EncodeToString(quote)})
	if err != nil {
		return Result{}, fmt.Errorf("marshal azure attestation request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, attestURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return Result{}, fmt.Errorf("create azure attestation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(v.APIKey) != "" {
		req.Header.Set("Ocp-Apim-Subscription-Key", strings.TrimSpace(v.APIKey))
	}

	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("call azure attestation: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("read azure attestation response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reason := strings.TrimSpace(string(respBody))
		if reason == "" {
			reason = resp.Status
		}
		return Result{Accepted: false, Reason: reason}, ErrQuoteRejected
	}

	var parsed azureAttestResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, fmt.Errorf("decode azure attestation response: %w", err)
	}
	token := strings.TrimSpace(parsed.Token)
	if token == "" {
		return Result{}, fmt.Errorf("azure attestation response missing token")
	}

	claims, err := parseJWTClaims(token)
	if err != nil {
		return Result{}, fmt.Errorf("parse attestation token claims: %w", err)
	}

	mrEnclave := normalizeClaim(claimByKeys(claims,
		"x-ms-sgx-mrenclave", "x_ms_sgx_mrenclave", "sgx_mrenclave", "mrenclave"))
	mrSigner := normalizeClaim(claimByKeys(claims,
		"x-ms-sgx-mrsigner", "x_ms_sgx_mrsigner", "sgx_mrsigner", "mrsigner"))
	claimsOut := map[string]string{
		"mrenclave": mrEnclave,
		"mrsigner":  mrSigner,
	}
	if expected := normalizeClaim(policy.ExpectedMeasurement); expected != "" && mrEnclave != expected {
		return Result{Accepted: false, Reason: "mrenclave mismatch", Claims: claimsOut}, ErrQuoteRejected
	}

	return Result{Accepted: true, Reason: "accepted by azure verifier", Claims: claimsOut}, nil
}

func parseJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid jwt format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode jwt payload: %w", err)
	}
	claims := make(map[string]any)
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("decode jwt claims: %w", err)
	}
	return claims, nil
}

func claimByKeys(claims map[string]any, keys ...string) string {
	for _, k := range keys {
		for ck, cv := range claims {
			if strings.EqualFold(ck, k) {
				if s, ok := cv.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

func normalizeClaim(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "0x"), "0X")
	return strings.ToLower(v)
}
