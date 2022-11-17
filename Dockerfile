
FROM golang:1.16.2-alpine as builder

ADD . /koinos-jsonrpc
WORKDIR /koinos-jsonrpc

RUN apk update && \
    apk add \
        gcc \
        musl-dev \
        linux-headers \
        git

RUN go get ./... && \
    go build -ldflags="-X main.Commit=$(git rev-parse HEAD)" -o koinos_jsonrpc cmd/koinos-jsonrpc/main.go

FROM alpine:latest
COPY --from=builder /koinos-jsonrpc/koinos_jsonrpc /usr/local/bin
ENTRYPOINT [ "/usr/local/bin/koinos_jsonrpc" ]
