#!/bin/bash

set -xe


AMKO_IMAGES=($DOCKER_IMAGE_NAME $DOCKER_AMKO_FEDERATOR_IMAGE_NAME $DOCKER_AMKO_SERVICE_DISCOVERY_IMAGE_NAME)
version_tag=$($WORKSPACE/hack/jenkins/get_build_version.sh $JOB_NAME $BUILD_NUMBER)

echo ${AMKO_IMAGES[@]}

for image in "${AMKO_IMAGES[@]}"
do
  source_image=$image:latest
  target_image=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY/amko/${branch,,}/$image:$version_tag
  sudo docker tag $source_image $target_image
  sudo docker push $target_image
done
