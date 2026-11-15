FROM golang:1.27.4-alpine
RUN apk add --no-cache build-base pkgconf libsodium-dev python3 py3-yaml git
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/gopsutil ./third_party/gopsutil
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY ai ./ai
COPY tools ./tools
COPY docs ./docs
COPY web/src/wsl ./web/src/wsl
COPY web/vite.dev.config.ts ./web/
COPY deploy ./deploy
COPY .github ./.github
COPY compose.yaml compose.local.yaml compose.dev.yaml upstream.lock.json AGENTS.md README.md ./
ENTRYPOINT ["go"]
CMD ["test","-race","-tags","sodium","./..."]
