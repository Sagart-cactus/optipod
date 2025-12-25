#!/bin/bash
set -e

CLUSTER_NAME="optipod-dev"
NAMESPACE="optipod-workloads"

echo "=== Setting up OptiPod Development Cluster ==="

# Create kind cluster if it doesn't exist
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo "Creating kind cluster: ${CLUSTER_NAME}"
    kind create cluster --name ${CLUSTER_NAME} --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF
else
    echo "Cluster ${CLUSTER_NAME} already exists"
fi

# Set kubectl context
kubectl config use-context kind-${CLUSTER_NAME}

echo ""
echo "=== Installing Metrics Server ==="
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Patch metrics-server for kind
kubectl patch deployment metrics-server -n kube-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--kubelet-insecure-tls"
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--kubelet-preferred-address-types=InternalIP"
  },
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--metric-resolution=15s"
  }
]'

echo "Waiting for metrics-server to be ready..."
kubectl wait --for=condition=available --timeout=120s deployment/metrics-server -n kube-system

echo ""
echo "=== Creating Workload Namespace ==="
kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace ${NAMESPACE} environment=development --overwrite

echo ""
echo "=== Deploying Sample Workloads ==="

# Deployment 1: NGINX web server (low resource usage)
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-web
  namespace: ${NAMESPACE}
  labels:
    app: nginx-web
    optimize: "true"
    workload-type: web
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx-web
  template:
    metadata:
      labels:
        app: nginx-web
    spec:
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "500m"
            memory: "256Mi"
        ports:
        - containerPort: 80
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: nginx-web
  namespace: ${NAMESPACE}
spec:
  selector:
    app: nginx-web
  ports:
  - port: 80
    targetPort: 80
EOF

# Deployment 2: Redis cache (medium resource usage)
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-cache
  namespace: ${NAMESPACE}
  labels:
    app: redis-cache
    optimize: "true"
    workload-type: cache
spec:
  replicas: 2
  selector:
    matchLabels:
      app: redis-cache
  template:
    metadata:
      labels:
        app: redis-cache
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        resources:
          requests:
            cpu: "200m"
            memory: "256Mi"
          limits:
            cpu: "1000m"
            memory: "512Mi"
        ports:
        - containerPort: 6379
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 999
          seccompProfile:
            type: RuntimeDefault
---
apiVersion: v1
kind: Service
metadata:
  name: redis-cache
  namespace: ${NAMESPACE}
spec:
  selector:
    app: redis-cache
  ports:
  - port: 6379
    targetPort: 6379
EOF

# StatefulSet: PostgreSQL database
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres-db
  namespace: ${NAMESPACE}
  labels:
    app: postgres-db
    optimize: "true"
    workload-type: database
spec:
  serviceName: postgres-db
  replicas: 2
  selector:
    matchLabels:
      app: postgres-db
  template:
    metadata:
      labels:
        app: postgres-db
    spec:
      containers:
      - name: postgres
        image: postgres:16-alpine
        env:
        - name: POSTGRES_PASSWORD
          value: "devpassword"
        - name: PGDATA
          value: /var/lib/postgresql/data/pgdata
        resources:
          requests:
            cpu: "500m"
            memory: "512Mi"
          limits:
            cpu: "2000m"
            memory: "1Gi"
        ports:
        - containerPort: 5432
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 999
          seccompProfile:
            type: RuntimeDefault
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
---
apiVersion: v1
kind: Service
metadata:
  name: postgres-db
  namespace: ${NAMESPACE}
spec:
  clusterIP: None
  selector:
    app: postgres-db
  ports:
  - port: 5432
    targetPort: 5432
EOF

# DaemonSet: Log collector (runs on every node)
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: log-collector
  namespace: ${NAMESPACE}
  labels:
    app: log-collector
    optimize: "true"
    workload-type: logging
spec:
  selector:
    matchLabels:
      app: log-collector
  template:
    metadata:
      labels:
        app: log-collector
    spec:
      containers:
      - name: fluentd
        image: fluent/fluentd:v1.16-1
        resources:
          requests:
            cpu: "100m"
            memory: "200Mi"
          limits:
            cpu: "500m"
            memory: "400Mi"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 100
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: varlog
          mountPath: /var/log
          readOnly: true
      volumes:
      - name: varlog
        hostPath:
          path: /var/log
EOF

# Deployment 3: API server with variable load
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  namespace: ${NAMESPACE}
  labels:
    app: api-server
    optimize: "true"
    workload-type: api
    auto-update: "true"
spec:
  replicas: 4
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
    spec:
      containers:
      - name: api
        image: nginx:1.25-alpine
        resources:
          requests:
            cpu: "250m"
            memory: "256Mi"
          limits:
            cpu: "1000m"
            memory: "512Mi"
        ports:
        - containerPort: 80
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: false
          runAsNonRoot: true
          runAsUser: 101
          seccompProfile:
            type: RuntimeDefault
        volumeMounts:
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
      volumes:
      - name: cache
        emptyDir: {}
      - name: run
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: api-server
  namespace: ${NAMESPACE}
spec:
  selector:
    app: api-server
  ports:
  - port: 80
    targetPort: 80
EOF

# Deployment 4: Worker pods (batch processing)
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: batch-worker
  namespace: ${NAMESPACE}
  labels:
    app: batch-worker
    optimize: "true"
    workload-type: worker
spec:
  replicas: 2
  selector:
    matchLabels:
      app: batch-worker
  template:
    metadata:
      labels:
        app: batch-worker
    spec:
      containers:
      - name: worker
        image: busybox:1.36
        command:
        - sh
        - -c
        - |
          while true; do
            echo "Processing batch job..."
            sleep 30
            # Simulate some CPU work
            dd if=/dev/zero of=/dev/null bs=1M count=100 2>/dev/null
            sleep 30
          done
        resources:
          requests:
            cpu: "150m"
            memory: "128Mi"
          limits:
            cpu: "800m"
            memory: "256Mi"
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 1000
          seccompProfile:
            type: RuntimeDefault
EOF

echo ""
echo "=== Waiting for workloads to be ready ==="
kubectl wait --for=condition=available --timeout=180s deployment/nginx-web -n ${NAMESPACE}
kubectl wait --for=condition=available --timeout=180s deployment/redis-cache -n ${NAMESPACE}
kubectl wait --for=condition=available --timeout=180s deployment/api-server -n ${NAMESPACE}
kubectl wait --for=condition=available --timeout=180s deployment/batch-worker -n ${NAMESPACE}

# Wait for StatefulSet
kubectl wait --for=condition=ready --timeout=180s pod/postgres-db-0 -n ${NAMESPACE} || true

# Wait for DaemonSet
kubectl rollout status daemonset/log-collector -n ${NAMESPACE} --timeout=180s

echo ""
echo "=== Cluster Setup Complete ==="
echo ""
echo "Cluster Name: ${CLUSTER_NAME}"
echo "Workload Namespace: ${NAMESPACE}"
echo ""
echo "Deployed Workloads:"
echo "  - nginx-web (Deployment, 3 replicas) - Web server"
echo "  - redis-cache (Deployment, 2 replicas) - Cache"
echo "  - postgres-db (StatefulSet, 2 replicas) - Database"
echo "  - log-collector (DaemonSet) - Logging"
echo "  - api-server (Deployment, 4 replicas) - API with auto-update label"
echo "  - batch-worker (Deployment, 2 replicas) - Batch processing"
echo ""
echo "To use this cluster:"
echo "  kubectl config use-context kind-${CLUSTER_NAME}"
echo ""
echo "To check workload status:"
echo "  kubectl get pods -n ${NAMESPACE}"
echo "  kubectl top pods -n ${NAMESPACE}"
echo ""
echo "To install OptiPod:"
echo "  make install"
echo "  make deploy IMG=example.com/optipod:v0.0.1"
echo ""
echo "To destroy this cluster:"
echo "  kind delete cluster --name ${CLUSTER_NAME}"
echo ""
