# Tailcar

<div align="center">
  <img width="200" alt="Tailcar Logo" src="./assets/logo.svg">

  [![Go Report Card](https://goreportcard.com/badge/github.com/rajsinghtech/tailcar)](https://goreportcard.com/report/github.com/rajsinghtech/tailcar)
  [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
  [![Kubernetes](https://img.shields.io/badge/Kubernetes-v1.20+-blue.svg)](https://kubernetes.io/)

  **Kubernetes operator for automatic Tailscale sidecar injection**

  Seamlessly integrate Tailscale into your Kubernetes pods with zero-touch sidecar injection.
</div>

---

## Quick Start

### Prerequisites
- Kubernetes cluster (v1.20+)
- `kubectl` configured
- Helm 3.x (recommended)
- [cert-manager](https://cert-manager.io/) (v1.0+) - For webhook certificate management
- Tailscale account with OAuth client

### Install cert-manager

Tailcar uses cert-manager to manage webhook certificates. Install it first if not already present:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
```

Wait for cert-manager to be ready:

```bash
kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager -n cert-manager --timeout=300s
```

### Install with Helm (Recommended)

```bash
# Create namespace and install latest stable release
kubectl create namespace tailcar-system
helm install tailcar oci://ghcr.io/rajsinghtech/tailcar-helm \
  --namespace tailcar-system
```

### Create Tailscale OAuth Secret

```bash
kubectl create secret generic my-tailnet-oauth \
  --from-literal=client-id='<your-oauth-client-id>' \
  --from-literal=client-secret='<your-oauth-client-secret>' \
  -n default
```

### Create Your First Tailnet

```yaml
apiVersion: tailcar.rajsingh.info/v1alpha1
kind: Tailnet
metadata:
  name: my-tailnet
  namespace: default
spec:
  tailnetName: "-"  # Use "-" for default tailnet

  oauthSecretRef:
    name: my-tailnet-oauth
    namespace: default

  tailscale:
    tags:
      - "tag:k8s"
```

```bash
kubectl apply -f tailnet.yaml
```

### Enable Sidecar Injection on Pods

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  annotations:
    tailcar.rajsingh.info/inject: "true"
    tailcar.rajsingh.info/tailnet: "my-tailnet"
spec:
  containers:
  - name: app
    image: nginx
```

```bash
kubectl apply -f pod.yaml
```

---

### OAuth Client Setup

Create an OAuth client in the [Tailscale admin console](https://login.tailscale.com/admin/settings/oauth):

1. Navigate to **Settings** → **OAuth clients**
2. Generate a new OAuth client
3. Grant required scopes:
   - `all:write` (recommended for operator)
   - Or: `devices:write` + `keys:write`
4. Add tags that the client can create (e.g., `tag:k8s`)

**Scopes Explained:**
- `all:write` - Full access to manage devices and keys
- `devices:write` - Create and modify devices
- `keys:write` - Create and manage authentication keys

---

## License

This project is licensed under the [Apache License 2.0](LICENSE).

---

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=rajsinghtech/tailcar&type=Date)](https://star-history.com/#rajsinghtech/tailcar&Date)

---

<div align="center">

*Built with [Kubebuilder](https://kubebuilder.io/) and <3 from the [Tailscale](https://tailscale.com) community*

</div>
