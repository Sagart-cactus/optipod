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

# Check if cert-manager is available
check_cert_manager() {
    if kubectl get crd certificates.cert-manager.io >/dev/null 2>&1; then
        echo "✓ cert-manager detected"
        return 0
    else
        echo "✗ cert-manager not found"
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

    # Cleanup
    rm -rf "$CERT_DIR"
    echo "✓ Self-signed certificates created"
}

# Install webhook components
install_webhook() {
    echo "Installing webhook components..."

    # Create namespace if it doesn't exist
    kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    # Determine certificate management approach
    if [ "$CERT_MANAGER" = "auto" ]; then
        if check_cert_manager; then
            echo "Using cert-manager for certificate management"
            kubectl apply -k config/webhook-enabled/
        else
            echo "Using manual certificate management"
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
    fi
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
    echo "Examples:"
    echo "  $0                                    # Install with webhook enabled, auto cert management"
    echo "  WEBHOOK_MODE=disabled $0              # Install without webhook (SSA only)"
    echo "  CERT_MANAGER=manual $0                # Install with manual certificate management"
    echo "  NAMESPACE=my-optipod $0               # Install in custom namespace"
}

# Handle help flag
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    usage
    exit 0
fi

# Run main installation
main
