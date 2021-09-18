# See here for image contents: https://github.com/microsoft/vscode-dev-containers/tree/v0.137.0/containers/go/.devcontainer/base.Dockerfile

# This is pinned to a particular version of go:
FROM mcr.microsoft.com/vscode/devcontainers/go:0-1.16

# APT dependencies
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get -y install --no-install-recommends bash-completion software-properties-common lsb-release \
    # install az-cli
    && curl -sL https://aka.ms/InstallAzureCLIDeb | bash -

# install kubectl - https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/#install-using-native-package-management

RUN apt-get update
RUN apt-get install -y apt-transport-https ca-certificates curl
RUN curl -fsSLo /usr/share/keyrings/kubernetes-archive-keyring.gpg https://packages.cloud.google.com/apt/doc/apt-key.gpg
RUN echo "deb [signed-by=/usr/share/keyrings/kubernetes-archive-keyring.gpg] https://apt.kubernetes.io/ kubernetes-xenial main" | sudo tee /etc/apt/sources.list.d/kubernetes.list
RUN apt-get update
RUN apt-get install -y kubectl

# install docker
# - not yet needed?
# RUN curl -fsSL https://get.docker.com | sh -
# RUN usermod -aG docker vscode

COPY install-dependencies.sh .
RUN ./install-dependencies.sh devcontainer && rm install-dependencies.sh

# Add kubebuilder to PATH
#ENV PATH=$PATH:/usr/local/kubebuilder/bin

# Add further bash customizations
# note that the base image includes oh-my-bash, we are enabling plugins here
# TODO: restore oh-my-bash? it was removed in base image.
# RUN sed -i '/^plugins=/a kubectl\ngolang' "/home/vscode/.bashrc"
# RUN sed -i '/^completions=/a kubectl\ngo\ntask' "/home/vscode/.bashrc"

# Make kubectl completions work with 'k' alias
RUN echo 'complete -F __start_kubectl k' >> "/home/vscode/.bashrc"

# Setup go-task completions
RUN curl -sL "https://raw.githubusercontent.com/go-task/task/v3.0.0/completion/bash/task.bash" > "/home/vscode/.task.completion.sh" \
    && echo 'source /home/vscode/.task.completion.sh' >> /home/vscode/.bashrc

ENV KIND_CLUSTER_NAME=aso

# install docker, from: https://github.com/microsoft/vscode-dev-containers/blob/main/script-library/docs/docker.md
COPY library-scripts/docker-debian.sh /tmp/library-scripts/
RUN bash /tmp/library-scripts/docker-debian.sh
ENTRYPOINT ["/usr/local/share/docker-init.sh"]

# https://github.com/Microsoft/vscode-dev-containers/tree/main/containers/kubernetes-helm#how-it-works--adapting-your-existing-dev-container-config
COPY copy-kube-config.sh /usr/local/share/
RUN echo "source /usr/local/share/copy-kube-config.sh" | tee -a /root/.bashrc >> /root/.zshrc

#RUN curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 && sudo install #minikube-linux-amd64 /usr/local/bin/minikube

CMD ["sleep", "infinity"]
