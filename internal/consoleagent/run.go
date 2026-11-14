package consoleagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	api "github.com/optipod/optipod/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Options struct {
	Kubeconfig, ClusterID, QueueDir, URL, TokenFile string
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
	uploader := &Uploader{URL: o.URL, TokenFile: o.TokenFile, AllowHTTP: o.AllowHTTP}
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
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
