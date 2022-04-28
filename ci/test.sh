#!/bin/bash

set -e
set -x

if [[ -z $BUILD_DOCKER ]]; then
   golangci-lint run ./...
else
   TAG="$TRAVIS_BRANCH"
   if [ "$TAG" = "master" ]; then
      TAG="latest"
   fi

   export JSONRPC_TAG=$TAG

   git clone https://github.com/koinos/koinos-integration-tests.git

   cd koinos-integration-tests
   go get ./...
   ./run.sh
fi
