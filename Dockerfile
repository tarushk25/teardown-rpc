FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /teardown-rpc main.go && \
    echo 'nonroot:x:65532:65532:nonroot:/nonexistent:/sbin/nologin' > /passwd

FROM scratch
COPY --from=builder /passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /teardown-rpc /teardown-rpc
EXPOSE 8545
USER nonroot
ENTRYPOINT ["/teardown-rpc"]
