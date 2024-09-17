FROM golang:1.22 AS builder

WORKDIR /work
COPY . .

ENV CGO_ENABLED=0
RUN go build -v -o stunmesh

FROM scratch

WORKDIR /app
COPY --from=builder /work/stunmesh /app/stunmesh

CMD ["/app/stunmesh"]
