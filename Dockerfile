FROM golang:latest AS builder

WORKDIR /work
COPY . .

# Build main application
RUN make

# Build all plugins
RUN make plugin

FROM scratch

WORKDIR /app

# Copy main application
COPY --from=builder /work/stunmesh-go /app/stunmesh-go

# Copy all plugins (automatically includes any new plugins)
COPY --from=builder /work/contrib/*/stunmesh-* /app/plugins/

CMD ["/app/stunmesh-go"]
