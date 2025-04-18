# Tailscale MagicDNS Proxy (tsmagicproxy)

This application creates a DNS server that exposes Tailscale's MagicDNS information from a tailnet to external clients. It registers itself as a machine on the tailnet and provides DNS resolution for all machines in your tailnet.

## Features

- Registers itself as a machine on your tailnet
- Exposes MagicDNS records via a standard DNS server (port 53)
- Automatically detects the tailnet domain
- Supports both forward lookups (name to IP) and reverse lookups (IP to name)
- Containerized for easy deployment
- Kubernetes deployment with kustomize

## Requirements

- A Tailscale account
- An auth key from your tailnet (ephemeral or not)
- Docker (for containerized deployment) or Go 1.24+ (for local builds)

## Quick Start with Docker

```bash
# Use pre-built image
docker run -d --name tsmagicproxy \
  -p 53:53/udp \
  -e TS_AUTHKEY="tskey-auth-xxxx" \
  quay.io/rajsinghcpre/tsmagicproxy:latest

# Or build locally
docker build -t tsmagicproxy .
docker run -d --name tsmagicproxy \
  -p 53:53/udp \
  -e TS_AUTHKEY="tskey-auth-xxxx" \
  tsmagicproxy
```

## Building and Running Locally

```bash
# Clone the repository
git clone https://github.com/rajsinghtech/tsmagicproxy.git
cd tsmagicproxy

# Build the application
go build -o tsmagicproxy .

# Run the application (requires sudo to bind to port 53)
sudo TS_AUTHKEY="tskey-auth-xxxx" ./tsmagicproxy

# Alternatively, run on a different port that doesn't require root
./tsmagicproxy -listen ":5353" -authkey "tskey-auth-xxxx"

# Force login even if state exists
./tsmagicproxy -force-login -listen ":5353" -authkey "tskey-auth-xxxx"
```

## Kubernetes Deployment

We provide Kubernetes manifests for deploying with kustomize. See the [kubernetes/README.md](./kubernetes/README.md) for details.

Quick start:

```bash
# Deploy to dev environment (update auth key first!)
kubectl apply -k kubernetes/kustomize
```

## Usage

```
Usage of ./tsmagicproxy:
  -authkey string
        Tailscale auth key (default: value of TS_AUTHKEY environment variable)
  -hostname string
        Hostname for the tailnet node (default "tsmagicproxy")
  -listen string
        Address to listen on for DNS requests (default ":53")
  -state-dir string
        Directory to store tailscale state (default "./tsmagicproxy-state")
  -ttl int
        TTL for DNS responses (default 600)
  -domain string
        Domain suffix to append to hostnames (default: auto-detected from tailnet)
  -force-login
        Force login even if state exists (default: false)
  -debug
        Enable verbose debug logging (default: false)
```

## Example: Querying for Machines in Your Tailnet

Once the proxy is running, you can query it using standard DNS tools:

```bash
# Look up a machine in your tailnet
dig @localhost myhost.example.com

# Reverse lookup
dig @localhost -x 100.100.100.100
```

## Kubernetes Deployment

Here's an example Kubernetes deployment:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tailscale-auth
type: Opaque
stringData:
  TS_AUTHKEY: "tskey-auth-xxxx"  # Replace with your actual auth key
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tsmagicproxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tsmagicproxy
  template:
    metadata:
      labels:
        app: tsmagicproxy
    spec:
      containers:
      - name: tsmagicproxy
        image: tsmagicproxy:latest
        ports:
        - containerPort: 53
          protocol: UDP
        env:
        - name: TS_AUTHKEY
          valueFrom:
            secretKeyRef:
              name: tailscale-auth
              key: TS_AUTHKEY
        volumeMounts:
        - name: tailscale-state
          mountPath: /var/lib/tsmagicproxy
      volumes:
      - name: tailscale-state
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: tsmagicproxy
spec:
  selector:
    app: tsmagicproxy
  ports:
  - port: 53
    protocol: UDP
  type: ClusterIP
```

## How It Works

1. The application connects to Tailscale using the provided auth key
2. It registers itself as a node on your tailnet with the specified hostname
3. It retrieves information about all other nodes in your tailnet
4. It starts a DNS server that answers queries based on the MagicDNS information
5. When a DNS query arrives, it looks up the corresponding machine in your tailnet and returns its Tailscale IP

## Security Considerations

- The auth key used to register this proxy with your tailnet will have access to all your tailnet information, so use an appropriate key with the necessary permissions.
- Consider using ephemeral keys if you don't want the proxy to be a permanent node in your tailnet.
- Since this exposes DNS information, be careful about who can access this service.
- All Tailscale security policies apply as normal. This service only exposes DNS information for nodes that the auth key has permission to see.

## Troubleshooting

- **Can't bind to port 53**: Port 53 requires root/administrator privileges. Either run with sudo/as administrator or use a different port.
- **Can't connect to tailnet**: Make sure your auth key is valid and has the necessary permissions.
- **Empty DNS responses**: Check that MagicDNS is enabled for your tailnet.
- **Connection timeout**: Check network connectivity and firewall settings.
- **Error about state already existing**: Use the `-force-login` flag to force a new login.

## License

This project is licensed under the BSD 3-Clause License - see the LICENSE file for details.

## Acknowledgments

This project uses the Tailscale Go libraries, particularly the `tsnet` package, which allows embedding Tailscale connectivity into Go applications.
