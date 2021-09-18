#!/bin/sh

set -eu

# This script must run in two modes:
#
# - When being used to set up a devcontainer.
#   In this mode the code is not checked out yet,
#   and we can install the tools globally.
#
# - When being used to install tools locally.
#   In this mode the code is already checked out,
#   and we do not want to pollute the user’s system.
#
# To distinguish between these modes we will
# have the devcontainer script pass an argument:


architecture="$(uname -m)"
case $architecture in
    x86_64) architecture="amd64";;
    aarch64 | armv8*) architecture="arm64";;
    aarch32 | armv7* | armvhf*) architecture="arm";;
    i?86) architecture="386";;
    *) echo "(!) Architecture $architecture unsupported"; exit 1 ;;
esac

KUBECTL_VERSION="${1:-"latest"}"
KUBECTL_SHA256="${4:-"automatic"}"

# Install the kubectl, verify checksum

curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${architecture}/kubectl"
chmod 0755 /usr/local/bin/kubectl

if ! type kubectl > /dev/null 2>&1; then
    echo '(!) kubectl installation failed!'
    exit 1
fi

# kubectl zsh completion
mkdir -p /home/${USERNAME}/.oh-my-zsh/completions
kubectl completion zsh > /home/${USERNAME}/.oh-my-zsh/completions/_kubectl


if [ "$1" = "devcontainer" ]; then 
    TOOL_DEST=/usr/local/bin
    KUBEBUILDER_DEST=/usr/local/kubebuilder
else
    TOOL_DEST=$(git rev-parse --show-toplevel)/hack/tools
    mkdir -p "$TOOL_DEST"
    KUBEBUILDER_DEST="$TOOL_DEST/kubebuilder"
fi

if ! command -v go > /dev/null 2>&1; then
    echo "Go must be installed manually: https://golang.org/doc/install"
    exit 1
fi

if ! command -v az > /dev/null 2>&1; then
    echo "Azure CLI must be installed manually: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli"
    exit 1
fi

echo "Installing tools to $TOOL_DEST"

# Install Go tools
TMPDIR=$(mktemp -d)
clean() { 
    chmod +w -R "$TMPDIR"
    rm -rf "$TMPDIR"
}
trap clean EXIT

export GOBIN=$TOOL_DEST
export GOPATH=$TMPDIR
export GOCACHE=$TMPDIR/cache
export GO111MODULE=on

echo "Installing Go tools…"

# go tools for vscode are preinstalled by base image (see first comment in Dockerfile)
go get \
    k8s.io/code-generator/cmd/conversion-gen@v0.18.2 \
    sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1 \
    sigs.k8s.io/kind@v0.9.0 \
    sigs.k8s.io/kustomize/kustomize/v3@v3.8.6 

if [ "$1" != "devcontainer" ]; then 
    echo "Installing golangci-lint…"
    # golangci-lint is provided by base image if in devcontainer
    # this command copied from there
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$TOOL_DEST" 2>&1 
fi

# Install go-task (task runner)
echo "Installing go-task…"
curl -sL "https://github.com/go-task/task/releases/download/v3.0.0/task_linux_amd64.tar.gz" | tar xz -C "$TOOL_DEST" task

# Install kubebuilder
os=$(go env GOOS)
arch=$(go env GOARCH)
echo "Installing kubebuilder ($os $arch)…"
#curl -L "https://go.kubebuilder.io/dl/2.3.1/${os}/${arch}" | tar -xz -C /tmp/
#mv "/tmp/kubebuilder_2.3.1_${os}_${arch}" "$KUBEBUILDER_DEST"
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/${os}/${arch}"
chmod +x kubebuilder && mv kubebuilder /usr/local/bin/
#chmod +x /tmp/kubebuilder && mv /tmp/kubebuilder "$KUBEBUILDER_DEST"
#curl -L -o /tmp/kubebuilder https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)


echo "Installed tools: $(ls "$TOOL_DEST")"


if [ "$1" = "devcontainer" ]; then 
    echo "Setting up k8s webhook certificates"

    mkdir -p /tmp/k8s-webhook-server/serving-certs
    openssl genrsa 2048 > tls.key
    openssl req -new -x509 -nodes -sha256 -days 3650 -key tls.key -subj '/' -out tls.crt
    mv tls.key tls.crt /tmp/k8s-webhook-server/serving-certs
fi

