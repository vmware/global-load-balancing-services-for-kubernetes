## AMKOCluster CRD for AMKO
AMKO requires a CRD called `AMKOCluster` to federate the following objects to a list of member clusters:
1. `GSLBConfig` object
2. `GlobalDeploymentPolicy` object (GDP)

A typical `AMKOCluster` object looks like this:
```yaml
apiVersion: amko.vmware.com/v1alpha1
kind: AMKOCluster
metadata:
  name: amkocluster-sample
  namespace: avi-system
spec:
  isLeader: true
  clusterContext: cluster1
  version: 1.4.2
  clusters:
  - cluster1
  - cluster2
```
1. `namespace`: namespace of this object must be `avi-system`.
2. `isLeader`: Users must specify whether the AMKO in the current cluster is leader. Default value is `false`. If set to `false`, AMKO won't sync any objects to the Avi Controller, and the AMKO federator won't federate the objects to the member clusters.
3. `clusterContext`: Users must specify the current cluster's context.
4. `version`: Current cluster's AMKO version.
5. `clusters`: Member cluster list on which federation will be performed. Current cluster (if present) in this list will be ignored.

