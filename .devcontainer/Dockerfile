FROM mcr.microsoft.com/vscode/devcontainers/go:1.24

RUN apt-get update && apt-get install -y docker.io && rm -rf /var/lib/apt/lists/*

ARG USERNAME=vscode
RUN usermod -aG docker ${USERNAME}

ENV GONOSUMDB=* \
    GONOPROXY=* \
    GOPROXY=https://proxy.golang.org,direct

# Pre-install Go tools
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install golang.org/x/tools/cmd/goimports@latest
