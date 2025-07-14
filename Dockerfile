FROM golang:latest AS builder

WORKDIR /work
COPY . .

RUN make

FROM scratch

WORKDIR /app
COPY --from=builder /work/stunmesh-go /app/stunmesh-go

CMD ["/app/stunmesh-go"]
