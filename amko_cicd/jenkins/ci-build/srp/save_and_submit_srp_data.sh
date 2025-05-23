#!/bin/bash

set -xe


BRANCH=$branch
CI_REGISTRY_PATH=$PVT_DOCKER_REGISTRY/$PVT_DOCKER_REPOSITORY

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

#collecting source provenance data
PRODUCT_NAME="Avi Multi Kubernetes Operator"
JENKINS_INSTANCE=$(echo $JENKINS_URL | sed -E 's/^\s*.*:\/\///g' | sed -E 's/\///g')
COMP_UID="uid.obj.build.jenkins(instance='$JENKINS_INSTANCE',job_name='$JOB_NAME',build_number='$BUILD_NUMBER')"
provenance_source_file="$WORKSPACE/provenance/source.json"

# initialize credentials that are required for submission, Credentials value set by jenkins vault plugin
sudo /srp-tools/srp config auth --client-id=${SRP_CLIENT_ID} --client-secret=${SRP_CLIENT_SECRECT}

# initialize blank provenance in the working directory, $SRP_WORKING_DIR
sudo /srp-tools/srp provenance init --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-build jenkins --instance $JENKINS_INSTANCE --build-number $BUILD_NUMBER --job-name $JOB_NAME --working-dir $WORKSPACE/provenance

# add an action for the golang build, importing the observations that were captured in the build-golang-app step
sudo /srp-tools/srp provenance action start --name=amko-build --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance action import-observation --name=amko-obs --file=$WORKSPACE/provenance/network_provenance.json --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance action stop --working-dir $WORKSPACE/provenance

# declare the git source tree for the build.  We refer to this declaration below when adding source inputs.
sudo /srp-tools/srp provenance declare-source git --verbose --set-key=mainsrc --path=$WORKSPACE --branch=$BRANCH --working-dir $WORKSPACE/provenance

#Enable this option to create image manifest.json
export DOCKER_CLI_EXPERIMENTAL=enabled

CI_REGISTRY_IMAGE=$CI_REGISTRY_PATH/amko/$branch/amko
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
docker manifest inspect $CI_REGISTRY_IMAGE:${build_version} --insecure > amko_manifest.json
cat amko_manifest.json
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --manifest-path $WORKSPACE/amko_manifest.json --working-dir $WORKSPACE/provenance

CI_REGISTRY_IMAGE=$CI_REGISTRY_PATH/amko/$branch/amko-federator
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
docker manifest inspect $CI_REGISTRY_IMAGE:${build_version} --insecure > amko_federator_manifest.json
cat amko_federator_manifest.json
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-federator-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --manifest-path $WORKSPACE/amko_federator_manifest.json --working-dir $WORKSPACE/provenance

CI_REGISTRY_IMAGE=$CI_REGISTRY_PATH/amko/$branch/amko-service-discovery
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
docker manifest inspect $CI_REGISTRY_IMAGE:${build_version} --insecure > amko_service_discovery_manifest.json
cat amko_service_discovery_manifest.json
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-service-discovery-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --manifest-path $WORKSPACE/amko_service_discovery_manifest.json --working-dir $WORKSPACE/provenance

# use the syft plugin to scan the container and add all inputs it discovers. This will include the golang application we added
# to the container, which are duplicate of the inputs above, but in this case we KNOW they are incorporated.
sudo /srp-tools/srp provenance add-input syft --output-key=amko-image --usage functionality --incorporated true --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-input syft --output-key=amko-federator-image --usage functionality --incorporated true --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-input syft --output-key=amko-service-discovery-image --usage functionality --incorporated true --working-dir $WORKSPACE/provenance

# adding source input
sudo /srp-tools/srp provenance add-input source --source-key=mainsrc --output-key=amko-image --is-component-source --incorporated=true --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-input source --source-key=mainsrc --output-key=amko-federator-image --is-component-source --incorporated=true --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-input source --source-key=mainsrc --output-key=amko-service-discovery-image --is-component-source --incorporated=true --working-dir $WORKSPACE/provenance

# compile the provenance to a file and then dump it out to the console for reference
sudo /srp-tools/srp provenance compile --saveto $WORKSPACE/provenance/srp_prov3_fragment.json --working-dir $WORKSPACE/provenance
cat $WORKSPACE/provenance/srp_prov3_fragment.json

# submit the created provenance to SRP
sudo /srp-tools/srp provenance submit --verbose --path $WORKSPACE/provenance/srp_prov3_fragment.json --working-dir $WORKSPACE/provenance

provenance_path=$target_path/provenance
sudo mkdir -p $provenance_path
sudo cp $WORKSPACE/provenance/*json $provenance_path/;
