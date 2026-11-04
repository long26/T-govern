package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// NodeConfig is a static configuration for a committee node in MVP.
type NodeConfig struct {
	NodeID                         string            `json:"node_id"`
	ListenAddr                     string            `json:"listen_addr"`
	StaticNodes                    []string          `json:"static_nodes"`
	StaticNodeAddrs                map[string]string `json:"static_node_addrs"`
	ControlAddr                    string            `json:"control_addr"`
	AllowedMREnclaves              []string          `json:"allowed_mr_enclaves,omitempty"`
	AllowedMRSigners               []string          `json:"allowed_mr_signers,omitempty"`
	AttestationProvider            string            `json:"attestation_provider,omitempty"`
	AzureAttestationEndpoint       string            `json:"azure_attestation_endpoint,omitempty"`
	AzureAttestationAPIKey         string            `json:"azure_attestation_api_key,omitempty"`
	AzureAttestationAPIVersion     string            `json:"azure_attestation_api_version,omitempty"`
	AzureAttestationTimeoutMillis  int               `json:"azure_attestation_timeout_millis,omitempty"`
	AliyunQuoteVerifyCommand       string            `json:"aliyun_quote_verify_command,omitempty"`
	AliyunQuoteVerifyArgs          []string          `json:"aliyun_quote_verify_args,omitempty"`
	AliyunQuoteVerifyTimeoutMillis int               `json:"aliyun_quote_verify_timeout_millis,omitempty"`
	ChainRPCMode                   string            `json:"chain_rpc_mode,omitempty"`
	GethIPCPath                    string            `json:"geth_ipc_path,omitempty"`
	CommitteeSize                  int               `json:"committee_size"`
	Threshold                      int               `json:"threshold"`
	CoordinatorID                  string            `json:"coordinator_id"`
	DomainSeparation               string            `json:"domain_separation"`
	MessageVersion                 string            `json:"message_version"`
}

func Load(path string) (*NodeConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var cfg NodeConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

func (c *NodeConfig) Validate() error {
	if c == nil {
		return errors.New("nil config")
	}
	if c.NodeID == "" {
		return errors.New("node_id is required")
	}
	if c.ListenAddr == "" {
		return errors.New("listen_addr is required")
	}
	if c.CommitteeSize <= 0 {
		return errors.New("committee_size must be > 0")
	}
	if c.Threshold <= 0 || c.Threshold > c.CommitteeSize {
		return errors.New("threshold must be in range (0, committee_size]")
	}
	if len(c.StaticNodes) == 0 {
		return errors.New("static_nodes cannot be empty")
	}
	if len(c.StaticNodeAddrs) == 0 {
		return errors.New("static_node_addrs cannot be empty")
	}
	for _, nodeID := range c.StaticNodes {
		addr, ok := c.StaticNodeAddrs[nodeID]
		if !ok || addr == "" {
			return fmt.Errorf("missing static_node_addrs entry for %s", nodeID)
		}
	}
	if c.CoordinatorID == "" {
		return errors.New("coordinator_id is required")
	}
	if c.DomainSeparation == "" {
		return errors.New("domain_separation is required")
	}
	if c.MessageVersion == "" {
		return errors.New("message_version is required")
	}

	c.AttestationProvider = strings.ToLower(strings.TrimSpace(c.AttestationProvider))
	if c.AttestationProvider == "" {
		c.AttestationProvider = "noop"
	}
	switch c.AttestationProvider {
	case "noop":
	case "azure":
		if strings.TrimSpace(c.AzureAttestationEndpoint) == "" {
			return errors.New("azure_attestation_endpoint is required when attestation_provider=azure")
		}
		if c.AzureAttestationTimeoutMillis < 0 {
			return errors.New("azure_attestation_timeout_millis must be >= 0")
		}
	case "aliyun":
		c.AliyunQuoteVerifyCommand = strings.TrimSpace(c.AliyunQuoteVerifyCommand)
		if c.AliyunQuoteVerifyTimeoutMillis < 0 {
			return errors.New("aliyun_quote_verify_timeout_millis must be >= 0")
		}
	default:
		return fmt.Errorf("unsupported attestation_provider %q (allowed: noop, azure, aliyun)", c.AttestationProvider)
	}

	c.AllowedMREnclaves = normalizeClaimList(c.AllowedMREnclaves)
	c.AllowedMRSigners = normalizeClaimList(c.AllowedMRSigners)

	mode := strings.ToLower(strings.TrimSpace(c.ChainRPCMode))
	switch mode {
	case "", "mock":
		c.ChainRPCMode = "mock"
	case "ipc":
		c.ChainRPCMode = "ipc"
		if strings.TrimSpace(c.GethIPCPath) == "" {
			return errors.New("geth_ipc_path is required when chain_rpc_mode=ipc")
		}
		c.GethIPCPath = strings.TrimSpace(c.GethIPCPath)
	default:
		return fmt.Errorf("unsupported chain_rpc_mode %q (allowed: mock, ipc)", c.ChainRPCMode)
	}
	return nil
}

func normalizeClaimList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		n := strings.ToLower(strings.TrimSpace(v))
		if strings.HasPrefix(n, "0x") {
			n = n[2:]
		}
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
