ARG GO_IMAGE=docker.m.daocloud.io/library/golang:1.24-alpine
ARG PYTHON_IMAGE=docker.m.daocloud.io/library/python:3.11-slim-bookworm
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS go-builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /build
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/rtc-demo .

FROM ${PYTHON_IMAGE}
WORKDIR /opt/rtc-demo
COPY --from=go-builder /out/rtc-demo /usr/local/bin/rtc-demo
COPY scripts/ ./scripts/
COPY token_lib/ ./token_lib/
COPY web-demo/ ./web-demo/
RUN mkdir -p /opt/certs \
    && mv web-demo/key/edge.rtcdevelopers.com.pem /opt/certs/server.pem \
    && mv web-demo/key/edge.rtcdevelopers.com-key.pem /opt/certs/server-key.pem \
    && rm -rf web-demo/key \
    && chmod 600 /opt/certs/server-key.pem
ENV APP_ROOT=/opt/rtc-demo \
    PORT=8800 \
    TLS_ENABLED=true \
    TLS_CERT_FILE=/opt/certs/server.pem \
    TLS_KEY_FILE=/opt/certs/server-key.pem \
    PYTHONUNBUFFERED=1 \
    GIN_MODE=release
EXPOSE 8800
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD python3 -c "import os,ssl,urllib.request; scheme='http' if os.getenv('TLS_ENABLED','true').lower()=='false' else 'https'; urllib.request.urlopen(scheme+'://127.0.0.1:'+os.getenv('PORT','8800')+'/healthz', context=ssl._create_unverified_context(), timeout=3).read()"
ENTRYPOINT ["/usr/local/bin/rtc-demo"]
