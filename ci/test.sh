#!/bin/bash

set -e
set -x

if [[ -z $BUILD_DOCKER ]]; then
   golint -set_exit_status ./...
else
   export JSONRPC_TAG=$TAG

   git clone https://github.com/koinos/koinos-integration-tests.git

   cd koinos-integration-tests
   go get ./...
   cd tests
   ./run.sh
fi
