#!/bin/bash

set -xe


source_image=$DOCKER_IMAGE_NAME:latest

version_tag=$($WORKSPACE/hack/jenkins/get_build_version.sh $JOB_NAME $BUILD_NUMBER)

target_image=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY/$DOCKER_IMAGE_NAME:$version_tag

sudo docker tag $source_image $target_image

sudo docker push $target_image

source_image=$DOCKER_AMKO_FEDERATOR_IMAGE_NAME:latest

target_image=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY/$DOCKER_AMKO_FEDERATOR_IMAGE_NAME:$version_tag

sudo docker tag $source_image $target_image

sudo docker push $target_image

source_image=$DOCKER_AMKO_SERVICE_DISCOVERY_IMAGE_NAME:latest

target_image=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY/$DOCKER_AMKO_SERVICE_DISCOVERY_IMAGE_NAME:$version_tag

sudo docker tag $source_image $target_image

sudo docker push $target_image
