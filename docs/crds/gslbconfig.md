## GSLBConfig for AMKO
A CRD has been provided to add the GSLB configuration. The name of the object is `GSLBConfig` (short name is `gc`) and it has the following parameters:

```yaml
apiVersion: "avilb.k8s.io/v1alpha1"
kind: "GSLBConfig"
metadata:
  name: "amko-gc"
  namespace: "avi-system"
spec:
  gslbLeader:
    credentials: gslb-avi-secret
    controllerVersion: 20.1.1
    controllerIP: 10.10.10.10
    tenant: admin
  memberClusters:
    - clusterContext: cluster1-admin
    - clusterContext: cluster2-admin
  refreshInterval: 1800
  logLevel: "INFO"
  useCustomGlobalFqdn: false
```
1. `apiVersion`: The api version for this object has to be `avilb.k8s.io/v1alpha1`.
2. `kind`: the object kind is `GSLBConfig`.
3. `name`: Can be anything, but it has to be specified in the GDP object.
4. `namespace`: By default, this object must be created in `avi-system`.
5. `gslbLeader.credentials`: A secret object has to be created for (`helm install` does that automatically) the GSLB Leader cluster. The username and password have to be provided as part of this secret object.
6. `gslbLeader.controllerVersion`: The version of the GSLB leader cluster.
7. `gslbLeader.controllerIP`: The GSLB leader IP address or the hostname along with the port number, if any.
8. `gslbLeader.tenant`: The tenant where all the AMKO objects will be created in AVI.
9. `memberClusters`: The kubernetes/openshift cluster contexts which are part of this GSLB cluster. See [here](../kubeconfig.md#creating-a-multi-cluster-kubeconfig-file) to create contexts for multiple kubernetes clusters.
10.  `refreshInterval`: This is an internal cache refresh time interval, on which syncs up with the AVI objects and checks if a sync is required.
11. `logLevel`: Define the log level that the amko pod prints. The allowed levels are: `[INFO, DEBUG, WARN, ERROR]`.
12. `useCustomGlobalFqdn`: If set to true, AMKO will look for AKO HostRules to derive the GslbService name using the local to global fqdn mapping. If set to false (default case), AMKO ignores AKO HostRules and uses the default way of deriving GslbService names by just looking at the local fqdn in the ingress/route/service type LB. See [Local and Global Fqdn](../local_and_global_fqdn.md).

### Notes
* Only one `GSLBConfig` object is allowed.
* If using `helm install`, a `GSLBConfig` object is created by picking up values from the `values.yaml` file.
* During `helm delete`, the `GSLBConfig` that holds the UUID of the current instance is deleted. Hence a cleanup of stale GSLB services, if any, is required at the controller before re-installing AMKO.
