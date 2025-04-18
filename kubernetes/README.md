# Kubernetes Deployment for tsmagicproxy

This directory contains Kubernetes manifests for deploying tsmagicproxy using kustomize.

## Structure

- `kustomize-standalone/`: Base Kubernetes resources
  - `deployment.yaml`: Deployment configuration
  - `service.yaml`: Service configuration for exposing DNS
  - `secret.yaml`: Template for Tailscale auth key (don't commit real keys!)
  - `kustomization.yaml`: Base kustomization file
## Deploying with Kustomize

### Prerequisites

- Kubernetes cluster
- kubectl configured to access your cluster
- kustomize (built into kubectl)
- A Tailscale auth key

### Deploy to Development Environment

1. Edit the auth key in `kubernetes/kustomize-standalone/secret.yaml` with a valid Tailscale auth key.

2. Apply the configuration:

```bash
kubectl apply -k kubernetes/kustomize -n tailscale
```

## Using as a DNS Server

After deployment, the DNS server will be available inside your cluster at:

```
tsmagicproxy.tailscale.svc.cluster.local
```

Configure your applications or CoreDNS to forward queries to this service for Tailscale Magic DNS resolution.
