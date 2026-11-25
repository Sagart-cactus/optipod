package consoleagent

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"
)

// RunCommand parses the flags shared by both agent entry points.
func RunCommand(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("agent", flag.ContinueOnError)
	var o Options
	f.StringVar(&o.ClusterID, "cluster-id", "", "registered stable cluster ID")
	f.StringVar(&o.Kubeconfig, "kubeconfig", "", "explicit kubeconfig; otherwise uses in-cluster identity")
	f.StringVar(&o.QueueDir, "queue-dir", "/var/lib/optipod-console", "durable queue directory (PVC in Kubernetes)")
	f.StringVar(&o.URL, "server-url", "", "shared console HTTPS origin")
	f.StringVar(&o.TokenFile, "token-file", "", "file containing this cluster's scoped agent token")
	f.StringVar(&o.IdentityFile, "identity-file", "", "private mTLS identity; renewals persist on the queue PVC")
	f.StringVar(&o.CAFile, "ca-file", "", "additional trusted console CA for token-only legacy agents")
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
	return Run(ctx, o)
}
