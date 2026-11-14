package teehost

import (
	"encoding/hex"
	"github.com/ethereum/go-ethereum/log"
	"os"
)

func logDKRSCommunicationSession(cfg DKRSProtocolConfig) {
	if os.Getenv("TGOVERN_COMM_STATS") != "1" {
		return
	}
	log.Info("E2E_COMM", "marker_schema", 2, "event", "association", "transport", "geth_eth",
		"stage", "rotation_flow", "component", "tee_dkrs", "session", hex.EncodeToString(cfg.SessionID[:]), "epoch", epochString(cfg.NewEpoch))
}
