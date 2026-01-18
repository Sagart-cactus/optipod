# OptiPod Helm Chart Configuration Reference

This document provides a comprehensive reference for all configuration options available in the OptiPod Helm chart.

## Table of Contents
- [Controller Configuration](#controller-configuration)
- [Webhook Configuration](#webhook-configuration)
- [Metrics Configuration](#metrics-configuration)
- [Metrics Provider Configuration](#metrics-provider-configuration)
- [Certificate Manager Configuration](#certificate-manager-configuration)
- [Common Configuration](#common-configuration)

---

## Controller Configuration

### Basic Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `controller.replicaCount` | Number of controller replicas | `1` | int |
| `controller.dryRun` | Enable dry-run mode (no actual changes) | `false` | bool |
| `controller.leaderElect` | Enable leader election | `true` | bool |
| `controller.reconciliationInterval` | Policy reconciliation frequency | `"5m"` | duration |
| `controller.enableHTTP2` | Enable HTTP/2 support | `false` | bool |

### Port Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `controller.ports.health` | Health probe port | `8081` | int |
| `controller.ports.metrics` | Metrics endpoint port | `8080` | int |

### Deployment Strategy

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `controller.strategy.type` | Deployment strategy type | `RollingUpdate` | string |
| `controller.strategy.rollingUpdate.maxUnavailable` | Max unavailable pods during update | `0` | int |
| `controller.strategy.rollingUpdate.maxSurge` | Max surge pods during update | `1` | int |

### Pod Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `controller.priorityClassName` | Priority class for controller pods | `""` | string |
| `controller.terminationGracePeriodSeconds` | Graceful shutdown timeout | `30` | int |
| `controller.dnsPolicy` | DNS policy for controller pods | `""` | string |
| `controller.podAnnotations` | Annotations for controller pods | `{}` | object |
| `controller.nodeSelector` | Node selector for controller pods | `{}` | object |
| `controller.tolerations` | Tolerations for controller pods | `[]` | array |
| `controller.affinity` | Affinity rules for controller pods | `{}` | object |
| `controller.topologySpreadConstraints` | Topology spread constraints | `[]` | array |

### Resource Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `512Mi` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |

### Security Context

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `controller.podSecurityContext.runAsUser` | User ID | `65532` |
| `controller.podSecurityContext.fsGroup` | Filesystem group | `65532` |
| `controller.securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `controller.securityContext.capabilities.drop` | Dropped capabilities | `["ALL"]` |
| `controller.securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `true` |

### Health Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.livenessProbe.httpGet.path` | Liveness probe path | `/healthz` |
| `controller.livenessProbe.httpGet.port` | Liveness probe port | `8081` |
| `controller.livenessProbe.initialDelaySeconds` | Initial delay | `15` |
| `controller.livenessProbe.periodSeconds` | Check period | `20` |
| `controller.readinessProbe.httpGet.path` | Readiness probe path | `/readyz` |
| `controller.readinessProbe.httpGet.port` | Readiness probe port | `8081` |
| `controller.readinessProbe.initialDelaySeconds` | Initial delay | `5` |
| `controller.readinessProbe.periodSeconds` | Check period | `10` |

---

## Webhook Configuration

### Basic Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.enabled` | Enable mutating webhook | `true` | bool |
| `webhook.port` | Webhook server port | `9443` | int |
| `webhook.certDir` | Certificate directory path | `/tmp/k8s-webhook-server/serving-certs` | string |
| `webhook.failurePolicy` | Webhook failure policy (Ignore/Fail) | `Ignore` | string |
| `webhook.timeoutSeconds` | Webhook timeout | `10` | int |

### Port Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.ports.health` | Health probe port | `8081` | int |
| `webhook.ports.metrics` | Metrics endpoint port | `8080` | int |

### Deployment Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.deployment.replicaCount` | Number of webhook replicas | `2` | int |
| `webhook.deployment.leaderElect` | Enable leader election | `false` | bool |
| `webhook.deployment.enableHTTP2` | Enable HTTP/2 support | `false` | bool |
| `webhook.deployment.priorityClassName` | Priority class | `""` | string |
| `webhook.deployment.terminationGracePeriodSeconds` | Graceful shutdown timeout | `30` | int |
| `webhook.deployment.dnsPolicy` | DNS policy | `""` | string |

### Deployment Strategy

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.deployment.strategy.type` | Deployment strategy type | `RollingUpdate` | string |
| `webhook.deployment.strategy.rollingUpdate.maxUnavailable` | Max unavailable pods | `0` | int |
| `webhook.deployment.strategy.rollingUpdate.maxSurge` | Max surge pods | `1` | int |

### Service Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.service.type` | Service type | `ClusterIP` | string |
| `webhook.service.port` | Service port | `443` | int |
| `webhook.service.targetPort` | Target port | `9443` | int |

### Selectors

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.namespaceSelector` | Namespace selector for webhook | See values.yaml |
| `webhook.objectSelector` | Object selector for webhook | `{}` |

### Pod Disruption Budget

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.pdb.enabled` | Enable PDB | `true` | bool |
| `webhook.pdb.minAvailable` | Minimum available pods | `1` | int |

### Network Policy

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `webhook.networkPolicy.enabled` | Enable network policy | `true` | bool |
| `webhook.networkPolicy.ingress` | Ingress rules | See values.yaml | array |

### Health Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.deployment.livenessProbe.httpGet.path` | Liveness probe path | `/healthz` |
| `webhook.deployment.livenessProbe.httpGet.port` | Liveness probe port | `8081` |
| `webhook.deployment.livenessProbe.initialDelaySeconds` | Initial delay | `15` |
| `webhook.deployment.livenessProbe.periodSeconds` | Check period | `20` |
| `webhook.deployment.readinessProbe.httpGet.path` | Readiness probe path | `/readyz` |
| `webhook.deployment.readinessProbe.httpGet.port` | Readiness probe port | `8081` |
| `webhook.deployment.readinessProbe.initialDelaySeconds` | Initial delay | `5` |
| `webhook.deployment.readinessProbe.periodSeconds` | Check period | `10` |

---

## Metrics Configuration

### Basic Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metrics.enabled` | Enable metrics | `true` | bool |
| `metrics.secure` | Enable HTTPS for metrics | `true` | bool |

### TLS Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metrics.tls.certPath` | TLS certificate directory | `""` | string |
| `metrics.tls.certName` | TLS certificate filename | `"tls.crt"` | string |
| `metrics.tls.keyName` | TLS key filename | `"tls.key"` | string |

### ServiceMonitor

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metrics.serviceMonitor.enabled` | Enable Prometheus ServiceMonitor | `false` | bool |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` | duration |
| `metrics.serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` | duration |

---

## Metrics Provider Configuration

### Provider Selection

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.type` | Metrics provider type | `"metrics-server"` | string |

**Options**: `"metrics-server"` or `"prometheus"`

### Prometheus Configuration

Used when `metricsProvider.type` is `"prometheus"`.

#### Basic Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.prometheus.url` | Prometheus server URL | `"http://prometheus:9090"` | string |
| `metricsProvider.prometheus.timeout` | HTTP client timeout | `"30s"` | duration |

#### Authentication

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.prometheus.auth.type` | Auth type (none/basic/bearer) | `"none"` | string |

##### Basic Authentication

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.prometheus.auth.basic.username` | Username (not recommended) | `""` | string |
| `metricsProvider.prometheus.auth.basic.password` | Password (not recommended) | `""` | string |
| `metricsProvider.prometheus.auth.basic.existingSecret.name` | Secret name | `""` | string |
| `metricsProvider.prometheus.auth.basic.existingSecret.usernameKey` | Username key in secret | `"username"` | string |
| `metricsProvider.prometheus.auth.basic.existingSecret.passwordKey` | Password key in secret | `"password"` | string |

##### Bearer Token Authentication

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.prometheus.auth.bearer.token` | Bearer token (not recommended) | `""` | string |
| `metricsProvider.prometheus.auth.bearer.existingSecret.name` | Secret name | `""` | string |
| `metricsProvider.prometheus.auth.bearer.existingSecret.key` | Token key in secret | `"token"` | string |

#### TLS Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.prometheus.tls.enabled` | Enable TLS | `false` | bool |
| `metricsProvider.prometheus.tls.insecureSkipVerify` | Skip TLS verification | `false` | bool |
| `metricsProvider.prometheus.tls.existingSecret.name` | Secret name | `""` | string |
| `metricsProvider.prometheus.tls.existingSecret.caKey` | CA cert key in secret | `"ca.crt"` | string |
| `metricsProvider.prometheus.tls.existingSecret.certKey` | Client cert key in secret | `"tls.crt"` | string |
| `metricsProvider.prometheus.tls.existingSecret.keyKey` | Client key key in secret | `"tls.key"` | string |

### Metrics-Server Configuration

Used when `metricsProvider.type` is `"metrics-server"`.

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `metricsProvider.metricsServer.samplingInterval` | Background sampling interval | `"5m"` | duration |
| `metricsProvider.metricsServer.maxSamplesPerTarget` | Max samples to cache per target | `2880` | int |
| `metricsProvider.metricsServer.minSamplesRequired` | Min samples for recommendations | `10` | int |
| `metricsProvider.metricsServer.targetTTL` | Target eviction TTL | `"15m"` | duration |

---

## Certificate Manager Configuration

### Basic Settings

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `certManager.install` | Install cert-manager subchart | `true` | bool |
| `certManager.installCRDs` | Install cert-manager CRDs | `true` | bool |

### Issuer Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `certManager.issuer.kind` | Issuer kind (Issuer/ClusterIssuer) | `Issuer` | string |
| `certManager.issuer.name` | Issuer name | `optipod-selfsigned-issuer` | string |
| `certManager.issuer.selfSigned` | Use self-signed issuer | `true` | bool |

### Certificate Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `certManager.certificate.secretName` | Certificate secret name | `webhook-server-certs` | string |
| `certManager.certificate.duration` | Certificate duration | `8760h` | duration |
| `certManager.certificate.renewBefore` | Renew before expiry | `720h` | duration |
| `certManager.certificate.organization` | Subject organization | `optipod` | string |
| `certManager.certificate.dnsNames` | DNS names (auto-generated if empty) | `[]` | array |
| `certManager.certificate.privateKey.algorithm` | Private key algorithm | `RSA` | string |
| `certManager.certificate.privateKey.size` | Private key size | `2048` | int |

---

## Common Configuration

### Image Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `image.repository` | Image repository | `ghcr.io/sagart-cactus/optipod` | string |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` | string |
| `image.tag` | Image tag (defaults to Chart.appVersion) | `""` | string |
| `imagePullSecrets` | Image pull secrets | `[]` | array |

### Namespace Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `namespaceOverride` | Override namespace | `""` | string |
| `nameOverride` | Override chart name | `""` | string |
| `fullnameOverride` | Override full name | `""` | string |

### Service Account

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `serviceAccount.create` | Create service account | `true` | bool |
| `serviceAccount.annotations` | Service account annotations | `{}` | object |
| `serviceAccount.name` | Service account name | `""` | string |

### RBAC

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `rbac.create` | Create RBAC resources | `true` | bool |

### Logging

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `logging.level` | Log level (debug/info/error) | `info` | string |
| `logging.format` | Log format (console/json) | `json` | string |

### Extra Configuration

| Parameter | Description | Default | Type |
|-----------|-------------|---------|------|
| `extraVolumes` | Additional volumes | `[]` | array |
| `extraVolumeMounts` | Additional volume mounts | `[]` | array |
| `extraEnv` | Additional environment variables | `[]` | array |

---

## Example Configurations

### Production Configuration

```yaml
controller:
  replicaCount: 2
  priorityClassName: system-cluster-critical
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: controller
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi

webhook:
  deployment:
    replicaCount: 3
    priorityClassName: system-cluster-critical
    topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/component: webhook

metricsProvider:
  type: prometheus
  prometheus:
    url: https://prometheus.monitoring.svc:9090
    auth:
      type: basic
      basic:
        existingSecret:
          name: prometheus-credentials
          usernameKey: username
          passwordKey: password
    tls:
      enabled: true
      existingSecret:
        name: prometheus-tls
```

### Development Configuration

```yaml
controller:
  dryRun: true
  reconciliationInterval: "1m"
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi

webhook:
  enabled: false

logging:
  level: debug
  format: console

metricsProvider:
  type: metrics-server
  metricsServer:
    samplingInterval: "1m"
    minSamplesRequired: 3
```

### High Availability Configuration

```yaml
controller:
  replicaCount: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: controller
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/component: controller

webhook:
  deployment:
    replicaCount: 5
    topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/component: webhook
  pdb:
    enabled: true
    minAvailable: 2
```
