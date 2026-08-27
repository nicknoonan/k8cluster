FROM lscr.io/linuxserver/code-server:latest

# Set Go environment variables
ENV GO_VERSION=1.27.0
ENV GOPATH=/config/go
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

# PlatformIO environment
# Keep PlatformIO core/toolchains on the persistent /config volume so they
# survive pod restarts, and allow pip installs on Ubuntu 24.04 (PEP 668).
ENV PLATFORMIO_CORE_DIR=/config/.platformio
ENV PLATFORMIO_HOME_DIR=/config/.platformio
ENV PIP_BREAK_SYSTEM_PACKAGES=1
ENV PIP_NO_CACHE_DIR=1

# Install base dependencies and build essentials
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    build-essential \
    git \
    dnsutils \
    vim \
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

# Install Python 3 + dependencies required by the PlatformIO IDE extension
# (python3-venv is required by PlatformIO on Linux; cmake/ninja are used by
# platforms such as ESP-IDF; clangd powers C/C++ IntelliSense).
RUN apt-get update && apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    python3-dev \
    cmake \
    ninja-build \
    clangd \
    dfu-util \
    libftdi1-2 \
    libusb-1.0-0 \
    usbutils \
    && rm -rf /var/lib/apt/lists/*

# Install PlatformIO Core (CLI) system-wide so `pio` works in any terminal
RUN pip3 install --break-system-packages platformio

# PlatformIO udev rules + grant the default user (abc) serial/USB device access
RUN mkdir -p /etc/udev/rules.d \
    && curl -fsSL https://raw.githubusercontent.com/platformio/platformio-core/develop/platformio/assets/system/99-platformio-udev.rules \
        -o /etc/udev/rules.d/99-platformio-udev.rules \
    && groupadd -f plugdev \
    && usermod -a -G dialout,plugdev abc

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


# Install Starship
RUN curl -sS https://starship.rs/install.sh | sh -s -- -y

# Add to ~/.bashrc
RUN echo 'eval "$(starship init bash)"' >> /etc/bash.bashrc

# Auto-install the PlatformIO IDE extension on first container boot
# (script lives in /custom-cont-init.d, run by linuxserver's s6 init as root)
COPY custom-cont-init.d/10-platformio.sh /custom-cont-init.d/10-platformio.sh
RUN chmod +x /custom-cont-init.d/10-platformio.sh
