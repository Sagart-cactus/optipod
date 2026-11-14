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
		return fmt.Errorf("usage: optipod-console serve|agent|hash-token")
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Println("usage: optipod-console serve|agent|hash-token; use <command> --help for options")
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
	case "agent":
		return agent(ctx, args[1:])
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
	server := &http.Server{
		Addr: *listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go retentionLoop(ctx, store, *retention)
	slog.Info("shared console listening", "address", *listen)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
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

func agent(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("agent", flag.ContinueOnError)
	var o consoleagent.Options
	f.StringVar(&o.ClusterID, "cluster-id", "", "registered stable cluster ID")
	f.StringVar(&o.Kubeconfig, "kubeconfig", "", "explicit kubeconfig; otherwise uses in-cluster identity")
	f.StringVar(&o.QueueDir, "queue-dir", "/var/lib/optipod-console", "durable queue directory (PVC in Kubernetes)")
	f.StringVar(&o.URL, "server-url", "", "shared console HTTPS origin")
	f.StringVar(&o.TokenFile, "token-file", "", "file containing this cluster's scoped agent token")
	f.DurationVar(&o.Interval, "interval", time.Minute, "observation interval")
	f.Int64Var(&o.QueueBytes, "queue-bytes", 256*1024*1024, "maximum queued payload bytes")
	f.BoolVar(&o.AllowHTTP, "allow-insecure-http", false, "allow HTTP only for isolated local testing")
	f.BoolVar(&o.Once, "once", false, "collect and upload once; fail if delivery is incomplete")
	namespaces := f.String("namespaces", "", "comma-separated namespaces; empty means all")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *namespaces != "" {
		for _, n := range strings.Split(*namespaces, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				return fmt.Errorf("empty namespace")
			}
			o.Namespaces = append(o.Namespaces, n)
		}
	}
	return consoleagent.Run(ctx, o)
}
