# Developer Guide

Thank you for taking the time out to contribute to AMKO!

This guide will walk you through the process of making your first commit and how
to effectively get it merged upstream.

- [Developer Guide](#developer-guide)
  - [Getting Started](#getting-started)
    - [CLA](#cla)
    - [Accounts Setup](#accounts-setup)
  - [Contribute](#contribute)
    - [GitHub Workflow](#github-workflow)
    - [Getting reviewers](#getting-reviewers)
    - [Building and testing your change](#building-and-testing-your-change)
    - [Dev testing on real setup](#dev-testing-on-real-setup)
    - [Running the end-to-end tests](#running-the-end-to-end-tests)
    - [Reverting a commit](#reverting-a-commit)

## Getting Started

To get started, let's ensure you have completed the following prerequisites for
contributing to AMKO:
1. Read and observe the [code of conduct](CODE_OF_CONDUCT.md).
2. Sign the [CLA](#cla).
3. Check out the [Architecture document](docs/architecture.md) for AMKO's architecture.
4. Set up necessary [accounts](#accounts-setup).
5. Set up your [development environment](#dev-testing-on-real-setup).

Now that you're setup, skip ahead to learn how to [contribute](#contribute). 

### CLA

We welcome contributions from everyone but we can only accept them if you sign
our Contributor License Agreement (CLA). If you would like to contribute and you
have not signed it, our CLA-bot will walk you through the process when you open
a Pull Request. For questions about the CLA process, see the
[FAQ](https://cla.vmware.com/faq) or submit a question through the GitHub issue
tracker.

### Accounts Setup

At minimum, you need the following accounts for effective participation:

1. **Github**: Committing any change requires you to have a [github
   account](https://github.com/join).


## Contribute

There are multiple ways in which you can contribute, either by contributing
code in the form of new features or bug-fixes or non-code contributions like
helping with code reviews, triaging of bugs, documentation updates, filing
[new issues](#filing-an-issue) or writing blogs/manuals etc.


### GitHub Workflow

Developers work in their own forked copy of the repository and when ready,
submit pull requests to have their changes considered and merged into the
project's repository.

1. Fork your own copy of the repository to your GitHub account by clicking on
   `Fork` button on [AMKO github repository](https://github.com/vmware/global-load-balancing-services-for-kubernetes).
2. Clone the forked repository on your local setup.
    ```
    git clone https://github.com/$user/global-load-balancing-services-for-kubernetes
    ```
    Add a remote upstream to track upstream AMKO repository.
    ```
    git remote add upstream https://github.com/vmware/global-load-balancing-services-for-kubernetes
    ```
    Never push to upstream master
    ```
    git remote set-url --push upstream no_push
    ```
3. Create a topic branch.
    ```
    git checkout -b branchName
    ```
4. Make changes and commit it locally.
    ```
    git add <modifiedFile>
    git commit
    ```
5. Update the "Unreleased" section of the [CHANGELOG](CHANGELOG.md) for any
   significant change that impacts users.
6. Keeping branch in sync with upstream.
    ```
    git checkout branchName
    git fetch upstream
    git rebase upstream/master
    ```
7. Push local branch to your forked repository.
    ```
    git push -f $remoteBranchName branchName
    ```
8. Create a Pull request on GitHub.
   Visit your fork at `https://github.com/vmware/global-load-balancing-services-for-kubernetes` and click
   `Compare & Pull Request` button next to your `remoteBranchName` branch.

### Getting reviewers

Once you have opened a Pull Request (PR), reviewers will be assigned to your
PR and they may provide review comments which you need to address.
Commit changes made in response to review comments to the same branch on your
fork. Once a PR is ready to merge, squash any *fix review feedback, typo*
and *merged* sorts of commits.

To make it easier for reviewers to review your PR, consider the following:
1. Follow the golang [coding conventions](https://github.com/golang/go/wiki/CodeReviewComments)
2. Follow [git commit](https://chris.beams.io/posts/git-commit/) guidelines.
3. Follow [logging](docs/logging.md) guidelines.

### Building and testing your change

To build the AMKO Docker image together with all AMKO bits, you can simply
do:

1. Checkout your feature branch.
2. Run `make docker`

The second step will compile the AMKO code in a `golang` container, and build
a `photon os` Docker image that includes all the generated binaries. [`Docker`](https://docs.docker.com/install)
must be installed on your local machine in advance. Ensure your docker has `multi-stage-build` support.

Alternatively, you can build the AMKO code in your local Go environment. The
AMKO uses the [Go modules support](https://github.com/golang/go/wiki/Modules) which was introduced in Go 1.11. It
facilitates dependency tracking and no longer requires projects to live inside
the `$GOPATH`.

To develop locally, you can follow these steps:

 1. [Install Go 1.13](https://golang.org/doc/install)
 2. Checkout your feature branch and `cd` into it.
 3. To build all Go files and install, run `make build`
 4. To run all Go unit tests, run `make test`

### Dev testing on real setup

AMKO can be tested from your laptop. Assuming that you have setup the following:
 1. At least one Avi Controller with GSLB setup.
 2. Created a multi-cluster kubeconfig file.
 3. Deployed AKO on member clusters.

Please follow the instructions to start AMKO:

 1. Set the following environment variables:
    ```
    export KUBECONFIG="/path/to/kubeconfig"
    export GSLB_CONFIG=`cat /path/to/multi-cluster-kube-config`
    ```
 2. Run `./bin/amko`

     You can control additional settings by exporting respective variables from inside the deployment [file](https://github.com/vmware/global-load-balancing-services-for-kubernetes/blob/master/helm/amko/templates/statefulset.yaml).

### Running the end-to-end tests

AMKO uses Jenkins pipelines currently to run a set of tests.
All tests are presently hosted inside VMware internal network:

 1. PR builder jobs, that run `make test`. Without this passing, you can't get the PR to merge.
 2. Daily smoke tests. 
 3. Nightly Functional Test suites that runs AMKO tests using a real controller/kubernetes/openshift setup.

### Reverting a commit

1. Create a branch in your forked repo
    ```
    git checkout -b revertName
    ```
2. Sync the branch with upstream
    ```
    git fetch upstream
    git rebase upstream/master
    ```
3. Create a revert based on the SHA of the commit.
    ```
    git revert SHA
    ```
4. Push this new commit.
    ```
    git push $remoteRevertName revertName
    ```
5. Create a Pull Request on GitHub.
   Visit your fork at `https://github.com/avinetworks/ako` and click
   `Compare & Pull Request` button next to your `remoteRevertName` branch.
