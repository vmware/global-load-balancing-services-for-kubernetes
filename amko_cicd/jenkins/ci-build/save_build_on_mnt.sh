#!/bin/bash

set -xe

BRANCH=$branch
CI_REGISTRY_PATH=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY

echo $(git rev-parse origin/${branch}) > $WORKSPACE/HEAD_COMMIT;
cat $WORKSPACE/HEAD_COMMIT

# Function to get GIT workspace root location
function get_git_ws {
    git_ws=$(git rev-parse --show-toplevel)
    [ -z "$git_ws" ] && echo "Couldn't find git workspace root" && exit 1
    echo $git_ws
}

BRANCH_VERSION_SCRIPT=$WORKSPACE/hack/jenkins/get_branch_version.sh
# Compute base_build_num
base_build_num=$(cat $(get_git_ws)/base_build_num)
version_build_num=$(expr "$base_build_num" + "$BUILD_NUMBER")
branch_version=$(bash $BRANCH_VERSION_SCRIPT)

BUILD_VERSION_SCRIPT=$WORKSPACE/hack/jenkins/get_build_version.sh
CHARTS_PATH="$(get_git_ws)/helm/amko"

build_version=$(bash $BUILD_VERSION_SCRIPT "dummy" $BUILD_NUMBER)

target_path=/mnt/builds/amko_OS/$BRANCH/ci-build-$build_version

sudo mkdir -p $target_path

sudo cp -r $CHARTS_PATH/* $target_path/

set +e
sudo cp "$(get_git_ws)/HEAD_COMMIT" $target_path/

if [ "$?" != "0" ]; then
    echo "ERROR: Could not save the head commit file into target path"
fi

set -e

sudo sed -i --regexp-extended "s/^(\s*)(appVersion\s*:\s*latest\s*$)/\1appVersion: $build_version/" $target_path/Chart.yaml

#Save ako images as tarball
sudo docker save -o amko.tar amko:latest
sudo cp -r amko.tar $target_path/

sudo docker save -o amko-federator.tar amko-federator:latest
sudo cp -r amko-federator.tar $target_path/

sudo docker save -o amko-service-discovery.tar amko-service-discovery:latest
sudo cp -r amko-service-discovery.tar $target_path/
echo "Docker image tar files generated and stored succssfully..."
