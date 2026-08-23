FROM lscr.io/linuxserver/code-server:latest

# Set Go environment variables
ENV GO_VERSION=1.27.0
ENV GOPATH=/config/go
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

# Install base dependencies and build essentials
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    build-essential \
    git \
    && rm -rf /var/lib/apt/lists/*

# Add Docker official GPG key and APT repository
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
    && chmod a+r /etc/apt/keyrings/docker.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu noble stable" \
    > /etc/apt/sources.list.d/docker.list

# Add GitHub CLI official GPG key and APT repository
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list

# Install Docker CLI and GitHub CLI
RUN apt-get update && apt-get install -y \
    docker-ce-cli \
    docker-buildx-plugin \
    docker-compose-plugin \
    gh \
    && rm -rf /var/lib/apt/lists/*

# Install Go Toolchain
RUN curl -sL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz

# Install Go CLI utilities (gopls, dlv, golangci-lint, mockgen)
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest \
    && go install github.com/golang/mock/mockgen@latest

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

# Install k9s
RUN curl -sL https://github.com/derailed/k9s/releases/latest/download/k9s_Linux_amd64.tar.gz | tar xz -C /usr/local/bin/ k9s

# Ensure default user shell inherits GOPATH and PATH permanently
RUN echo 'export GOPATH=/config/go' >> /etc/bash.bashrc \
    && echo 'export PATH=$GOPATH/bin:/usr/local/go/bin:$PATH' >> /etc/bash.bashrc

# Install vim
RUN apt-get update && apt-get install -y vim \
    && rm -rf /var/lib/apt/lists/*

# Install Starship
RUN curl -sS https://starship.rs/install.sh | sh

# Add to ~/.bashrc
RUN echo 'eval "$(starship init bash)"' >> /etc/bash.bashrc