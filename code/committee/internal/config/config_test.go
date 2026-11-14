package config

import (
	"strings"
	"testing"
)

func TestValidateDefaultChainRPCMode(t *testing.T) {
	cfg := validNodeConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if cfg.ChainRPCMode != "mock" {
		t.Fatalf("expected default chain_rpc_mode to normalize to mock, got: %q", cfg.ChainRPCMode)
	}
}

func TestValidateNormalizesAttestationProvider(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AttestationProvider = "  NoOp  "

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if cfg.AttestationProvider != "noop" {
		t.Fatalf("expected attestation_provider noop, got: %q", cfg.AttestationProvider)
	}
}

func TestValidateChainRPCModeIPCRequiresPath(t *testing.T) {
	cfg := validNodeConfig()
	cfg.ChainRPCMode = "ipc"
	cfg.GethIPCPath = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "geth_ipc_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateChainRPCModeIPCWithPath(t *testing.T) {
	cfg := validNodeConfig()
	cfg.ChainRPCMode = "ipc"
	cfg.GethIPCPath = "/tmp/geth.ipc"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid ipc config, got: %v", err)
	}
}

func TestValidateChainRPCModeUnsupported(t *testing.T) {
	cfg := validNodeConfig()
	cfg.ChainRPCMode = "http"

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "unsupported chain_rpc_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttestationProviderAzureRequiresEndpoint(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AttestationProvider = "azure"
	cfg.AzureAttestationEndpoint = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "azure_attestation_endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttestationProviderAliyunAllowsSDKDefault(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AttestationProvider = "aliyun"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid aliyun config, got: %v", err)
	}
}

func TestValidateAttestationProviderAliyunWithCommand(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AttestationProvider = "aliyun"
	cfg.AliyunQuoteVerifyCommand = "quote-verify-wrapper"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid aliyun config, got: %v", err)
	}
	if cfg.AttestationProvider != "aliyun" {
		t.Fatalf("unexpected provider: %q", cfg.AttestationProvider)
	}
}

func TestValidateAttestationProviderUnsupported(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AttestationProvider = "foo"

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "unsupported attestation_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateNormalizesAllowedClaims(t *testing.T) {
	cfg := validNodeConfig()
	cfg.AllowedMREnclaves = []string{" 0xAA ", "aa", ""}
	cfg.AllowedMRSigners = []string{"0xBb", "bb", "  "}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if len(cfg.AllowedMREnclaves) != 1 || cfg.AllowedMREnclaves[0] != "aa" {
		t.Fatalf("unexpected allowed_mr_enclaves: %#v", cfg.AllowedMREnclaves)
	}
	if len(cfg.AllowedMRSigners) != 1 || cfg.AllowedMRSigners[0] != "bb" {
		t.Fatalf("unexpected allowed_mr_signers: %#v", cfg.AllowedMRSigners)
	}
}

func validNodeConfig() *NodeConfig {
	return &NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1", "node-2", "node-3"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
			"node-2": "127.0.0.1:3402",
			"node-3": "127.0.0.1:3403",
		},
		CommitteeSize:    3,
		Threshold:        2,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}
}
