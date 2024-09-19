FROM golang:1.22 AS builder

WORKDIR /work
COPY . .

ENV CGO_ENABLED=0
RUN go build -v -o stunmesh-go

FROM scratch

WORKDIR /app
COPY --from=builder /work/stunmesh-go /app/stunmesh-go

CMD ["/app/stunmesh-go"]
