#!/bin/bash

set -xe

export GOLANG_SRC_REPO=avi-alb-docker-virtual.packages.vcfd.broadcom.net/golang:latest
export PHOTON_SRC_REPO=avi-alb-docker-virtual.packages.vcfd.broadcom.net/photon:5.0

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
