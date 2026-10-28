# Prometheus Authentication Documentation

This directory contains comprehensive documentation for OptiPod's Prometheus authentication support.

## 📚 Documentation Index

### Quick Start
- **[Quick Start Guide](../PROMETHEUS_QUICK_START.md)** - Get up and running in 5 minutes
  - Secret creation commands
  - Helm installation examples
  - Cloud provider quick setups
  - Common troubleshooting

### Detailed Guides
- **[Authentication Guide](../PROMETHEUS_AUTHENTICATION.md)** - Complete authentication reference
  - All authentication methods explained
  - Security best practices
  - Cloud provider integrations
  - Detailed troubleshooting
  - Testing and verification

- **[Migration Guide](../PROMETHEUS_MIGRATION_GUIDE.md)** - Migrate from unsecured Prometheus
  - Step-by-step migration scenarios
  - Zero-downtime migration strategies
  - Rollback procedures
  - Credential rotation
  - Post-migration verification

### Configuration
- **[Configuration Reference](../../charts/optipod/CONFIGURATION.md)** - Helm chart configuration
  - All configuration parameters
  - Values reference table
  - Integration with other features

- **[Example Values](../../charts/optipod/examples/prometheus-auth-values.yaml)** - Ready-to-use examples
  - Basic authentication
  - Bearer token
  - mTLS
  - AWS AMP
  - GCP Managed Prometheus

### Implementation
- **[Implementation Summary](../../PROMETHEUS_AUTH_IMPLEMENTATION.md)** - Technical details
  - Architecture overview
  - Files changed/created
  - Testing results
  - Security features

## 🚀 Quick Reference

### Authentication Methods

| Method | Use Case | Security Level |
|--------|----------|----------------|
| **None** | Development, unsecured Prometheus | ⚠️ Low |
| **Basic Auth** | Simple username/password | ⭐⭐ Medium |
| **Bearer Token** | OAuth2/OIDC, cloud providers | ⭐⭐⭐ High |
| **mTLS** | Enterprise, high security | ⭐⭐⭐⭐ Very High |

### Common Commands

**Create Basic Auth Secret:**
```bash
kubectl create secret generic prometheus-credentials \
  --from-literal=username=optipod \
  --from-literal=password='your-password' \  # pragma: allowlist secret
  -n optipod-system
```

**Create Bearer Token Secret:**
```bash
kubectl create secret generic prometheus-token \
  --from-literal=token='your-token' \
  -n optipod-system
```

**Create TLS Secret:**
```bash
kubectl create secret generic prometheus-tls \
  --from-file=ca.crt=ca.pem \
  --from-file=tls.crt=client.pem \
  --from-file=tls.key=client-key.pem \
  -n optipod-system
```

**Install with Authentication:**
```bash
helm install optipod ./charts/optipod \
  --set metricsProvider.type=prometheus \
  --set metricsProvider.prometheus.url=https://prometheus.example.com \
  --set metricsProvider.prometheus.auth.type=basic \
  --set metricsProvider.prometheus.auth.basic.existingSecret.name=prometheus-credentials \
  -n optipod-system
```

## 🛠️ Helper Tools

### Secret Creation Script
```bash
./scripts/create-prometheus-secrets.sh --help
```

Creates Kubernetes secrets for all authentication types with proper formatting.

## 🔒 Security Best Practices

### ✅ DO
- Use Kubernetes Secrets for credentials
- Enable TLS for production
- Rotate credentials regularly
- Use least-privilege access
- Monitor authentication failures

### ❌ DON'T
- Put credentials in values.yaml
- Use command-line flags for passwords
- Disable TLS verification in production
- Use default/weak passwords
- Share credentials across environments

## 🌩️ Cloud Provider Support

### AWS Managed Prometheus (AMP)
- Bearer token authentication
- IAM role integration (IRSA)
- SigV4 signing support

### GCP Managed Prometheus
- OAuth2 token authentication
- Workload Identity integration
- Service account support

### Azure Monitor
- Bearer token authentication
- Managed Identity support

## 🐛 Troubleshooting

### Quick Diagnostics

**Check Controller Logs:**
```bash
kubectl logs -n optipod-system deployment/optipod-controller -f
```

**Verify Secrets:**
```bash
kubectl get secret prometheus-credentials -n optipod-system
kubectl exec -n optipod-system deployment/optipod-controller -- env | grep PROMETHEUS
```

**Test Connectivity:**
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v -u username:password https://prometheus:9090/api/v1/query?query=up
```

### Common Issues

| Error | Cause | Solution |
|-------|-------|----------|
| 401 Unauthorized | Invalid credentials | Verify secret contents, recreate if needed |
| 403 Forbidden | Insufficient permissions | Check Prometheus user permissions |
| Certificate error | Invalid CA or cert | Verify certificate files and expiration |
| Connection refused | Wrong URL or network issue | Test connectivity from within cluster |
| Timeout | Slow Prometheus or network | Increase timeout value |

## 📖 Additional Resources

### External Documentation
- [Prometheus Security](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#configuration)
- [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)
- [cert-manager](https://cert-manager.io/docs/)
- [AWS AMP Authentication](https://docs.aws.amazon.com/prometheus/latest/userguide/AMP-and-IAM.html)
- [GCP Managed Prometheus](https://cloud.google.com/stackdriver/docs/managed-prometheus/authentication)

### OptiPod Documentation
- [Installation Guide](../INSTALLATION.md)
- [CRD Reference](../CRD_REFERENCE.md)
- [Webhook Documentation](../WEBHOOK_EXAMPLES.md)

## 🤝 Contributing

Found an issue or have a suggestion? Please:
1. Check existing documentation
2. Search GitHub issues
3. Open a new issue with details

## 📝 Version History

- **v1.5.0** - Initial Prometheus authentication support
  - Basic authentication
  - Bearer token authentication
  - mTLS support
  - Comprehensive documentation

## 📄 License

This documentation is part of the OptiPod project and is licensed under the Apache License 2.0.
