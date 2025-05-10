## Troubleshooting guide for Avi Multi Kubernetes Operator

#### helm install failure

#####  Fails with a GlobalDeploymentPolicy error
```
Error: GlobalDeploymentPolicy.amko.vmware.com "global-gdp" is invalid: spec.matchRules: Invalid value: "null": spec.matchRules in body must be of type object: "null"
```

##### Reasons/Solutions:
At least one of `appSelector` or `namespaceSelector` must be present in the `values.yaml`.

#### AMKO POD is not running

##### Pod shows ImagePullBackOff
Check the status of the pod:
```
    $ kubectl get pods -n avi-system
    NAME                 READY   STATUS             RESTARTS   AGE
    amko-0               0/1     ImagePullBackOff   0          15s
```

##### Possible Reasons/Solutions:
Ensure that:
1. The docker registry is configured properly.
2. Or, the image is configured locally.
3. Or, the docker registry is reachable from where AMKO is deployed.


##### Pod shows CrashLoopBackOff and/or continuously restarts
Check the status of the pod:
```
    $ kubectl get pods -n avi-system
    NAME                 READY   STATUS             RESTARTS   AGE
    amko-0               0/1     CrashLoopBackOff   0          300s
```

##### Possible Reasons/Solutions:
If the liveness probe of the AMKO pod fails, then kubernetes/openshift will keep on restarting it. Liveness probe can fail because of following reasons:
1. AMKO is unable to obtain a connection to the Avi controller during bootup. Either check the amko logs or the status of the `GSLBConfig` object to confirm:
   ```
    $ kubectl get gc -n avi-system -o=jsonpath='{.items[0].status.state}{"\n"}'

    error: issue in connecting to the controller API, no avi clients initialized
   ```
  Please ensure that the connectivity to the controller is fine between AMKO and the Avi controller.

2. AMKO is unable to initialize a client for a member kubernetes or openshift cluster. Either check the amko logs or the status of the `GSLBConfig` object to confirm:
   ```
    $ kubectl get gc -n avi-system -o=jsonpath='{.items[0].status.state}{"\n"}'

    error: cluster healthcheck failed, cluster oshift health check failed, can't access the services api
   ```
Ensure that all the member clusters are reachable from where AMKO is running during bootup.


#### Added/Changed some fields in the GSLBConfig object, but no effect

##### Possible reasons/solutions

Only the `logLevel` field in the `GSLBConfig` is editable. Rest all other field changes in the `GSLBConfig` object requires a reboot of AMKO, for the changes to take effect.


#### AMKO Pod is up, but no GSLB service object created

##### Possible reasons/solutions

##### No selectors present in the GDP object

Check the `GDP` object:
```yaml
    matchRules:
      appSelector: {}
      namespaceSelector: {}
```
Please add a proper `appSelector` or a `namespaceSelector` in order to select an openshift or a kubernetes object. The labels provided here must match the labels present in a kubernetes object.

##### Selectors present, but can't select an object

Verify the `namespaceSelector` or `appSelector` filters on the `GlobalDeploymentPolicy` is able to select a
valid ingress/route/service type LoadBalancer object(s).

##### Invalid cluster context provided

If you provide an invalid cluster context in the `GDP` object, the status message of the `GDP` object will reflect
the reason for error as shown below:

```yaml
    spec:
      matchClusters:
      - oshift1
      - k8s
      matchRules:
        appSelector:
          label:
            app: gslb
        namespaceSelector: {}
    status:
      errorStatus: cluster context oshift1 not present in GSLBConfig
```
 Ensure that the cluster context is always the right non-empty value in the `GSLBConfig` object. Also ensure that the cluster contexts present in the `GDP` and `GSLBConfig` object must be present in the `gslb-config-secret` created as part of the installation [pre-requisites](../README.md#pre-requisites).

##### Traffic split Invalid values

The traffic split values in the GDP object should be between 1 to 20 (1 and 20 included). Any other value
will result into an error on the GDP status object:

```yaml
     spec:
        matchClusters:
        - oshift
        - k8s
        matchRules:
          appSelector:
            label:
              app: gslb
          namespaceSelector: {}
        trafficSplit:
        - cluster: oshift
          weight: 10
        - cluster: k8s
          weight: 50
      status:
        errorStatus: traffic weight 50 must be between 1 and 20
```

##### Specified namespaceSelector and appSelector both but yet the GslbServices are not created

The `namespaceSelector` and the `appSelector` are 'AND'ed while searching for a given application FQDN. Hence
the ingress's app selector label must belong to an ingress object is also selected by the namespaceSelector.

Either remove the `namespaceSelector` or ensure that the `namespaceSelector` belongs to the namespace of the ingress object.

##### Selected applications properly but still GslbServices are not created

Check if the DNS sub-domain of the applications are configured in the Avi controller for the GS DNS VS.


##### GSLB leader flipped

If the GSLB leader becomes follower and the configuration is not updated on AMKO via the GSLBConfig, the GS objects
won't get created. This can be verified by looking at the GSLBConfig object's status message:

```yaml
    spec:
      gslbLeader:
        controllerIP: 10.10.10.10
        controllerVersion: 20.1.1
        credentials: gslb-avi-secret
      logLevel: DEBUG
      memberClusters:
      - clusterContext: oshift
      - clusterContext: k8s
      refreshInterval: 300
    status:
      state: 'error: controller not a leader'
```

##### Selected an object via selectors and added the same label to an ingress object, still no GslbService

Ensure that the ingress object has an IP address in it's status field. If its not there, ensure that AKO is properly confirured and running.


#### Removed an ingress, but still GSLB service is up

##### Possible Reason/Solution

Please check if the FQDN is present in any other ingress object which is still active.
AMKO uses a set of custom HTTP health monitors to determine the health of a GSLB service.
The custom health monitors are created per host per path. Hence all host/path combinations for a given
FQDN should be removed in order for the corresponding GSLB service to fail health monitor.

#### Existing GSLB services are not modified on change in ingress after re-install of AMKO 

##### Possible Reason/Solution

During `helm uninstall`, the `GSLBConfig` that holds the UUID as annotation of the current instance is deleted.When Amko is installed again it will create a new `GSLBConfig` with a different UUID. This causes discrepancy between already created GSLB services on controller and ingress/route  on cluster.


Follow below steps to maintain the correct state of AMKO during reinstall
1. Before you uninstall AMKO conserve the amko-UUID from annotations of GSLBconfig. 
2. Add it in `configs.amkoUUID` field of [values.yaml](../../README.md#parameters) during reinstall. Otherwise a cleanup of stale GSLB services, if any, is required at the controller before re-installing AMKO.

Example of amko-uuid in GSLBConfig :
  ```yaml
  annotations:
      amko.vmware.com/amko-uuid: b3923b8e-7bff-11ee-8972-a24a90367d8f
  ```
## Log Collection

For every log collection, also collect the following information:

    1. What kubernetes distribution are you using? For example: RKE, PKS etc.
    2. What is the CNI you are using with versions? For example: Calico v3.15
    3. What is the Avi Controller version you are using? For example: 20.1.1

### How do I gather the AMKO logs?

Get the script from [here](https://github.com/avinetworks/devops/tree/master/tools/ako/log_collector.py)

The script is used to collect all relevant information for the AMKO pod.

**About the script:**

1. Collects log file of AMKO pod
2. Collects `GSLBConfig` and `GlobalDeploymentPolicy` objects  in a yaml file
3. Zips the folder and returns

_For logs collection, 3 cases are considered:_

Case 1 : A running AMKO pod logging into a Persistent Volume Claim, in this case the logs are collected from the PVC that the pod uses.

Case 2 : A running AMKO pod logging into console, in this case the logs are collected from the pod directly.

Case 3 : A dead AMKO pod that uses a Persistent Volume Claim, in this case a backup pod is created with the same PVC attached to the AMKO pod and the logs are collected from it.

**Configuring PVC for the AMKO pod:**

We recommend using a Persistent Volume Claim for the amko pod. Refer this [link](https://kubernetes.io/docs/tasks/configure-pod-container/configure-persistent-volume-storage/) to create a persistent volume(PV) and a Persistent Volume Claim(PVC). 

Below is an example of hostpath persistent volume. We recommend you use the PV based on the storage class of your kubernetes environment. 

```yaml
    # persistent-volume.yaml
    apiVersion: v1
    kind: PersistentVolume
    metadata:
      name: amko-pv
      namespace : avi-system
      labels:
        type: local
    spec:
      storageClassName: manual
      capacity:
        storage: 10Gi
      accessModes:
        - ReadWriteOnce
      hostPath:
        path: <any-host-path-dir>  # make sure that the directory exists
```

A persistent volume claim can be created using the following file

```yaml
    # persistent-volume-claim.yaml
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
      name: amko-pvc
      namespace : avi-system
    spec:
      storageClassName: manual
      accessModes:
        - ReadWriteOnce
      resources:
        requests:
          storage: 3Gi
```

Add PVC name into the amko/helm/amko/values.yaml before the creation of the amko pod like 

```yaml
    persistentVolumeClaim: amko-pvc
    mountPath: /log
    logFile: avi.log
```

**How to use the script for AMKO**

Usage:

1. Case 1: With PVC, (Mandatory) --amkoNamespace (-amko) : The namespace in which the AMKO pod is present.

    `python3 log_collections.py -amko avi-system`

2. Case 2: Without PVC (Optional) --since (-s) : time duration from present time for logs.

    `python3 log_collections.py -amko avi-system -s 24h`

**Sample Run:**

At each stage of execution, the commands being executed are logged on the screen.
The results are stored in a zip file with the format below:

    amko-<helmchart name>-<current time>

Sample Output with PVC :

```
    2020-09-25 13:20:37,141 - ******************** AMKO ********************
    2020-09-25 13:20:37,141 - For AMKO : helm list -n avi-system
    2020-09-25 13:20:38,974 - kubectl get pod -n avi-system -l app.kubernetes.io/instance=my-amko-release
    2020-09-25 13:20:41,850 - kubectl describe pod amko-56887bd5b7-c2t6n -n avi-system
    2020-09-25 13:20:44,019 - helm get all my-amko-release -n avi-system
    2020-09-25 13:20:46,360 - PVC name is my-pvc
    2020-09-25 13:20:46,361 - PVC mount point found - /log
    2020-09-25 13:20:46,361 - Log file name is avi.log
    2020-09-25 13:20:46,362 - Creating directory amko-my-amko-release-2020-06-25-132046
    2020-09-25 13:20:46,373 - kubectl cp avi-system/amko-56887bd5b7-c2t6n:log/avi.log amko-my-amko-release-2020-06-25-132046/amko.log
    2020-09-25 13:21:02,098 - kubectl get cm -n avi-system -o yaml > amko-my-amko-release-2020-06-25-132046/config-map.yaml
    2020-09-25 13:21:03,495 - Zipping directory amko-my-amko-release-2020-06-25-132046
    2020-09-25 13:21:03,525 - Clean up: rm -r amko-my-amko-release-2020-06-25-132046

    Success, Logs zipped into amko-my-amko-release-2020-06-25-132046.zip
```

### How do I gather the controller tech support?

It's recommended we collect the controller tech support logs as well. Please follow this [link](https://avinetworks.com/docs/18.2/collecting-tech-support-logs/)  for the controller tech support.
