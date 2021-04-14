#!/bin/bash

set -e
set -x

if [[ -z $BUILD_DOCKER ]]; then
   go get ./...
   mkdir -p build
   go build -o build/koinos_jsonrpc cmd/koinos-jsonrpc/main.go
else
   docker build . -t koinos-jsonrpc
fi
