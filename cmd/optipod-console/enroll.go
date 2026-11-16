package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/optipod/optipod/internal/consoleenroll"
)

func enroll(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("enroll", flag.ContinueOnError)
	var o consoleenroll.Options
	f.StringVar(&o.EnrollmentFile, "enrollment-file", "", "private JSON enrollment file downloaded from the console")
	f.StringVar(&o.Kubeconfig, "kubeconfig", "", "explicit target kubeconfig; never uploaded")
	f.StringVar(&o.Context, "context", "", "explicit target Kubernetes context")
	f.StringVar(&o.TokenOutput, "token-output", "", "new private file for the runtime token; must not already exist")
	f.BoolVar(&o.AllowHTTP, "allow-insecure-http", false, "allow loopback HTTP only for local testing")
	if err := f.Parse(args); err != nil {
		return err
	}
	result, err := consoleenroll.Run(ctx, o)
	if err != nil {
		return err
	}
	fmt.Printf("Enrollment complete. Credential %s expires %s.\n",
		result.CredentialID, result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Println("Runtime token saved to the private output file.")
	return nil
}
