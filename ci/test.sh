#!/bin/bash

set -e
set -x

if [[ -z $BUILD_DOCKER ]]; then
   golint -set_exit_status ./...
fi
