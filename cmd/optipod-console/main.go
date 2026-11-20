// optipod-console serves the shared read-only console or exports cluster state.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleagent"
	"github.com/optipod/optipod/internal/consoleversion"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && err != flag.ErrHelp {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: optipod-console <command>; use --help to list commands")
	}
	switch args[0] {
	case "plan", "preflight", "onboard", "render":
		return onboarding(ctx, args[0], args[1:])
	case "probe":
		return probe(ctx, args[1:])
	case "reconcile", "uninstall":
		return lifecycle(ctx, args[0], args[1:])
	case "pki":
		return pki(args[1:])
	case "version":
		fmt.Println(consoleversion.Version)
		return nil
	case "--help", "-h", "help":
		fmt.Println("commands: serve, agent, enroll, plan, preflight, onboard, render, reconcile, uninstall, " +
			"pki, version, hash-token")
		return nil
	case "hash-token":
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return err
		}
		token := strings.TrimSpace(string(data))
		if len(token) < 32 {
			return fmt.Errorf("use a randomly generated token of at least 32 characters")
		}
		fmt.Println(console.TokenHash(token))
		return nil
	case "serve":
		return serve(ctx, args[1:])
	case "enroll":
		return enroll(ctx, args[1:])
	case "agent":
		return consoleagent.RunCommand(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command")
	}
}

func serve(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	config := f.String("config", "", "JSON configuration with hashed credentials and explicit grants")
	database := f.String("database-url-file", "", "file containing PostgreSQL DSN")
	listen := f.String("listen", "127.0.0.1:8766", "HTTP listener; use a TLS ingress for shared access")
	retention := f.Int("retention-days", 90, "retention for observed history and allocation samples")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *retention < 1 || *retention > 3650 {
		return fmt.Errorf("retention-days must be 1..3650")
	}
	cfg, err := console.LoadConfig(*config)
	if err != nil {
		return err
	}
	dsn, err := os.ReadFile(*database)
	if err != nil {
		return err
	}
	store, err := console.Open(ctx, strings.TrimSpace(string(dsn)))
	if err != nil {
		return err
	}
	defer store.Pool.Close()
	if err = store.Migrate(ctx); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	api := &console.Server{Store: store, Config: cfg}
	if err = api.InitOIDC(ctx); err != nil {
		return err
	}
	tlsConfig, err := api.InitAgentTLS()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: *listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute,
	}
	go retentionLoop(ctx, store, *retention)
	go healthLoop(ctx, store)
	slog.Info("shared console listening", "address", *listen)
	servers := []*http.Server{server}
	errors := make(chan error, 2)
	go func() { errors <- server.ListenAndServe() }()
	if tlsConfig != nil {
		agentServer := &http.Server{
			Addr: cfg.AgentTLS.Listen, Handler: api.Handler(), TLSConfig: tlsConfig,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
			WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute,
		}
		servers = append(servers, agentServer)
		go func() { errors <- agentServer.ListenAndServeTLS("", "") }()
	}
	select {
	case <-ctx.Done():
	case err = <-errors:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, item := range servers {
		_ = item.Shutdown(shutdown)
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func healthLoop(ctx context.Context, store *console.Store) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := store.MaintainAlerts(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("console health notifications deferred")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func retentionLoop(ctx context.Context, store *console.Store, days int) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		pruneCtx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := store.Prune(pruneCtx, days); err != nil {
			slog.Warn("console retention deferred")
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
