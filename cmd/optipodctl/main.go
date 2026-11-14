// optipodctl provides offline migration and explicit cluster inspection tools.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/optipod/optipod/internal/migration"
	"sigs.k8s.io/yaml"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 0 && (args[0] == "fleet" || args[0] == commandHistory || args[0] == "serve") {
		return inspect(args)
	}
	if len(args) == 0 || args[0] != "migrate" {
		return fmt.Errorf("usage: optipodctl fleet|history|serve --kubeconfig FILE; or " +
			"optipodctl migrate -f configuration.yaml --namespace TARGET --name POLICY " +
			"--startup-mode ignore --coverage-percent 90 --minimum-history 24h")
	}
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	file := flags.String("f", "", "workload migration configuration YAML (explicit settings; see examples/migration)")
	namespace := flags.String("namespace", "", "target namespace")
	name := flags.String("name", "", "policy name")
	startup := flags.String("startup-mode", "", "ignore or include; must match your effective source behavior")
	coverage := flags.Int("coverage-percent", 90, "independent observed-time coverage gate (0..100)")
	history := flags.Duration("minimum-history", 24*time.Hour, "independent minimum observed history")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *coverage < 0 || *coverage > 100 {
		return fmt.Errorf("coverage-percent must be 0..100")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	result, err := migration.Translate(data, migration.Options{
		Namespace: *namespace, Name: *name, StartupMode: *startup,
		CoveragePercent: int32(*coverage), MinimumHistory: *history,
	})
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "Review:", warning)
	}
	out, err := yaml.Marshal(result.Policy)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}
