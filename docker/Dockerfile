FROM golang:1.22 as builder

COPY . /work
WORKDIR /work

RUN go build -v -o stunmesh

FROM scratch

WORKDIR /
copy --from=builder /work/stunmesh .

CMD ["/stunmesh"]
