# Deterministic Prometheus-compatible metrics fixture for the e2e harness.
# Base images match the production Dockerfile so local layer caches are shared
# and preflight base-image resolution covers both builds.
FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.mod
COPY test/e2e/metricsfixture/ test/e2e/metricsfixture/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o metricsfixture ./test/e2e/metricsfixture

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/metricsfixture .
USER 65532:65532

ENTRYPOINT ["/metricsfixture"]
