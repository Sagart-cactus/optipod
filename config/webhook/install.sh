#!/bin/bash

# OptipPod Webhook Installation Script
# This script installs the OptipPod mutating webhook

set -e

NAMESPACE=${NAMESPACE:-optipod-system}
WEBHOOK_MODE=${WEBHOOK_MODE:-enabled}
CERT_MANAGER=${CERT_MANAGER:-auto}

echo "Installing OptipPod with webhook support..."
echo "Namespace: $NAMESPACE"
echo "Webhook Mode: $WEBHOOK_MODE"
echo "Certificate Management: $CERT_MANAGER"

# Check if cert-manager is available and usable
check_cert_manager() {
    if kubectl get crd certificates.cert-manager.io >/dev/null 2>&1; then
        echo "cert-manager CRD found, checking for usable issuers..."
        return 0
    else
        echo "✗ cert-manager CRD not found"
        return 1
    fi
}

# Check if cert-manager has usable issuers
check_cert_manager_usable() {
    if kubectl get issuer -A >/dev/null 2>&1 || kubectl get clusterissuer >/dev/null 2>&1; then
        echo "✓ cert-manager is usable (found issuers)"
        return 0
    else
        echo "✗ cert-manager found but no issuers available"
        return 1
    fi
}

# Generate self-signed certificates manually
generate_certs() {
    echo "Generating self-signed certificates..."

    # Create temporary directory
    CERT_DIR=$(mktemp -d)

    # Generate private key
    openssl genrsa -out "$CERT_DIR/tls.key" 2048

    # Generate certificate
    openssl req -new -x509 -key "$CERT_DIR/tls.key" -out "$CERT_DIR/tls.crt" -days 365 \
        -subj "/CN=optipod-webhook-service.$NAMESPACE.svc" \
        -addext "subjectAltName=DNS:optipod-webhook-service.$NAMESPACE.svc,DNS:optipod-webhook-service.$NAMESPACE.svc.cluster.local"

    # Create secret
    kubectl create secret tls webhook-server-certs \
        --cert="$CERT_DIR/tls.crt" \
        --key="$CERT_DIR/tls.key" \
        --namespace="$NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Inject CA bundle into webhook configuration
    echo "Injecting CA bundle into webhook configuration..."
    CA_BUNDLE=$(kubectl get secret webhook-server-certs \
        -n "$NAMESPACE" \
        -o jsonpath='{.data.tls\.crt}')

    if [ -n "$CA_BUNDLE" ]; then
        kubectl patch mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration \
            --type=json \
            -p="[{
                \"op\": \"replace\",
                \"path\": \"/webhooks/0/clientConfig/caBundle\",
                \"value\": \"$CA_BUNDLE\"
            }]"
        echo "✓ CA bundle injected into webhook configuration"
    else
        echo "✗ Failed to retrieve CA bundle from secret"
        exit 1
    fi

    # Cleanup
    rm -rf "$CERT_DIR"
    echo "✓ Self-signed certificates created and configured"
}

# Install webhook components
install_webhook() {
    echo "Installing webhook components..."

    # Create namespace if it doesn't exist
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    # Determine certificate management approach
    if [ "$CERT_MANAGER" = "auto" ]; then
        if check_cert_manager && check_cert_manager_usable; then
            echo "Using cert-manager for certificate management"
            kubectl apply -k config/webhook-enabled/
            wait_for_cert_manager_ca_bundle
        else
            echo "Using manual certificate management (cert-manager not usable)"
            generate_certs
            # Apply webhook config without cert-manager
            kubectl apply -k config/webhook/ --namespace="$NAMESPACE"
        fi
    elif [ "$CERT_MANAGER" = "manual" ]; then
        echo "Using manual certificate management"
        generate_certs
        kubectl apply -k config/webhook/ --namespace="$NAMESPACE"
    else
        echo "Using cert-manager for certificate management"
        kubectl apply -k config/webhook-enabled/
        wait_for_cert_manager_ca_bundle
    fi
}

# Wait for cert-manager to inject CA bundle into webhook configuration
wait_for_cert_manager_ca_bundle() {
    echo "Waiting for cert-manager to inject CA bundle into webhook configuration..."

    local max_wait=120  # Maximum wait time in seconds
    local wait_interval=5  # Check interval in seconds
    local elapsed=0

    while [ $elapsed -lt $max_wait ]; do
        # Check if CA bundle is present in webhook configuration
        CA_BUNDLE=$(kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration \
            -o jsonpath='{.webhooks[0].clientConfig.caBundle}' 2>/dev/null || echo "")

        if [ -n "$CA_BUNDLE" ] && [ "$CA_BUNDLE" != "null" ]; then
            echo "✓ CA bundle injected by cert-manager"
            return 0
        fi

        echo "  Waiting for CA bundle injection... ($elapsed/${max_wait}s)"
        sleep $wait_interval
        elapsed=$((elapsed + wait_interval))
    done

    echo "⚠️  Timeout waiting for cert-manager to inject CA bundle"
    echo "   The webhook may not work properly until the CA bundle is injected"
    echo "   Check cert-manager logs: kubectl logs -n cert-manager -l app=cert-manager"
    return 1
}

# Verify installation
verify_installation() {
    echo "Verifying installation..."

    # Wait for webhook deployment
    kubectl wait --for=condition=available deployment/optipod-webhook-deployment \
        --namespace="$NAMESPACE" --timeout=300s

    # Check webhook configuration
    if kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration >/dev/null 2>&1; then
        echo "✓ Webhook configuration created"

        # Verify CA bundle is present
        CA_BUNDLE=$(kubectl get mutatingwebhookconfiguration optipod-webhook-mutating-webhook-configuration \
            -o jsonpath='{.webhooks[0].clientConfig.caBundle}')
        if [ -n "$CA_BUNDLE" ]; then
            echo "✓ CA bundle configured in webhook"
        else
            echo "✗ CA bundle missing in webhook configuration"
            exit 1
        fi
    else
        echo "✗ Webhook configuration not found"
        exit 1
    fi

    # Check service
    if kubectl get service optipod-webhook-service --namespace="$NAMESPACE" >/dev/null 2>&1; then
        echo "✓ Webhook service created"
    else
        echo "✗ Webhook service not found"
        exit 1
    fi

    # Check certificate secret
    if kubectl get secret webhook-server-certs --namespace="$NAMESPACE" >/dev/null 2>&1; then
        echo "✓ Webhook certificate secret exists"
    else
        echo "✗ Webhook certificate secret not found"
        exit 1
    fi

    # Verify certificates are mounted in controller
    echo "Verifying certificate mounting..."
    if kubectl exec -n "$NAMESPACE" deploy/controller-manager -- \
        test -f /tmp/k8s-webhook-server/serving-certs/tls.crt >/dev/null 2>&1; then
        echo "✓ Certificates mounted in controller"
    else
        echo "✗ Certificates not mounted in controller pod"
        echo "This may indicate a configuration issue or the controller hasn't started yet"
        exit 1
    fi

    # Show warning for manual certificates
    if [ "$CERT_MANAGER" = "manual" ] || ([ "$CERT_MANAGER" = "auto" ] && ! check_cert_manager_usable); then
        echo ""
        echo "⚠️  Manual certificates expire in 365 days. cert-manager is recommended for production."
        echo "   Consider installing cert-manager for automatic certificate rotation."
    fi

    echo "✓ Installation completed successfully"
}

# Main installation flow
main() {
    case "$WEBHOOK_MODE" in
        "enabled")
            install_webhook
            verify_installation
            ;;
        "disabled")
            echo "Installing OptipPod without webhook (SSA mode only)"
            kubectl apply -k config/default/
            ;;
        *)
            echo "Invalid webhook mode: $WEBHOOK_MODE"
            echo "Valid options: enabled, disabled"
            exit 1
            ;;
    esac
}

# Show usage
usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  NAMESPACE=<namespace>     Target namespace (default: optipod-system)"
    echo "  WEBHOOK_MODE=<mode>       Webhook mode: enabled|disabled (default: enabled)"
    echo "  CERT_MANAGER=<mode>       Certificate management: auto|manual|cert-manager (default: auto)"
    echo ""
    echo "Certificate Management Modes:"
    echo "  auto          - Detect cert-manager availability and use appropriate method"
    echo "  manual        - Force manual certificate generation (good for kind clusters)"
    echo "  cert-manager  - Force cert-manager usage (requires cert-manager with issuers)"
    echo ""
    echo "Examples:"
    echo "  $0                                    # Install with webhook enabled, auto cert management"
    echo "  WEBHOOK_MODE=disabled $0              # Install without webhook (SSA only)"
    echo "  CERT_MANAGER=manual $0                # Install with manual certificate management"
    echo "  NAMESPACE=my-optipod $0               # Install in custom namespace"
    echo ""
    echo "Quick install modes:"
    echo "  make kind-install                     # Install for kind cluster (manual certs)"
    echo "  make webhook-certmanager              # Install with cert-manager"
    echo "  make webhook-manual                   # Install with manual certs"
}

# Handle help flag
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    usage
    exit 0
fi

# Run main installation
main
