ARG GO_IMAGE=docker.m.daocloud.io/library/golang:1.24-alpine
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /build
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/rtc-log-receiver .
FROM scratch
COPY --from=builder /out/rtc-log-receiver /rtc-log-receiver
EXPOSE 6444
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 CMD ["/rtc-log-receiver", "-healthcheck"]
ENTRYPOINT ["/rtc-log-receiver"]
