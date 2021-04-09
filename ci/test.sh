#!/bin/bash

set -e
set -x

golint -set_exit_status ./...
