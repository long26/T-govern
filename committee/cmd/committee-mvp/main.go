package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"committee-mvp/internal/attestation"
	"committee-mvp/internal/chain"
	"committee-mvp/internal/committee"
	"committee-mvp/internal/config"
	"committee-mvp/internal/rpc"
)

func main() {
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "node":
			if err := runNode(os.Args[2:]); err != nil {
				log.Fatalf("node failed: %v", err)
			}
			return
		case "admin":
			if err := runAdmin(os.Args[2:]); err != nil {
				log.Fatalf("admin failed: %v", err)
			}
			return
		case "console":
			if err := runAdminConsole(os.Args[2:]); err != nil {
				log.Fatalf("console failed: %v", err)
			}
			return
		case "version":
			fmt.Println("committee-mvp v0.1.0")
			return
		case "help":
			printUsage()
			return
		default:
			printUsage()
			os.Exit(1)
		}
	}

	// geth-like default behavior: running without subcommand starts the node.
	if err := runNode(os.Args[1:]); err != nil {
		log.Fatalf("node failed: %v", err)
	}
}

func runNode(args []string) error {
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	configPath := fs.String("config", "configs/devnet.json", "path to node config file")
	controlAddr := fs.String("control-addr", "", "optional control address for admin API, e.g. 127.0.0.1:4401")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config failed: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if *controlAddr != "" {
		cfg.ControlAddr = *controlAddr
	}

	opts, cleanup, err := buildServiceOptions(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	svc := committee.NewServiceWithNetworkAndOptions(cfg, nil, opts...)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := svc.Start(ctx); err != nil {
		return fmt.Errorf("service start failed: %w", err)
	}
	if cfg.ControlAddr != "" {
		log.Printf("rpc server listening on http://%s%s", cfg.ControlAddr, rpc.DefaultPath)
	}
	<-ctx.Done()
	if err := svc.Stop(context.Background()); err != nil {
		log.Printf("service stop failed: %v", err)
	}
	return nil
}

func buildServiceOptions(cfg *config.NodeConfig) ([]committee.Option, func(), error) {
	if cfg == nil {
		return nil, nil, errors.New("nil config")
	}
	opts := make([]committee.Option, 0, 2)

	switch cfg.AttestationProvider {
	case "azure":
		httpTimeout := 8 * time.Second
		if cfg.AzureAttestationTimeoutMillis > 0 {
			httpTimeout = time.Duration(cfg.AzureAttestationTimeoutMillis) * time.Millisecond
		}
		verifier := attestation.AzureVerifier{
			Endpoint:   cfg.AzureAttestationEndpoint,
			APIKey:     cfg.AzureAttestationAPIKey,
			APIVersion: cfg.AzureAttestationAPIVersion,
			HTTPClient: &http.Client{Timeout: httpTimeout},
		}
		opts = append(opts, committee.WithQuoteVerifier(verifier))
	case "aliyun":
		verifyTimeout := 8 * time.Second
		if cfg.AliyunQuoteVerifyTimeoutMillis > 0 {
			verifyTimeout = time.Duration(cfg.AliyunQuoteVerifyTimeoutMillis) * time.Millisecond
		}
		opts = append(opts, committee.WithQuoteVerifier(attestation.AliyunVerifier{
			Command: cfg.AliyunQuoteVerifyCommand,
			Args:    cfg.AliyunQuoteVerifyArgs,
			Timeout: verifyTimeout,
		}))
	default:
		opts = append(opts, committee.WithQuoteVerifier(attestation.NoopVerifier{RejectEmptyQuote: true}))
	}

	if cfg.ChainRPCMode == "mock" {
		return opts, func() {}, nil
	}
	if cfg.ChainRPCMode == "ipc" {
		dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		gateway, err := chain.NewIPCGateway(dialCtx, cfg.GethIPCPath)
		if err != nil {
			return nil, nil, fmt.Errorf("connect geth ipc %q: %w", cfg.GethIPCPath, err)
		}
		cleanup := func() {
			gateway.Close()
		}
		opts = append(opts, committee.WithChainGateway(gateway))
		return opts, cleanup, nil
	}

	return nil, nil, fmt.Errorf("invalid chain_rpc_mode after validation: %q", cfg.ChainRPCMode)
}

func runAdmin(args []string) error {
	if len(args) == 0 {
		return runAdminConsole(nil)
	}
	switch args[0] {
	case "console":
		return runAdminConsole(args[1:])
	case "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unsupported admin subcommand: %s (only 'console' is supported)", args[0])
	}
}

func runAdminConsole(args []string) error {
	fs := flag.NewFlagSet("admin console", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	controlAddr := fs.String("control-addr", "127.0.0.1:4401", "default node control address")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	currentAddr := *controlAddr
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("interactive admin console")
	fmt.Println("type 'help' for commands, 'exit' to quit")
	fmt.Printf("current target: %s\n", currentAddr)

	for {
		fmt.Printf("[%s] > ", currentAddr)
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "help":
			printConsoleHelp()
		case "exit", "quit":
			return nil
		case "target":
			if len(parts) < 2 {
				fmt.Println("usage: target <host:port>")
				continue
			}
			currentAddr = parts[1]
			fmt.Printf("target switched to %s\n", currentAddr)
		case "pubkey":
			client := newRPCClient(currentAddr, *timeout)
			var resp rpc.GetPublicKeyResult
			err := client.Call(context.Background(), rpc.MethodGetPublicKey, rpc.EmptyParams{}, &resp)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("committee_pub_key_hex=%s\n", resp.CommitteePubKey)
		case "autosign":
			if len(parts) < 2 || (parts[1] != "on" && parts[1] != "off") {
				fmt.Println("usage: autosign <on|off>")
				continue
			}
			enabled := parts[1] == "on"
			client := newRPCClient(currentAddr, *timeout)
			if err := client.Call(context.Background(), rpc.MethodSetAutoSign, rpc.SetAutoSignParams{
				Enabled: enabled,
			}, nil); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("auto-sign set to %v\n", enabled)
		case "verifyquote":
			fmt.Print("quote payload: ")
			quote, _ := reader.ReadString('\n')
			quote = strings.TrimSpace(quote)
			if quote == "" {
				fmt.Println("quote is required")
				continue
			}
			client := newRPCClient(currentAddr, *timeout)
			var resp rpc.VerifyQuoteResult
			err := client.Call(context.Background(), rpc.MethodVerifyQuote, rpc.VerifyQuoteParams{Quote: quote}, &resp)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("quote_accepted=%t reason=%s\n", resp.QuoteAccepted, resp.QuoteReason)
		case "registertee":
			fmt.Print("owner address: ")
			ownerAddress, _ := reader.ReadString('\n')
			ownerAddress = strings.TrimSpace(ownerAddress)
			if ownerAddress == "" {
				fmt.Println("owner address is required")
				continue
			}
			fmt.Print("quote payload: ")
			quote, _ := reader.ReadString('\n')
			quote = strings.TrimSpace(quote)
			if quote == "" {
				fmt.Println("quote is required")
				continue
			}
			client := newRPCClient(currentAddr, 30*time.Second)
			var resp rpc.RegisterTEEResult
			err := client.Call(context.Background(), rpc.MethodRegisterTEE, rpc.RegisterTEEParams{
				OwnerAddress: ownerAddress,
				Quote:        quote,
			}, &resp)
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Printf("ok=%t request_id=%s\n", true, resp.RequestID)
			fmt.Printf("reason=%s\n", resp.Reason)
			fmt.Printf("mr_enclave=%s\n", resp.MrEnclave)
			fmt.Printf("mr_signer=%s\n", resp.MrSigner)
			fmt.Printf("tx_hash=%s\n", resp.TxHash)
		default:
			fmt.Println("unknown command, type 'help'")
		}
	}
}

func printConsoleHelp() {
	fmt.Println("commands:")
	fmt.Println("  help                    show this help")
	fmt.Println("  target <host:port>      switch target control address")
	fmt.Println("  pubkey                  query committee public key hex")
	fmt.Println("  verifyquote             verify one quote with current node verifier")
	fmt.Println("  registertee             register one owner + quote through committee voting")
	fmt.Println("  autosign <on|off>       set node auto signing mode")
	fmt.Println("  exit                    quit console")
}

func newRPCClient(addr string, timeout time.Duration) *rpc.Client {
	return rpc.NewClient(addr, &http.Client{Timeout: timeout})
}

func runAddPeer(args []string) error {
	fs := flag.NewFlagSet("admin add-peer", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
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
	fmt.Println("committee-mvp usage:")
	fmt.Println("  committee-mvp [global flags]                  # default to node")
	fmt.Println("  committee-mvp node --config configs/devnet.json")
	fmt.Println("  committee-mvp admin console --control-addr 127.0.0.1:4401")
	fmt.Println("  committee-mvp console --control-addr 127.0.0.1:4401")
	fmt.Println("  committee-mvp version")
}
