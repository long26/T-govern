package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"committee-mvp/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "add-peer":
		if err := runAddPeer(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "add-peer failed: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func runAddPeer(args []string) error {
	fs := flag.NewFlagSet("add-peer", flag.ContinueOnError)
	from := fs.String("from", "", "source node id or all")
	peer := fs.String("peer", "", "peer node id")
	addr := fs.String("addr", "", "peer listen address")
	configDir := fs.String("config-dir", "configs/nodes", "directory holding node config json files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *peer == "" || *addr == "" {
		return errors.New("from, peer and addr are required")
	}

	targets, err := resolveTargetConfigs(*configDir, *from)
	if err != nil {
		return err
	}
	for _, path := range targets {
		cfg, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		cfg.StaticNodes = appendUnique(cfg.StaticNodes, *peer)
		if cfg.StaticNodeAddrs == nil {
			cfg.StaticNodeAddrs = make(map[string]string)
		}
		cfg.StaticNodeAddrs[*peer] = *addr
		if err := writeConfig(path, cfg); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("updated %s\n", path)
	}
	return nil
}

func resolveTargetConfigs(configDir, from string) ([]string, error) {
	if from == "all" {
		entries, err := os.ReadDir(configDir)
		if err != nil {
			return nil, err
		}
		res := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "node-") || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			res = append(res, filepath.Join(configDir, e.Name()))
		}
		sort.Strings(res)
		if len(res) == 0 {
			return nil, fmt.Errorf("no node config found under %s", configDir)
		}
		return res, nil
	}
	path := filepath.Join(configDir, from+".json")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("node config not found: %s", path)
	}
	return []string{path}, nil
}

func writeConfig(path string, cfg *config.NodeConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func appendUnique(in []string, s string) []string {
	for _, v := range in {
		if v == s {
			return in
		}
	}
	return append(in, s)
}

func printUsage() {
	fmt.Println("committee-cli usage:")
	fmt.Println("  committee-cli add-peer --from node-1 --peer node-9 --addr 127.0.0.1:3409 [--config-dir configs/nodes]")
}
