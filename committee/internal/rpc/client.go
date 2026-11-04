package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Client struct {
	endpoint   string
	httpClient *http.Client
	nextID     uint64
}

func NewClient(endpoint string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{
		endpoint:   normalizeEndpoint(endpoint),
		httpClient: httpClient,
	}
}

func (c *Client) Call(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := atomic.AddUint64(&c.nextID, 1)
	payload := map[string]interface{}{
		"jsonrpc": JSONRPCVersion,
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send rpc request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read rpc response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rpc http status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var rpcResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *Error          `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return fmt.Errorf("decode rpc response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("%s", rpcResp.Error.Message)
	}
	if result == nil {
		return nil
	}
	if len(rpcResp.Result) == 0 || string(rpcResp.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return fmt.Errorf("decode rpc result: %w", err)
	}
	return nil
}

func normalizeEndpoint(endpoint string) string {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return "http://127.0.0.1:4401" + DefaultPath
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	if strings.HasSuffix(s, DefaultPath) {
		return s
	}
	if strings.HasSuffix(s, "/") {
		return strings.TrimRight(s, "/") + DefaultPath
	}
	return s + DefaultPath
}
