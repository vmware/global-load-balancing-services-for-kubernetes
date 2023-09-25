#!/bin/bash

set -xe


if [ $# -lt 5 ] ; then
    echo "Usage: ./save_build.sh <BRANCH> <BUILD_NUMBER> <WORKSPACE> <JENKINS_JOB_NAME> <JENKINS_URL>";
    exit 1
fi

BRANCH=$1
BUILD_NUMBER=$2
WORKSPACE=$3
JENKINS_JOB_NAME=$4
JENKINS_URL=$5

SCRIPTPATH="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

# Function to get GIT workspace root location
function get_git_ws {
    git_ws=$(git rev-parse --show-toplevel)
    [ -z "$git_ws" ] && echo "Couldn't find git workspace root" && exit 1
    echo $git_ws
}

BRANCH_VERSION_SCRIPT=$SCRIPTPATH/get_branch_version.sh
# Compute base_build_num
base_build_num=$(cat $(get_git_ws)/base_build_num)
version_build_num=$(expr "$base_build_num" + "$BUILD_NUMBER")
branch_version=$(bash $BRANCH_VERSION_SCRIPT)

BUILD_VERSION_SCRIPT=$SCRIPTPATH/get_build_version.sh
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

#collecting source provenance data
PRODUCT_NAME="Avi Multi Kubernetes Operator"
JENKINS_INSTANCE=$(echo $JENKINS_URL | sed -E 's/^\s*.*:\/\///g' | sed -E 's/:.*//g')
COMP_UID="uid.obj.build.jenkins(instance='$JENKINS_INSTANCE',job_name='$JENKINS_JOB_NAME',build_number='$BUILD_NUMBER')"
provenance_source_file="$WORKSPACE/provenance/source.json"

# initialize credentials that are required for submission, Credentials value set by jenkins vault plugin
sudo /srp-tools/srp config auth --client-id=${SRP_CLIENT_ID} --client-secret=${SRP_CLIENT_SECRECT}

# initialize blank provenance in the working directory, $SRP_WORKING_DIR
sudo /srp-tools/srp provenance init --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance add-build jenkins --instance $JENKINS_INSTANCE --build-number $BUILD_NUMBER --job-name $JENKINS_JOB_NAME --working-dir $WORKSPACE/provenance

# add an action for the golang build, importing the observations that were captured in the build-golang-app step
sudo /srp-tools/srp provenance action start --name=amko-build --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance action import-observation --name=amko-obs --file=$WORKSPACE/provenance/network_provenance.json --working-dir $WORKSPACE/provenance
sudo /srp-tools/srp provenance action stop --working-dir $WORKSPACE/provenance

# Add an action for make build itself.  Note that it does not actually record the real command line here.  Also note
# that we have no observations for the make build as it is not currently possible to observe
#sudo /srp-tools/srp provenance action start --name=docker-bld --working-dir $WORKSPACE/provenance
#sudo /srp-tools/srp provenance action import-cmd  --cmd 'make docker' --working-dir $WORKSPACE/provenance
#sudo /srp-tools/srp provenance action stop --working-dir $WORKSPACE/provenance

# declare the git source tree for the build.  We refer to this declaration below when adding source inputs.
sudo /srp-tools/srp provenance declare-source git --verbose --set-key=mainsrc --path=$WORKSPACE --branch=$BRANCH --working-dir $WORKSPACE/provenance

CI_REGISTRY_IMAGE=10.79.172.11:5000/avi-buildops/amko
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --working-dir $WORKSPACE/provenance

CI_REGISTRY_IMAGE=10.79.172.11:5000/avi-buildops/amko-federator
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-federator-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --working-dir $WORKSPACE/provenance

CI_REGISTRY_IMAGE=10.79.172.11:5000/avi-buildops/amko-service-discovery
IMAGE_DIGEST=`sudo docker images $CI_REGISTRY_IMAGE  --digests | grep sha256 | xargs | cut -d " " -f3`
echo $IMAGE_DIGEST
sudo /srp-tools/srp provenance add-output package.oci --set-key=amko-service-discovery-image --action-key=amko-build --name=${CI_REGISTRY_IMAGE}  --digest=${IMAGE_DIGEST} --working-dir $WORKSPACE/provenance
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

# Function to run srp source provenance command
#function source_provenance() {
#    sudo /srp-tools/srp provenance source --scm-type git --name "$PRODUCT_NAME" --path ./ --saveto $provenance_source_file --comp-uid $COMP_UID --build-number ${version_build_num} --version $branch_version --all-ephemeral true --build-type release $@
#}

#output=( $(find $WORKSPACE/ -type d  -not -path "$WORKSPACE/build/*" -name '.git') )
#for line in "${output[@]}"
#do
#    cd $(dirname $line)
#    if [ -f  $provenance_source_file ]
#    then
#        source_provenance --append
#    else
#        source_provenance
#    fi
#done
#cd $WORKSPACE
#sudo /srp-tools/srp provenance merge --source ./provenance/source.json --network ./provenance/provenance.json --saveto ./provenance/merged.json
provenance_path=$target_path/provenance
sudo mkdir -p $provenance_path
sudo cp $WORKSPACE/provenance/*json $provenance_path/;
