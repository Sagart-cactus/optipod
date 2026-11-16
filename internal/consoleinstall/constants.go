package consoleinstall

const (
	probeName         = "probe"
	getVerb           = "get"
	listVerb          = "list"
	createKey         = "create"
	rbacGroup         = "rbac.authorization.k8s.io"
	newControllerMode = "new"
	contextFlag       = "--kube-context"
	nameKey           = "name"
	httpScheme        = "http"
	bearerAuth        = "bearer"
	basicAuth         = "basic"
	appsGroup         = "apps"
	namespacesKey     = "namespaces"
	enabledKey        = "enabled"
	typeKey           = "type"
	existingSecretKey = "existingSecret" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
)

const (
	podsResource   = "pods"
	httpsScheme    = "https"
	kubeconfigFlag = "--kubeconfig"
)

const namespaceFlag = "--namespace"
