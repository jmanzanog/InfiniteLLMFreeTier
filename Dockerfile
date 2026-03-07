FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine AS builder
WORKDIR /src

# deps (cache-friendly)
COPY go.mod go.sum ./
RUN go mod download

# source
COPY . .

# build per target platform (no QEMU needed for compile)
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" \
    -o /out/infinitellm .

# final image: tiny + certs for HTTPS
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/infinitellm /infinitellm
# Copy config.yaml to root so the app can find it by default
COPY --from=builder /src/config.yaml /config.yaml

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/infinitellm"]
