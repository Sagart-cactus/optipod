# Testing cert-manager Integration

## Prerequisites

- Kubernetes cluster (minikube, kind, or cloud provider)
- Helm 3.x installed
- kubectl configured

## Test Scenarios

### Scenario 1: Install with Bundled cert-manager

This installs cert-manager as a subchart alongside OptipPod.

```bash
# Install with cert-manager
helm install optipod ./charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true \
  --wait --timeout=10m

# Verify installation
kubectl get pods -n optipod-system
kubectl get crd | grep cert-manager
kubectl get certificate -n optipod-system
kubectl get issuer -n optipod-system

# Check bootstrap job logs
kubectl logs -n optipod-system job/optipod-cert-manager-bootstrap
```

### Scenario 2: Use Existing cert-manager

This assumes cert-manager is already installed cluster-wide.

```bash
# First, install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=available --timeout=300s \
  deployment/cert-manager -n cert-manager

# Install OptipPod (uses existing cert-manager)
helm install optipod ./charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --wait --timeout=5m

# Verify installation
kubectl get pods -n optipod-system
kubectl get certificate -n optipod-system
kubectl get issuer -n optipod-system
```

### Scenario 3: Upgrade from Bundled to External

```bash
# Start with bundled cert-manager
helm install optipod ./charts/optipod \
  --namespace optipod-system \
  --create-namespace \
  --set certManager.install=true \
  --wait

# Install cluster-wide cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml

# Upgrade to use external cert-manager
helm upgrade optipod ./charts/optipod \
  --namespace optipod-system \
  --set certManager.install=false \
  --reuse-values \
  --wait
```

## Verification Steps

### Check cert-manager CRDs

```bash
kubectl get crd | grep cert-manager
```

Expected output:
```
certificaterequests.cert-manager.io
certificates.cert-manager.io
challenges.acme.cert-manager.io
clusterissuers.cert-manager.io
issuers.cert-manager.io
orders.acme.cert-manager.io
```

### Check cert-manager Pods

```bash
# If installed as subchart
kubectl get pods -n optipod-system | grep cert-manager

# If installed cluster-wide
kubectl get pods -n cert-manager
```

### Check Certificate Status

```bash
kubectl get certificate -n optipod-system
kubectl describe certificate -n optipod-system
```

Expected status: `Ready=True`

### Check Issuer Status

```bash
kubectl get issuer -n optipod-system
kubectl describe issuer -n optipod-system
```

Expected status: `Ready=True`

### Check Secret

```bash
kubectl get secret webhook-server-certs -n optipod-system
kubectl describe secret webhook-server-certs -n optipod-system
```

Should contain `tls.crt` and `tls.key`

## Troubleshooting

### Bootstrap Job Failed

```bash
# Check job status
kubectl get job -n optipod-system

# View logs
kubectl logs -n optipod-system job/optipod-cert-manager-bootstrap

# Check events
kubectl get events -n optipod-system --sort-by='.lastTimestamp'
```

### CRDs Not Found

```bash
# Verify cert-manager installation
kubectl get deployment -n cert-manager
kubectl get deployment -n optipod-system | grep cert-manager

# Check CRD installation
kubectl get crd certificates.cert-manager.io
```

### Certificate Not Ready

```bash
# Check certificate status
kubectl describe certificate -n optipod-system

# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager
# or for subchart
kubectl logs -n optipod-system deployment/optipod-cert-manager
```

## Cleanup

```bash
# Uninstall OptipPod
helm uninstall optipod -n optipod-system

# Delete namespace
kubectl delete namespace optipod-system

# If you installed cluster-wide cert-manager
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.2/cert-manager.yaml
```
