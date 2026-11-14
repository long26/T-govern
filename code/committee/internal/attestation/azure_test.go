package attestation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAzureVerifierVerifySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attest/SgxEnclave" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("api-version"); got == "" {
			t.Fatalf("missing api-version")
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req["quote"] == "" {
			t.Fatalf("missing quote")
		}
		tok := testJWT(t, map[string]any{
			"x-ms-sgx-mrenclave": "0xAA",
			"x-ms-sgx-mrsigner":  "0xBB",
		})
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	}))
	defer server.Close()

	v := AzureVerifier{Endpoint: server.URL}
	res, err := v.Verify(context.Background(), []byte("quote"), Policy{})
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected accepted result")
	}
	if res.Claims["mrenclave"] != "aa" || res.Claims["mrsigner"] != "bb" {
		t.Fatalf("unexpected claims: %#v", res.Claims)
	}
}

func TestAzureVerifierVerifyMeasurementMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := testJWT(t, map[string]any{"x-ms-sgx-mrenclave": "0xAA"})
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	}))
	defer server.Close()

	v := AzureVerifier{Endpoint: server.URL}
	res, err := v.Verify(context.Background(), []byte("quote"), Policy{ExpectedMeasurement: "0xbb"})
	if err == nil {
		t.Fatalf("expected mismatch error")
	}
	if !strings.Contains(res.Reason, "mrenclave mismatch") {
		t.Fatalf("unexpected reason: %s", res.Reason)
	}
}

func TestAzureVerifierVerifyRejectsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	v := AzureVerifier{Endpoint: server.URL}
	_, err := v.Verify(context.Background(), []byte("quote"), Policy{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	pb, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(pb)
	return fmt.Sprintf("%s.%s.", header, payload)
}
