package consoleagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	"github.com/optipod/optipod/internal/console"
	"github.com/optipod/optipod/internal/consoleversion"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Options struct {
	Kubeconfig, ClusterID, QueueDir, URL, TokenFile string
	IdentityFile, CAFile                            string
	Namespaces                                      []string
	Interval                                        time.Duration
	QueueBytes                                      int64
	AllowHTTP                                       bool
	Once                                            bool
}

func Run(ctx context.Context, o Options) error {
	if o.ClusterID == "" || o.Interval < time.Second {
		return fmt.Errorf("cluster ID and interval >=1s required")
	}
	var cfg *rest.Config
	var err error
	if o.Kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return err
	}
	cfg.Timeout = 20 * time.Second
	scheme := runtime.NewScheme()
	if err = clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err = api.AddToScheme(scheme); err != nil {
		return err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	ns := &corev1.Namespace{}
	if err = c.Get(ctx, client.ObjectKey{Name: "kube-system"}, ns); err != nil {
		return err
	}
	q, err := OpenQueue(o.QueueDir, o.ClusterID+"/"+string(ns.UID), o.QueueBytes)
	if err != nil {
		return err
	}
	defer q.Close()
	collector := &Collector{Client: c, Queue: q, ClusterUID: string(ns.UID), Namespaces: o.Namespaces}
	uploader, identity, err := newUploader(o)
	if err != nil {
		return err
	}
	defer func() {
		if uploader.Client != nil {
			uploader.Client.CloseIdleConnections()
		}
	}()
	health := console.AgentHealth{Version: consoleversion.Version, QueueLimit: o.QueueBytes}
	if d, e := discovery.NewDiscoveryClientForConfig(cfg); e == nil {
		if v, e := d.ServerVersion(); e == nil {
			health.KubernetesVersion = v.GitVersion
		}
	}
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
		identity, err = refreshIdentity(o, uploader, identity)
		if err == nil {
			identity, err = renewIdentity(ctx, uploader, q, identity)
		}
		if err != nil {
			slog.Warn("client certificate renewal deferred")
		}
		if identity != nil {
			health.CertificateExpires = &identity.ExpiresAt
		}
		// Drain old work first; continue collecting during a central outage.
		if err = uploader.Drain(ctx, q); err != nil {
			slog.Warn("console upload deferred", "errorType", fmt.Sprintf("%T", err))
		}
		captureErr := collector.Capture(ctx)
		if captureErr != nil {
			slog.Warn("console collection deferred", "queueFull", errors.Is(captureErr, ErrQueueFull), "errorType", fmt.Sprintf("%T", captureErr))
		}
		uploadErr := uploader.Drain(ctx, q)
		if uploadErr != nil {
			slog.Warn("console upload deferred", "errorType", fmt.Sprintf("%T", uploadErr))
		}
		now := time.Now().UTC()
		if captureErr == nil {
			health.LastCollection = &now
		}
		if uploadErr == nil {
			health.LastUpload = &now
		}
		if size, e := q.Bytes(); e == nil {
			health.QueueBytes = size
		}
		if e := uploader.Heartbeat(ctx, health); e != nil {
			slog.Warn("console heartbeat deferred")
		}
		if o.Once {
			if captureErr != nil {
				return captureErr
			}
			return uploadErr
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
