package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/report"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func inspect(args []string) error {
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	path := flags.String("kubeconfig", "", "required explicit kubeconfig file")
	contextName := flags.String("context", "", "context in that kubeconfig; defaults to its current context")
	namespace := flags.String("namespace", "", "workload namespace filter (all if omitted)")
	output := flags.String("output", outputJSON, "json, csv, or table for fleet; json for history")
	uid := flags.String("workload-uid", "", "history workload UID filter")
	port := flags.Int("port", 8765, "serve port on 127.0.0.1 only (0 chooses an available port)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return fmt.Errorf("%s requires --kubeconfig FILE and no positional arguments", command)
	}
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("port must be 0..65535")
	}
	if command == commandHistory && *output != outputJSON {
		return fmt.Errorf("history supports --output json")
	}
	if command == "fleet" && *output != outputJSON && *output != "csv" && *output != "table" {
		return fmt.Errorf("fleet output must be json, csv, or table")
	}
	raw, err := clientcmd.LoadFromFile(*path)
	if err != nil {
		return err
	}
	if *contextName == "" {
		*contextName = raw.CurrentContext
	}
	if raw.Contexts[*contextName] == nil {
		return fmt.Errorf("context %q not found", *contextName)
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(
		*raw, *contextName, &clientcmd.ConfigOverrides{},
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: *path},
	).ClientConfig()
	if err != nil {
		return err
	}
	cfg.Timeout = 30 * time.Second
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := api.AddToScheme(scheme); err != nil {
		return err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	collector := &report.Collector{Client: c, Namespace: *namespace}
	fmt.Fprintf(os.Stderr, "Read-only context: %s; namespace: %s\n", *contextName, displayNamespace(*namespace))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "serve" {
		return serve(ctx, collector, *port)
	}
	ctx, cancelRead := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelRead()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if command == commandHistory {
		journal, err := collector.History(ctx, *namespace, *uid)
		if err != nil {
			return err
		}
		return encoder.Encode(journal)
	}
	fleet, err := collector.Fleet(ctx)
	if err != nil {
		return err
	}
	if *output == outputJSON {
		return encoder.Encode(fleet)
	}
	return writeFleet(fleet, *output)
}

func displayNamespace(value string) string {
	if value == "" {
		return "all"
	}
	return value
}

func serve(ctx context.Context, collector *report.Collector, port int) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	host := listener.Addr().String()
	server := &http.Server{
		Handler: collector.Handler(host), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintln(os.Stderr, "OptiPod explorer: http://"+host)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func writeFleet(fleet *report.Fleet, output string) error {
	headers := []string{"Namespace", "Kind", "Workload", "Policy", "Mode", "CPU_milli", "Memory_bytes", "Pods", "Reason"}
	rows := make([][]string, 0, 1+len(fleet.Workloads))
	rows = append(rows, headers)
	for _, w := range fleet.Workloads {
		rows = append(rows, []string{
			w.Namespace, w.Kind, w.Name, w.Policy, string(w.Mode),
			strconv.FormatInt(w.RequestedMilliCPU, 10), strconv.FormatInt(w.RequestedMemoryBytes, 10),
			strconv.Itoa(len(w.Pods)), w.Reason,
		})
	}
	if output == "csv" {
		writer := csv.NewWriter(os.Stdout)
		for _, row := range rows {
			for i, cell := range row {
				if isSpreadsheetFormula(cell) {
					row[i] = "'" + cell
				}
			}
			if err := writer.Write(row); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	}
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, strings.Join(row[:8], "\t")); err != nil {
			return err
		}
	}
	return writer.Flush()
}
func isSpreadsheetFormula(cell string) bool {
	trimmed := strings.TrimLeft(cell, " \t\r\n")
	return len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))
}
