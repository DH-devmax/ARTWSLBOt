FROM golang:1.27.3-alpine
RUN apk add --no-cache build-base pkgconf libsodium-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
CMD ["go","test","-tags","sodium","-v","./internal/nativewire","./internal/nativeauth"]
