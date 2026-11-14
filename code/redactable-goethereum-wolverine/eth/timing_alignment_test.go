package eth

import "testing"

func TestLedgerObserverIdentityRequiresExplicitOrdinaryFullRole(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		index int
		ok    bool
	}{
		{name: "observer", env: map[string]string{"NITCH_NODE_ROLE": "ledger_observer", "NITCH_NODE_INDEX": "17"}, index: 17, ok: true},
		{name: "committee", env: map[string]string{"NITCH_NODE_ROLE": "core", "NITCH_NODE_INDEX": "1"}},
		{name: "light", env: map[string]string{"NITCH_NODE_ROLE": "light", "NITCH_NODE_INDEX": "18"}},
		{name: "missing index", env: map[string]string{"NITCH_NODE_ROLE": "ledger_observer"}},
		{name: "invalid index", env: map[string]string{"NITCH_NODE_ROLE": "ledger_observer", "NITCH_NODE_INDEX": "0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, ok := ledgerObserverIdentity(func(key string) string { return test.env[key] })
			if index != test.index || ok != test.ok {
				t.Fatalf("ledgerObserverIdentity() = (%d, %t), want (%d, %t)", index, ok, test.index, test.ok)
			}
		})
	}
}
