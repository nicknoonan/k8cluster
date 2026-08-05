1. Project StructurePlaintextnode-power-manager/
├── cmd/
│   └── server/
│       └── main.go
├── pkg/
│   ├── k8s/
│   │   └── client.go      # Node status, cordon, drain, scale deployments
│   ├── wol/
│   │   └── wol.go         # Magic packet magic via UDP
│   └── power/
│       └── ssh.go         # Remote systemctl poweroff execution
├── static/
│   ├── index.html         # Clean, simple web dashboard
│   └── app.js             # Fetch endpoints & status polling
├── Dockerfile
├── charts/
│   └── node-power-manager/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
└── .github/
    └── workflows/
        └── release.yaml
2. Core Go Component Logicpkg/wol/wol.go: Builds the standard 102-byte Magic Packet ($6 \times \text{0xFF} + 16 \times \text{Target MAC}$) and sends it over UDP broadcast to port 9 (255.255.255.255:9).pkg/k8s/client.go: Uses k8s.io/client-go/kubernetes (using rest.InClusterConfig()) to execute:GetNodeStatus(nodeName): Checks if the target node is Ready, NotReady, or missing.ScaleWorkloads(namespace, deploymentNames, replicas): Sets deployment spec.replicas to 1 (on boot) or 0 (before shutdown) to prevent Pending pod buildup.CordonAndDrain(nodeName): Sets unschedulable: true on the node, then issues eviction calls for active non-DaemonSet pods.pkg/power/ssh.go: Uses golang.org/x/crypto/ssh with an in-memory or mounted private key to run sudo systemctl poweroff on the target node's IP.3. API EndpointsGET /api/status: Returns current status of target node (OFFLINE, BOOTING, READY, DRAINING) and associated deployment replica counts.POST /api/power/on: Trigger WoL $\rightarrow$ Poll node state $\rightarrow$ Scale deployments $0 \rightarrow 1$.POST /api/power/off: Scale deployments $1 \rightarrow 0$ $\rightarrow$ Cordon & Drain node $\rightarrow$ Execute SSH power-off.Phase 2: Helm Chart & Kubernetes DeploymentKey Configuration PointshostNetwork: true: Crucial for the WoL magic packet to reach your physical L2 local network subnet.RBAC Rules: Grants permissions to get/update nodes and update deployment scale subresources.SSH Key Secret: Mounts a private SSH key configured in the target node's /root/.ssh/authorized_keys.charts/node-power-manager/templates/deployment.yamlYAMLapiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "node-power-manager.fullname" . }}
  namespace: {{ .Release.Namespace }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ include "node-power-manager.name" . }}
  template:
    metadata:
      labels:
        app: {{ include "node-power-manager.name" . }}
    spec:
      hostNetwork: true # Required for L2 WoL UDP broadcast
      serviceAccountName: {{ include "node-power-manager.fullname" . }}
      containers:
        - name: manager
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          env:
            - name: TARGET_NODE_NAME
              value: {{ .Values.config.targetNodeName | quote }}
            - name: TARGET_MAC_ADDRESS
              value: {{ .Values.config.targetMacAddress | quote }}
            - name: TARGET_IP
              value: {{ .Values.config.targetIp | quote }}
            - name: MANAGED_DEPLOYMENTS
              value: {{ .Values.config.managedDeployments | quote }} # e.g. "default/llm-inference,ci/runner"
          volumeMounts:
            - name: ssh-key
              mountPath: /etc/ssh-keys
              readOnly: true
      volumes:
        - name: ssh-key
          secret:
            secretName: {{ .Values.config.sshSecretName }}
            defaultMode: 0400
Phase 3: GitHub Actions Workflow (CI/CD to GHCR)This workflow triggers on releases or pushes to main. It builds the Go container image, builds the Helm chart, and publishes both to GitHub Container Registry (GHCR) as OCI artifacts..github/workflows/release.yamlYAMLname: Build and Publish Artifacts

on:
  push:
    branches:
      - main
    tags:
      - 'v*'

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      # Extract metadata (tags, labels) for Docker
      - name: Extract Docker Metadata
        id: meta
        using: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=semver,pattern={{version}}
            type=sha,format=short
            type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}

      # 1. Build & Push Go Container Image
      - name: Build and Push Docker Image
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      # 2. Package & Push Helm Chart to GHCR as OCI
      - name: Install Helm
        uses: azure/setup-helm@v4

      - name: Package and Push Helm Chart
        run: |
          # Set appVersion in Chart.yaml dynamically
          VERSION="${{ steps.meta.outputs.version }}"
          if [ -z "$VERSION" ]; then VERSION="0.1.0-${GITHUB_SHA::7}"; fi
          
          helm package charts/node-power-manager --version "$VERSION" --app-version "$VERSION"
          
          # Push OCI chart to GHCR
          helm push "node-power-manager-$VERSION.tgz" "oci://${{ env.REGISTRY }}/${{ github.repository_owner }}/charts"
Phase 4: ArgoCD IntegrationWith the Helm chart published as an OCI artifact in GHCR, configure your ArgoCD Application to point directly to the OCI registry.argocd-app.yamlYAMLapiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: node-power-manager
  namespace: argocd
spec:
  project: default
  source:
    chart: node-power-manager
    repoURL: ghcr.io/your-github-username/charts
    targetRevision: 0.1.0-xxxxxxx # Or semver pattern
    helm:
      values: |
        config:
          targetNodeName: "gpu-worker"
          targetMacAddress: "00:d8:61:59:88:14"
          targetIp: "192.168.1.103"
          managedDeployments: "default/ollama-inference,media/stable-diffusion"
          sshSecretName: "node-power-manager-ssh"
  destination:
    server: https://kubernetes.default.svc
    namespace: management
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true

## Current progress

- The release workflow should now read `version` and `appVersion` from `charts/node-power-manager/Chart.yaml` when packaging the chart.