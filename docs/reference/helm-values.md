# Helm Values Reference

Complete reference for all Helm chart configuration values.

## Overview

The OptiPod Helm chart provides extensive configuration options for customizing the deployment. This reference documents all available values with their defaults, types, and descriptions.

## Installation

```bash
helm install optipod charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --values custom-values.yaml
```

## Global Settings

### nameOverride

Override the chart name.

**Type**: `string`  
**Default**: `""`

**Example**:
```yaml
nameOverride: "my-optipod"
```

### fullnameOverride

Override the full resource names.

**Type**: `string`  
**Default**: `""`

**Example**:
```yaml
fullnameOverride: "optipod-prod"
```

### namespaceOverride

Override the namespace for all resources.

**Type**: `string`  
**Default**: `""` (uses release namespace)

**Example**:
```yaml
namespaceOverride: "custom-namespace"
```

## Image Configuration

### image.repository

Container image repository.

**Type**: `string`  
**Default**: `"ghcr.io/sagart-cactus/optipod"`

**Example**:
```yaml
image:
  repository: "my-registry.io/optipod"
```

### image.pullPolicy

Image pull policy.

**Type**: `string`  
**Default**: `"IfNotPresent"`  
**Options**: `Always`, `IfNotPresent`, `Never`

**Example**:
```yaml
image:
  pullPolicy: Always
```

### image.tag

Image tag to use.

**Type**: `string`  
**Default**: `""` (uses Chart.appVersion)

**Example**:
```yaml
image:
  tag: "v1.5.3"
```

### imagePullSecrets

Secrets for pulling images from private registries.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
imagePullSecrets:
  - name: my-registry-secret
```

## Service Account

### serviceAccount.create

Create a service account.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
serviceAccount:
  create: true
```

### serviceAccount.annotations

Annotations for the service account.

**Type**: `object`  
**Default**: `{}`

**Example**:
```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/optipod"
```

### serviceAccount.name

Service account name to use.

**Type**: `string`  
**Default**: `""` (auto-generated)

**Example**:
```yaml
serviceAccount:
  name: "optipod-sa"
```

## RBAC

### rbac.create

Create RBAC resources.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
rbac:
  create: true
```

## Controller Configuration

### controller.replicaCount

Number of controller replicas.

**Type**: `integer`  
**Default**: `1`

**Example**:
```yaml
controller:
  replicaCount: 1
```

**Note**: Controller uses leader election, so only one replica is active at a time.

### controller.strategy

Deployment strategy for controller.

**Type**: `object`  
**Default**:
```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1
```

### controller.ports

Port configuration for controller.

**Type**: `object`  
**Default**:
```yaml
ports:
  health: 8081
  metrics: 8080
```

### controller.dryRun

Enable dry-run mode (no actual changes).

**Type**: `boolean`  
**Default**: `false`

**Example**:
```yaml
controller:
  dryRun: true
```

**Use case**: Testing without applying changes.

### controller.leaderElect

Enable leader election for high availability.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
controller:
  leaderElect: true
```

### controller.reconciliationInterval

Default reconciliation interval for policies.

**Type**: `string` (duration)  
**Default**: `"5m"`

**Example**:
```yaml
controller:
  reconciliationInterval: "10m"
```

### controller.enableHTTP2

Enable HTTP/2 for controller.

**Type**: `boolean`  
**Default**: `false`

**Example**:
```yaml
controller:
  enableHTTP2: false
```

### controller.priorityClassName

Priority class for controller pods.

**Type**: `string`  
**Default**: `""`

**Example**:
```yaml
controller:
  priorityClassName: "system-cluster-critical"
```

### controller.terminationGracePeriodSeconds

Grace period for pod termination.

**Type**: `integer`  
**Default**: `30`

**Example**:
```yaml
controller:
  terminationGracePeriodSeconds: 60
```

### controller.dnsPolicy

DNS policy for controller pods.

**Type**: `string`  
**Default**: `""` (uses cluster default)  
**Options**: `ClusterFirst`, `ClusterFirstWithHostNet`, `Default`, `None`

**Example**:
```yaml
controller:
  dnsPolicy: "ClusterFirst"
```

### controller.topologySpreadConstraints

Topology spread constraints for controller pods.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
controller:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/component: controller
```

### controller.resources

Resource requests and limits for controller.

**Type**: `object`  
**Default**:
```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

**Example**:
```yaml
controller:
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi
```

### controller.nodeSelector

Node selector for controller pods.

**Type**: `object`  
**Default**: `{}`

**Example**:
```yaml
controller:
  nodeSelector:
    node-role.kubernetes.io/control-plane: ""
```

### controller.tolerations

Tolerations for controller pods.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
controller:
  tolerations:
    - key: "node-role.kubernetes.io/control-plane"
      operator: "Exists"
      effect: "NoSchedule"
```

### controller.affinity

Affinity rules for controller pods.

**Type**: `object`  
**Default**: `{}`

**Example**:
```yaml
controller:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists
```

### controller.podAnnotations

Annotations for controller pods.

**Type**: `object`  
**Default**: `{}`

**Example**:
```yaml
controller:
  podAnnotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
```

### controller.podSecurityContext

Security context for controller pods.

**Type**: `object`  
**Default**:
```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
```

### controller.securityContext

Security context for controller container.

**Type**: `object`  
**Default**:
```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
```

### controller.livenessProbe

Liveness probe configuration.

**Type**: `object`  
**Default**:
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8081
  initialDelaySeconds: 15
  periodSeconds: 20
```

### controller.readinessProbe

Readiness probe configuration.

**Type**: `object`  
**Default**:
```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10
```


## Webhook Configuration

### webhook.enabled

Enable the mutating webhook.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
webhook:
  enabled: true
```

**Note**: Requires cert-manager for certificate management.

### webhook.port

Webhook server port.

**Type**: `integer`  
**Default**: `9443`

**Example**:
```yaml
webhook:
  port: 9443
```

### webhook.ports

Port configuration for webhook metrics and health.

**Type**: `object`  
**Default**:
```yaml
ports:
  health: 8081
  metrics: 8080
```

### webhook.certDir

Certificate directory for webhook TLS.

**Type**: `string`  
**Default**: `"/tmp/k8s-webhook-server/serving-certs"`

**Example**:
```yaml
webhook:
  certDir: "/tmp/k8s-webhook-server/serving-certs"
```

### webhook.failurePolicy

Webhook failure policy.

**Type**: `string`  
**Default**: `"Ignore"`  
**Options**: `Ignore`, `Fail`

**Values**:
- `Ignore` - Allow pod creation even if webhook fails (recommended for initial setup)
- `Fail` - Block pod creation if webhook fails (stricter enforcement)

**Example**:
```yaml
webhook:
  failurePolicy: Ignore
```

### webhook.timeoutSeconds

Webhook timeout in seconds.

**Type**: `integer`  
**Default**: `10`

**Example**:
```yaml
webhook:
  timeoutSeconds: 10
```

### webhook.namespaceSelector

Namespace selector for webhook.

**Type**: `object`  
**Default**:
```yaml
namespaceSelector:
  matchExpressions:
  - key: name
    operator: NotIn
    values: ["kube-system", "kube-public", "kube-node-lease"]
  - key: control-plane
    operator: DoesNotExist
```

**Example**:
```yaml
webhook:
  namespaceSelector:
    matchLabels:
      optipod-webhook: "enabled"
```

### webhook.objectSelector

Object selector for webhook.

**Type**: `object`  
**Default**: `{}`

**Note**: Kubernetes objectSelector only supports label matching, not annotations. OptiPod filters by annotations in webhook code.

### webhook.service

Webhook service configuration.

**Type**: `object`  
**Default**:
```yaml
service:
  type: ClusterIP
  port: 443
  targetPort: 9443
```

### webhook.deployment.replicaCount

Number of webhook replicas.

**Type**: `integer`  
**Default**: `2`

**Example**:
```yaml
webhook:
  deployment:
    replicaCount: 3
```

**Recommendation**: Use 2+ replicas for high availability.

### webhook.deployment.strategy

Deployment strategy for webhook.

**Type**: `object`  
**Default**:
```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1
```

### webhook.deployment.resources

Resource requests and limits for webhook.

**Type**: `object`  
**Default**:
```yaml
resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 64Mi
```

### webhook.deployment.affinity

Affinity rules for webhook pods.

**Type**: `object`  
**Default**:
```yaml
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchExpressions:
          - key: app.kubernetes.io/component
            operator: In
            values:
            - webhook
        topologyKey: kubernetes.io/hostname
```

**Note**: Default affinity spreads webhook pods across nodes for high availability.

### webhook.pdb.enabled

Enable Pod Disruption Budget for webhook.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
webhook:
  pdb:
    enabled: true
```

### webhook.pdb.minAvailable

Minimum available webhook pods.

**Type**: `integer`  
**Default**: `1`

**Example**:
```yaml
webhook:
  pdb:
    minAvailable: 1
```

### webhook.networkPolicy.enabled

Enable network policy for webhook.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
webhook:
  networkPolicy:
    enabled: true
```

### webhook.networkPolicy.ingress

Ingress rules for webhook network policy.

**Type**: `array`  
**Default**:
```yaml
ingress:
  - from:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 9443
```

## Certificate Manager

### certManager.install

Install cert-manager as a subchart.

**Type**: `boolean`  
**Default**: `true`

**Values**:
- `true` - Install cert-manager with OptiPod
- `false` - Use existing cert-manager in cluster

**Example**:
```yaml
certManager:
  install: false  # Use existing cert-manager
```

### certManager.installCRDs

Install cert-manager CRDs.

**Type**: `boolean`  
**Default**: `true`

**Note**: Only used if `certManager.install` is `true`.

### certManager.issuer.kind

Issuer type.

**Type**: `string`  
**Default**: `"Issuer"`  
**Options**: `Issuer`, `ClusterIssuer`

**Example**:
```yaml
certManager:
  issuer:
    kind: ClusterIssuer
```

### certManager.issuer.name

Issuer name.

**Type**: `string`  
**Default**: `"optipod-selfsigned-issuer"`

**Example**:
```yaml
certManager:
  issuer:
    name: "my-issuer"
```

### certManager.issuer.selfSigned

Use self-signed issuer.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
certManager:
  issuer:
    selfSigned: true
```

### certManager.certificate.secretName

Secret name for webhook certificate.

**Type**: `string`  
**Default**: `"webhook-server-certs"`

**Example**:
```yaml
certManager:
  certificate:
    secretName: "optipod-webhook-certs"  # pragma: allowlist secret
```

### certManager.certificate.duration

Certificate duration.

**Type**: `string` (duration)  
**Default**: `"8760h"` (1 year)

**Example**:
```yaml
certManager:
  certificate:
    duration: "4380h"  # 6 months
```

### certManager.certificate.renewBefore

Renew certificate before expiry.

**Type**: `string` (duration)  
**Default**: `"720h"` (30 days)

**Example**:
```yaml
certManager:
  certificate:
    renewBefore: "1440h"  # 60 days
```

### certManager.certificate.privateKey

Private key configuration.

**Type**: `object`  
**Default**:
```yaml
privateKey:
  algorithm: RSA
  size: 2048
```

## Metrics Configuration

### metrics.enabled

Enable metrics endpoint.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
metrics:
  enabled: true
```

### metrics.secure

Enable TLS for metrics endpoint.

**Type**: `boolean`  
**Default**: `true`

**Example**:
```yaml
metrics:
  secure: true
```

### metrics.serviceMonitor.enabled

Enable Prometheus ServiceMonitor.

**Type**: `boolean`  
**Default**: `false`

**Example**:
```yaml
metrics:
  serviceMonitor:
    enabled: true
```

**Note**: Requires Prometheus Operator.

### metrics.serviceMonitor.interval

Scrape interval for ServiceMonitor.

**Type**: `string` (duration)  
**Default**: `"30s"`

**Example**:
```yaml
metrics:
  serviceMonitor:
    interval: "15s"
```

### metrics.serviceMonitor.scrapeTimeout

Scrape timeout for ServiceMonitor.

**Type**: `string` (duration)  
**Default**: `"10s"`

**Example**:
```yaml
metrics:
  serviceMonitor:
    scrapeTimeout: "5s"
```

## Metrics Provider

### metricsProvider.type

Metrics provider type.

**Type**: `string`  
**Default**: `"metrics-server"`  
**Options**: `metrics-server`, `prometheus`

**Example**:
```yaml
metricsProvider:
  type: "prometheus"
```

### metricsProvider.prometheus.url

Prometheus server URL.

**Type**: `string`  
**Default**: `"http://prometheus:9090"`

**Example**:
```yaml
metricsProvider:
  prometheus:
    url: "http://prometheus-server.monitoring:9090"
```

### metricsProvider.prometheus.auth.type

Prometheus authentication type.

**Type**: `string`  
**Default**: `"none"`  
**Options**: `none`, `basic`, `bearer`

**Example**:
```yaml
metricsProvider:
  prometheus:
    auth:
      type: "basic"
```

### metricsProvider.prometheus.auth.basic

Basic authentication configuration.

**Type**: `object`  
**Default**:
```yaml
basic:
  username: ""
  password: ""
  existingSecret:
    name: ""
    usernameKey: "username"
    passwordKey: "password"  # pragma: allowlist secret
```

**Example**:
```yaml
metricsProvider:
  prometheus:
    auth:
      type: "basic"
      basic:
        existingSecret:
          name: "prometheus-auth"
          usernameKey: "username"
          passwordKey: "password"  # pragma: allowlist secret
```

### metricsProvider.prometheus.auth.bearer

Bearer token authentication configuration.

**Type**: `object`  
**Default**:
```yaml
bearer:
  token: ""
  existingSecret:
    name: ""
    key: "token"
```

**Example**:
```yaml
metricsProvider:
  prometheus:
    auth:
      type: "bearer"
      bearer:
        existingSecret:
          name: "prometheus-token"
          key: "token"
```

### metricsProvider.prometheus.tls

TLS configuration for Prometheus.

**Type**: `object`  
**Default**:
```yaml
tls:
  enabled: false
  insecureSkipVerify: false
  existingSecret:
    name: ""
    caKey: "ca.crt"
    certKey: "tls.crt"
    keyKey: "tls.key"
```

**Example**:
```yaml
metricsProvider:
  prometheus:
    tls:
      enabled: true
      existingSecret:
        name: "prometheus-tls"
```

### metricsProvider.prometheus.timeout

HTTP client timeout for Prometheus.

**Type**: `string` (duration)  
**Default**: `"30s"`

**Example**:
```yaml
metricsProvider:
  prometheus:
    timeout: "60s"
```

### metricsProvider.metricsServer.samplingInterval

Background sampling interval for metrics-server.

**Type**: `string` (duration)  
**Default**: `"5m"`

**Example**:
```yaml
metricsProvider:
  metricsServer:
    samplingInterval: "10m"
```

### metricsProvider.metricsServer.maxSamplesPerTarget

Maximum samples to cache per target.

**Type**: `integer`  
**Default**: `2880`

**Example**:
```yaml
metricsProvider:
  metricsServer:
    maxSamplesPerTarget: 5000
```

### metricsProvider.metricsServer.minSamplesRequired

Minimum samples required for recommendations.

**Type**: `integer`  
**Default**: `10`

**Example**:
```yaml
metricsProvider:
  metricsServer:
    minSamplesRequired: 20
```

### metricsProvider.metricsServer.targetTTL

Target eviction TTL.

**Type**: `string` (duration)  
**Default**: `"15m"`

**Example**:
```yaml
metricsProvider:
  metricsServer:
    targetTTL: "30m"
```

## Observability

### logging.level

Log level.

**Type**: `string`  
**Default**: `"info"`  
**Options**: `debug`, `info`, `warn`, `error`

**Example**:
```yaml
logging:
  level: "debug"
```

### logging.format

Log format.

**Type**: `string`  
**Default**: `"json"`  
**Options**: `json`, `console`

**Example**:
```yaml
logging:
  format: "console"
```

## Additional Configuration

### extraVolumes

Additional volumes for controller.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
extraVolumes:
  - name: config
    configMap:
      name: optipod-config
```

### extraVolumeMounts

Additional volume mounts for controller.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
extraVolumeMounts:
  - name: config
    mountPath: /etc/optipod
    readOnly: true
```

### extraEnv

Additional environment variables for controller.

**Type**: `array`  
**Default**: `[]`

**Example**:
```yaml
extraEnv:
  - name: CUSTOM_VAR
    value: "custom-value"
  - name: SECRET_VAR
    valueFrom:
      secretKeyRef:
        name: my-secret
        key: secret-key
```

## Common Configuration Examples

### Minimal Configuration

```yaml
# Minimal setup with defaults
controller:
  replicaCount: 1

webhook:
  enabled: true

metricsProvider:
  type: "metrics-server"
```

### Production Configuration

```yaml
# Production setup with HA and monitoring
controller:
  replicaCount: 1
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-role.kubernetes.io/control-plane
                operator: Exists

webhook:
  enabled: true
  deployment:
    replicaCount: 3
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
      requests:
        cpu: 100m
        memory: 128Mi
  pdb:
    enabled: true
    minAvailable: 2

metrics:
  serviceMonitor:
    enabled: true

metricsProvider:
  type: "prometheus"
  prometheus:
    url: "http://prometheus-server.monitoring:9090"
    auth:
      type: "basic"
      basic:
        existingSecret:
          name: "prometheus-auth"

logging:
  level: "info"
  format: "json"
```

### GitOps Configuration

```yaml
# GitOps-friendly setup
webhook:
  enabled: true
  failurePolicy: Ignore
  deployment:
    replicaCount: 2

metricsProvider:
  type: "prometheus"
  prometheus:
    url: "http://prometheus:9090"

certManager:
  install: false  # Use existing cert-manager
```

### Development Configuration

```yaml
# Development setup
controller:
  dryRun: true
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
    requests:
      cpu: 50m
      memory: 64Mi

webhook:
  enabled: false

metricsProvider:
  type: "metrics-server"

logging:
  level: "debug"
  format: "console"
```

## Related Documentation

- [Installation Guide](../getting-started/installation.md) - Installation instructions
- [CRD Specification](crd-spec.md) - OptimizationPolicy field reference
- [Prometheus Authentication](../PROMETHEUS_AUTHENTICATION.md) - Prometheus auth setup
- [Webhook Configuration](../advanced/webhook-config.md) - Advanced webhook configuration
