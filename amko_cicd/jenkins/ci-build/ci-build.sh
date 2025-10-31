#!/bin/bash

set -xe

export GOLANG_SRC_REPO=avi-alb-docker-virtual.packages.vcfd.broadcom.net/golang:latest
export PHOTON_SRC_REPO=photonos-docker-local.packages.vcfd.broadcom.net/photon5-amd64:latest

export PATH=$PATH:/usr/local/go/bin
go version

make build

make BUILD_TAG=$version_tag docker
make BUILD_TAG=$version_tag amko-federator-docker
make BUILD_TAG=$version_tag amko-service-discovery-docker

if [ "$RUN_TESTS" = true ]; then
    make test
fi

if [ "$RUN_INT_TESTS" = true ]; then
    make int_test
fi
