package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleinstall"
)

func lifecycle(ctx context.Context, command string, args []string) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	var o consoleinstall.Options
	var admin consoleinstall.AdminClient
	f.StringVar(&admin.URL, "console-url", "", "HTTPS browser/API origin")
	f.StringVar(&admin.TokenFile, "admin-token-file", "", "private administrator credential file")
	f.StringVar(&admin.CAFile, "ca-file", "", "public console CA bundle")
	f.BoolVar(&o.AllowHTTP, "allow-insecure-http", false, "isolated local testing only")
	f.StringVar(&o.PlanFile, "plan-file", "", "saved installation plan")
	f.StringVar(&o.Kubeconfig, "kubeconfig", "", "explicit target kubeconfig")
	f.StringVar(&o.Context, "context", "", "explicit target context")
	registrations := f.String("registrations-file", "", "declarative JSON array of cluster registrations (maximum 50)")
	if err := f.Parse(args); err != nil {
		return err
	}
	admin.AllowHTTP = o.AllowHTTP
	if command == "uninstall" {
		if err := consoleinstall.Uninstall(ctx, o, admin); err != nil {
			return err
		}
		fmt.Println("Agent removed and credentials revoked. Queue PVC, optimizer and stored history retained.")
		return nil
	}
	data, err := os.ReadFile(*registrations)
	if err != nil {
		return err
	}
	var items []console.Registration
	if len(data) > 65536 || json.Unmarshal(data, &items) != nil || len(items) == 0 || len(items) > 50 {
		return fmt.Errorf("invalid registrations file")
	}
	for _, r := range items {
		if err = r.Validate(); err != nil {
			return err
		}
	}
	if err = admin.Post(ctx, "/api/admin/bulk", items); err != nil {
		return err
	}
	fmt.Println("Cluster registrations reconciled. Existing conflicting settings were not overwritten.")
	return nil
}
