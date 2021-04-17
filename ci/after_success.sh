#!/bin/bash

#coveralls-lcov --repo-token "$COVERALLS_REPO_TOKEN" --service-name travis-pro ./build/merged.info

if ! [[ -z $BUILD_DOCKER ]]; then
   echo "$DOCKER_PASSWORD" | docker login -u $DOCKER_USERNAME --password-stdin
   docker tag koinos-jsonrpc koinos/koinos-jsonrpc:$TAG
   docker push koinos/koinos-jsonrpc:$TAG
fi
