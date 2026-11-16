package consoleinstall

import (
	"fmt"
	"strings"

	"github.com/optipod/optipod/internal/console"
)

func planDigest(p *Plan) string { return console.Digest(p) }

func imageValues(image string) (map[string]any, error) {
	if strings.ContainsAny(image, "@ \t\r\n") {
		return nil, fmt.Errorf("use a pinned image tag; digest references are not supported by this chart")
	}
	index := strings.LastIndex(image, ":")
	if index <= strings.LastIndex(image, "/") || strings.HasSuffix(image, ":latest") || index == len(image)-1 {
		return nil, fmt.Errorf("an explicitly versioned image is required")
	}
	return map[string]any{"repository": image[:index], "tag": image[index+1:], "pullPolicy": "IfNotPresent"}, nil
}

func agentValues(p *Plan, gitops bool) (map[string]any, error) {
	image, err := imageValues(p.Image)
	if err != nil {
		return nil, err
	}
	pulls := make([]map[string]string, 0, len(p.ImagePullSecrets))
	for _, name := range p.ImagePullSecrets {
		pulls = append(pulls, map[string]string{nameKey: name})
	}
	agent := map[string]any{enabledKey: true, "clusterID": p.Registration.ID, "serverURL": p.ServerURL, namespacesKey: p.Registration.Namespaces, "identity": map[string]bool{enabledKey: p.Registration.RequireMTLS}, "persistence": map[string]string{"storageClass": p.Registration.Installation.StorageClass}, "allowInsecureHTTP": strings.HasPrefix(p.ServerURL, "http://")}
	if gitops {
		agent[existingSecretKey] = p.Registration.Installation.Release + "-agent-auth" // pragma: allowlist secret - Kubernetes Secret reference or fixture name
		agent["secret"] = map[string]any{createKey: false}
	} else {
		agent["secret"] = map[string]any{createKey: true}
	}
	if p.ConsoleCASecret != "" {
		agent["caSecret"] = p.ConsoleCASecret // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	}
	if p.ProxySecret != "" {
		agent["env"] = proxyEnv(p.ProxySecret)
	}
	return map[string]any{"server": map[string]bool{enabledKey: false}, "agent": agent, "image": image, "global": map[string]any{"imagePullSecrets": pulls}}, nil
}

func controllerValues(p *Plan, imageOverride string) (map[string]any, error) {
	metrics := map[string]any{typeKey: p.Registration.MetricsSource}
	prom := map[string]any{"url": p.Registration.Installation.PrometheusURL}
	if p.PrometheusAuth == bearerAuth {
		prom["auth"] = map[string]any{typeKey: bearerAuth, bearerAuth: map[string]any{existingSecretKey: map[string]string{nameKey: p.PrometheusSecret, "key": "token"}}}
	}
	if p.PrometheusAuth == basicAuth {
		prom["auth"] = map[string]any{typeKey: basicAuth, basicAuth: map[string]any{existingSecretKey: map[string]string{nameKey: p.PrometheusSecret, "usernameKey": "username", "passwordKey": "password"}}}
	}
	if p.PrometheusCASecret != "" {
		prom["tls"] = map[string]any{enabledKey: true, existingSecretKey: map[string]string{nameKey: p.PrometheusCASecret, "caKey": "ca.crt", "certKey": "", "keyKey": ""}}
	}
	metrics["prometheus"] = prom
	values := map[string]any{"metricsProvider": metrics, "webhook": map[string]bool{enabledKey: p.EnableWebhook}, "certManager": map[string]bool{"install": false}}
	pulls := make([]map[string]string, 0, len(p.ImagePullSecrets))
	for _, name := range p.ImagePullSecrets {
		pulls = append(pulls, map[string]string{nameKey: name})
	}
	values["imagePullSecrets"] = pulls // pragma: allowlist secret - Kubernetes Secret reference or fixture name
	if imageOverride == "" {
		imageOverride = "ghcr.io/sagart-cactus/optipod:console-" + p.ConsoleVersion
	}
	if imageOverride != "" {
		image, err := imageValues(imageOverride)
		if err != nil {
			return nil, err
		}
		values["image"] = image
	}
	if p.ProxySecret != "" {
		values["extraEnv"] = proxyEnv(p.ProxySecret)
	}
	return values, nil
}

func proxyEnv(secret string) []map[string]any {
	env := make([]map[string]any, 0, 3)
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		env = append(env, map[string]any{nameKey: name, "valueFrom": map[string]any{"secretKeyRef": map[string]any{nameKey: secret, "key": name, "optional": true}}})
	}
	return env
}
