FROM golang:1.22 AS builder

COPY . /work
WORKDIR /work

RUN go build -v -o stunmesh

FROM scratch

WORKDIR /
COPY --from=builder /work/stunmesh .

CMD ["/stunmesh"]
