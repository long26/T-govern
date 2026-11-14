package eth

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestDefaultElectionConfig(t *testing.T) {
	cfg := DefaultElectionConfig()
	if cfg.EpochLength != DefaultElectionEpochLength {
		t.Errorf("EpochLength: got %d, want %d", cfg.EpochLength, DefaultElectionEpochLength)
	}
	if cfg.CommitteeSize != DefaultElectionCommitteeSize {
		t.Errorf("CommitteeSize: got %d, want %d", cfg.CommitteeSize, DefaultElectionCommitteeSize)
	}
	if cfg.Threshold != DefaultElectionThreshold {
		t.Errorf("Threshold: got %d, want %d", cfg.Threshold, DefaultElectionThreshold)
	}
	if cfg.Target != MaxTarget {
		t.Errorf("Target: got %s, want %s", cfg.Target.Hex(), MaxTarget.Hex())
	}
	if cfg.SystemAddress != DefaultSolutionSystemAddress {
		t.Errorf("SystemAddress: got %s, want %s", cfg.SystemAddress.Hex(), DefaultSolutionSystemAddress.Hex())
	}
	if cfg.InitialBoundary != DefaultElectionInitialBoundary {
		t.Errorf("InitialBoundary: got %d, want %d", cfg.InitialBoundary, DefaultElectionInitialBoundary)
	}
}

func TestParseElectionConfigFromEnv(t *testing.T) {
	// 1. All empty env -> defaults
	emptyEnv := func(k string) string { return "" }
	cfg, err := ParseElectionConfigFromEnv(emptyEnv)
	if err != nil {
		t.Fatalf("unexpected error parsing empty env: %v", err)
	}
	if cfg != DefaultElectionConfig() {
		t.Errorf("expected default config, got %+v", cfg)
	}

	// 2. Valid overrides
	validEnvMap := map[string]string{
		"NITCH_EPOCH_LENGTH":         "50",
		"NITCH_POW_INITIAL_BOUNDARY": "100",
		"NITCH_POW_COMMITTEE_SIZE":   "7",
		"NITCH_POW_THRESHOLD":        "4",
		"NITCH_POW_DIFFICULTY":       "1000",
		"NITCH_POW_SYSTEM_ADDRESS":   "0x0000000000000000000000000000000000001234",
	}
	validEnv := func(k string) string { return validEnvMap[k] }
	cfg, err = ParseElectionConfigFromEnv(validEnv)
	if err != nil {
		t.Fatalf("unexpected error parsing valid env: %v", err)
	}
	if cfg.EpochLength != 50 {
		t.Errorf("EpochLength: got %d, want 50", cfg.EpochLength)
	}
	if cfg.InitialBoundary != 100 {
		t.Errorf("InitialBoundary: got %d, want 100", cfg.InitialBoundary)
	}
	if cfg.CommitteeSize != 7 {
		t.Errorf("CommitteeSize: got %d, want 7", cfg.CommitteeSize)
	}
	if cfg.Threshold != 4 {
		t.Errorf("Threshold: got %d, want 4", cfg.Threshold)
	}
	expectedTarget, _ := DifficultyToTarget(big.NewInt(1000))
	if cfg.Target != expectedTarget {
		t.Errorf("Target: got %s, want %s", cfg.Target.Hex(), expectedTarget.Hex())
	}
	if cfg.SystemAddress != common.HexToAddress("0x0000000000000000000000000000000000001234") {
		t.Errorf("SystemAddress: got %s", cfg.SystemAddress.Hex())
	}

	// 3. Explicit NITCH_POW_TARGET overrides NITCH_POW_DIFFICULTY
	targetOverrideMap := map[string]string{
		"NITCH_POW_DIFFICULTY": "1000",
		"NITCH_POW_TARGET":     "0x0000000000000000000000000000000000000000000000000000000000000042",
	}
	cfg, err = ParseElectionConfigFromEnv(func(k string) string { return targetOverrideMap[k] })
	if err != nil {
		t.Fatalf("unexpected error parsing target override: %v", err)
	}
	expectedExplicitTarget := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000042")
	if cfg.Target != expectedExplicitTarget {
		t.Errorf("Target override mismatch: got %s, want %s", cfg.Target.Hex(), expectedExplicitTarget.Hex())
	}

	// 4. Invalid input tests
	invalidCases := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "zero epoch length",
			env:  map[string]string{"NITCH_EPOCH_LENGTH": "0"},
		},
		{
			name: "epoch length 1 (below minimum 2)",
			env:  map[string]string{"NITCH_EPOCH_LENGTH": "1"},
		},
		{
			name: "negative epoch length",
			env:  map[string]string{"NITCH_EPOCH_LENGTH": "-5"},
		},
		{
			name: "non-numeric epoch length",
			env:  map[string]string{"NITCH_EPOCH_LENGTH": "abc"},
		},
		{
			name: "zero committee size",
			env:  map[string]string{"NITCH_POW_COMMITTEE_SIZE": "0"},
		},
		{
			name: "threshold equals committee size",
			env:  map[string]string{"NITCH_POW_COMMITTEE_SIZE": "4", "NITCH_POW_THRESHOLD": "4"},
		},
		{
			name: "threshold exceeds committee size",
			env:  map[string]string{"NITCH_POW_COMMITTEE_SIZE": "4", "NITCH_POW_THRESHOLD": "5"},
		},
		{
			name: "negative threshold",
			env:  map[string]string{"NITCH_POW_THRESHOLD": "-1"},
		},
		{
			name: "zero difficulty",
			env:  map[string]string{"NITCH_POW_DIFFICULTY": "0"},
		},
		{
			name: "negative difficulty",
			env:  map[string]string{"NITCH_POW_DIFFICULTY": "-10"},
		},
		{
			name: "invalid system address",
			env:  map[string]string{"NITCH_POW_SYSTEM_ADDRESS": "not-an-address"},
		},
		{
			name: "invalid initial boundary",
			env:  map[string]string{"NITCH_POW_INITIAL_BOUNDARY": "invalid"},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseElectionConfigFromEnv(func(k string) string { return tc.env[k] })
			if err == nil {
				t.Errorf("expected error for case %q, got nil", tc.name)
			}
		})
	}
}

func TestElectionConfigValidate(t *testing.T) {
	valid := DefaultElectionConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}

	// EpochLength < 2 rejected
	cZero := valid
	cZero.EpochLength = 0
	if err := cZero.Validate(); err == nil {
		t.Error("expected error for EpochLength=0, got nil")
	}

	cOne := valid
	cOne.EpochLength = 1
	if err := cOne.Validate(); err == nil {
		t.Error("expected error for EpochLength=1 (open interval empty), got nil")
	}

	// EpochLength == 2 accepted (open interval has 1 block)
	cTwo := valid
	cTwo.EpochLength = 2
	if err := cTwo.Validate(); err != nil {
		t.Errorf("EpochLength=2 should be valid, got: %v", err)
	}

	// CommitteeSize <= 0
	cComm := valid
	cComm.CommitteeSize = 0
	if err := cComm.Validate(); err == nil {
		t.Error("expected error for CommitteeSize=0, got nil")
	}

	// Threshold < 0 or >= CommitteeSize
	cThreshNeg := valid
	cThreshNeg.Threshold = -1
	if err := cThreshNeg.Validate(); err == nil {
		t.Error("expected error for negative threshold, got nil")
	}

	cThreshMax := valid
	cThreshMax.Threshold = cThreshMax.CommitteeSize
	if err := cThreshMax.Validate(); err == nil {
		t.Error("expected error for threshold == committee size, got nil")
	}

	// Zero target
	cZeroTarget := valid
	cZeroTarget.Target = common.Hash{}
	if err := cZeroTarget.Validate(); err == nil {
		t.Error("expected error for zero target, got nil")
	}
}
