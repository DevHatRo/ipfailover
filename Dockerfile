# Docker image for IP Failover
FROM gcr.io/distroless/static-debian12:nonroot

ENV APP_NAME=ipfailover
ENV APP_ENV=production
ENV APP_PORT=8080

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app

COPY bin/ipfailover-${TARGETOS}-${TARGETARCH} /app/ipfailover

EXPOSE 8080

VOLUME ["/app/config"]

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/app/ipfailover", "-health-check", "-config", "/app/config/config.yaml"]

# Default command
ENTRYPOINT ["/app/ipfailover"]
CMD ["-config", "/app/config/config.yaml"]
