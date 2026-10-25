FROM golang:1.27.0-alpine AS build
RUN apk add --no-cache gcc musl-dev pkgconf libsodium-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -tags sodium -trimpath -o /wsl ./cmd/wsl \
    && CGO_ENABLED=0 go build -trimpath -o /heatbot ./cmd/heatbot \
    && CGO_ENABLED=0 go build -trimpath -o /wslctl ./cmd/wslctl
FROM alpine:3.22
RUN apk add --no-cache ca-certificates libsodium && addgroup -S wsl && adduser -S -G wsl wsl
COPY --from=build /wsl /usr/local/bin/wsl
COPY --from=build /heatbot /usr/local/bin/heatbot
COPY --from=build /wslctl /usr/local/bin/wslctl
USER wsl
ENTRYPOINT ["wsl"]
CMD ["serve"]
