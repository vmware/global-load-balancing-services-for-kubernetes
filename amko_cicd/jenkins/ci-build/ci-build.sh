#!/bin/bash

set -xe

export GOLANG_SRC_REPO=${PVT_DOCKER_REGISTRY}/dockerhub-proxy-cache/library/golang:latest
export PHOTON_SRC_REPO=${PVT_DOCKER_REGISTRY}/dockerhub-proxy-cache/library/photon:5.0

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
