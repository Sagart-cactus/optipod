#!/bin/bash
# Script to create Kubernetes secrets for Prometheus authentication
# Usage: ./create-prometheus-secrets.sh [options]

set -e

NAMESPACE="optipod-system"
AUTH_TYPE=""
USERNAME=""
PASSWORD=""
TOKEN=""
CA_FILE=""
CERT_FILE=""
KEY_FILE=""

usage() {
    cat <<EOF
Create Kubernetes secrets for OptiPod Prometheus authentication

Usage: $0 [options]

Options:
    -n, --namespace NAMESPACE       Kubernetes namespace (default: optipod-system)
    -t, --type TYPE                 Authentication type: basic, bearer, tls
    -u, --username USERNAME         Username for basic auth
    -p, --password PASSWORD         Password for basic auth
    -T, --token TOKEN               Bearer token
    --ca-file FILE                  CA certificate file for TLS
    --cert-file FILE                Client certificate file for TLS
    --key-file FILE                 Client key file for TLS
    -h, --help                      Show this help message

Examples:
    # Create basic auth secret
    $0 --type basic --username optipod --password 'my-password'

    # Create bearer token secret
    $0 --type bearer --token 'eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...'

    # Create TLS secret
    $0 --type tls --ca-file ca.pem --cert-file client.pem --key-file client-key.pem

    # Create secrets in custom namespace
    $0 --namespace my-namespace --type basic --username user --password pass

EOF
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -t|--type)
            AUTH_TYPE="$2"
            shift 2
            ;;
        -u|--username)
            USERNAME="$2"
            shift 2
            ;;
        -p|--password)
            PASSWORD="$2"
            shift 2
            ;;
        -T|--token)
            TOKEN="$2"
            shift 2
            ;;
        --ca-file)
            CA_FILE="$2"
            shift 2
            ;;
        --cert-file)
            CERT_FILE="$2"
            shift 2
            ;;
        --key-file)
            KEY_FILE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate inputs
if [[ -z "$AUTH_TYPE" ]]; then
    echo "Error: Authentication type is required"
    usage
fi

# Create namespace if it doesn't exist
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
    echo "Creating namespace: $NAMESPACE"
    kubectl create namespace "$NAMESPACE"
fi

case "$AUTH_TYPE" in
    basic)
        if [[ -z "$USERNAME" ]] || [[ -z "$PASSWORD" ]]; then
            echo "Error: Username and password are required for basic auth"
            exit 1
        fi

        echo "Creating basic auth secret: prometheus-credentials"
        kubectl create secret generic prometheus-credentials \
            --from-literal=username="$USERNAME" \
            --from-literal=password="$PASSWORD" \
            --namespace="$NAMESPACE" \
            --dry-run=client -o yaml | kubectl apply -f -

        echo "✓ Secret 'prometheus-credentials' created successfully"
        echo ""
        echo "Use in Helm values:"
        echo "  metricsProvider:"
        echo "    type: prometheus"
        echo "    prometheus:"
        echo "      auth:"
        echo "        type: basic"
        echo "        basic:"
        echo "          existingSecret:"
        echo "            name: prometheus-credentials"
        ;;

    bearer)
        if [[ -z "$TOKEN" ]]; then
            echo "Error: Token is required for bearer auth"
            exit 1
        fi

        echo "Creating bearer token secret: prometheus-token"
        kubectl create secret generic prometheus-token \
            --from-literal=token="$TOKEN" \
            --namespace="$NAMESPACE" \
            --dry-run=client -o yaml | kubectl apply -f -

        echo "✓ Secret 'prometheus-token' created successfully"
        echo ""
        echo "Use in Helm values:"
        echo "  metricsProvider:"
        echo "    type: prometheus"
        echo "    prometheus:"
        echo "      auth:"
        echo "        type: bearer"
        echo "        bearer:"
        echo "          existingSecret:"
        echo "            name: prometheus-token"
        ;;

    tls)
        if [[ -z "$CA_FILE" ]]; then
            echo "Error: CA file is required for TLS"
            exit 1
        fi

        if [[ ! -f "$CA_FILE" ]]; then
            echo "Error: CA file not found: $CA_FILE"
            exit 1
        fi

        CMD="kubectl create secret generic prometheus-tls --from-file=ca.crt=$CA_FILE"

        if [[ -n "$CERT_FILE" ]] && [[ -n "$KEY_FILE" ]]; then
            if [[ ! -f "$CERT_FILE" ]]; then
                echo "Error: Certificate file not found: $CERT_FILE"
                exit 1
            fi
            if [[ ! -f "$KEY_FILE" ]]; then
                echo "Error: Key file not found: $KEY_FILE"
                exit 1
            fi
            CMD="$CMD --from-file=tls.crt=$CERT_FILE --from-file=tls.key=$KEY_FILE"
        fi

        CMD="$CMD --namespace=$NAMESPACE --dry-run=client -o yaml"

        echo "Creating TLS secret: prometheus-tls"
        eval "$CMD" | kubectl apply -f -

        echo "✓ Secret 'prometheus-tls' created successfully"
        echo ""
        echo "Use in Helm values:"
        echo "  metricsProvider:"
        echo "    type: prometheus"
        echo "    prometheus:"
        echo "      tls:"
        echo "        enabled: true"
        echo "        existingSecret:"
        echo "          name: prometheus-tls"
        ;;

    *)
        echo "Error: Invalid authentication type: $AUTH_TYPE"
        echo "Valid types: basic, bearer, tls"
        exit 1
        ;;
esac

echo ""
echo "Next steps:"
echo "1. Install or upgrade OptiPod with the appropriate Helm values"
echo "2. Verify the controller can connect to Prometheus:"
echo "   kubectl logs -n $NAMESPACE deployment/optipod-controller -f"
