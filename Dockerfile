FROM --platform=$BUILDPLATFORM golang:latest AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /work
COPY . .

# Build main application (cross-compile on build platform).
# EMBED_CA=1: the final image is FROM scratch with no CA store, so HTTPS
# plugins need the embedded Mozilla roots (inert when a CA volume is mounted).
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} make EMBED_CA=1

# Build all plugins (cross-compile on build platform)
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} make plugin

FROM scratch

WORKDIR /app

# Copy main application
COPY --from=builder /work/stunmesh-go /app/stunmesh-go

# Copy all plugins to /app (automatically includes any new plugins)
COPY --from=builder /work/contrib/*/stunmesh-* /app/

# Set PATH to include /app directory
ENV PATH="/app:${PATH}"

CMD ["/app/stunmesh-go"]
