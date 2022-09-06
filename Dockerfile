
FROM golang:1.16.2-alpine as builder

ADD . /koinos-jsonrpc
WORKDIR /koinos-jsonrpc

RUN apk update && \
    apk add \
        gcc \
        musl-dev \
        linux-headers

RUN go get ./... && \
    go build -o koinos_jsonrpc cmd/koinos-jsonrpc/main.go

FROM alpine:latest
COPY --from=builder /koinos-jsonrpc/koinos_jsonrpc /usr/local/bin
ENTRYPOINT [ "/usr/local/bin/koinos_jsonrpc" ]
