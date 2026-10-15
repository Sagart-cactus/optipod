package framework

import (
	"fmt"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	optipodv1alpha1 "github.com/optipod/optipod/api/v1alpha1"
)

// Clients groups the typed Kubernetes clients the harness uses against the
// owned cluster. Every client derives from the private kubeconfig's rest.Config
// only; the developer's default kubeconfig is never consulted.
type Clients struct {
	// Ctrl is a typed controller-runtime client with the Kubernetes and OptiPod
	// schemes registered.
	Ctrl client.Client
	// Kube is the typed client-go clientset.
	Kube kubernetes.Interface
	// Ext is the apiextensions clientset used for CRD condition checks.
	Ext *apiextensionsclient.Clientset

	restConfig *rest.Config
}

// Scheme returns the harness runtime scheme (Kubernetes core plus OptiPod API).
func Scheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(optipodv1alpha1.AddToScheme(scheme))
	return scheme
}

// NewClients builds typed clients from the given rest.Config, which must come
// from Cluster.RESTConfig() (the private kubeconfig).
func NewClients(cfg *rest.Config) (*Clients, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rest config is nil; derive it from the private kubeconfig")
	}
	ctrlClient, err := client.New(cfg, client.Options{Scheme: Scheme()})
	if err != nil {
		return nil, fmt.Errorf("build controller-runtime client: %w", err)
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}
	extClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build apiextensions clientset: %w", err)
	}
	return &Clients{
		Ctrl:       ctrlClient,
		Kube:       kubeClient,
		Ext:        extClient,
		restConfig: cfg,
	}, nil
}

// RESTConfig returns the private rest configuration the clients were built from.
func (c *Clients) RESTConfig() *rest.Config { return c.restConfig }
