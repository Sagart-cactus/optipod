package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/optipod/optipod/internal/consoletls"
)

func pki(args []string) error {
	f := flag.NewFlagSet("pki", flag.ContinueOnError)
	directory := f.String("output-dir", "", "new private output directory")
	hosts := f.String("hosts", "", "comma-separated agent endpoint DNS names/IPs")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *directory == "" || *hosts == "" {
		return fmt.Errorf("output-dir and hosts are required")
	}
	if err := consoletls.Generate(*directory, strings.Split(*hosts, ",")); err != nil {
		return err
	}
	fmt.Println("Private CA and server certificate created. Store the keys in your secret manager.")
	fmt.Println("Configure client CA and TLS server Secrets before enabling enrollment.")
	return nil
}
