# AMKO: Avi Multi Kubernetes Operator

## What's AMKO?
AMKO provides application load-balancing across multiple clusters using AVI's enterprise grade GSLB capabilities.

GSLB - Load balancing across instances of the application that have been deployed to multiple locations (typically, multiple data centers and/or public clouds). Avi uses the Domain Name System (DNS) for providing the optimal destination information to the user clients 

## Prerequisites
Few things are needed before we can kickstart AMKO:
1. Atleast one kubernetes/openshift cluster.
2. Atleast one AVI controller which manages the above kubernetes/openshift cluster. Additional controllers with openshift/kubernetes clusters can be added. 
3. One controller (site) designated as the GSLB leader site. Enable GSLB on the required controller and the other controllers (sites) as the follower nodes.
4. AMKO assumes that it has connectivity to all the member clusters' kubernetes API servers. Without this, AMKO wouldn't be able to watch over the ingress/route/services objects in the member kubernetes clusters.
5. Designate one openshift/kubernetes cluster which will be communicating with the GSLB leader. All the configs for `amko` will be added to this cluster. We will call this cluster, `cluster-amko`.
6. Create a namespace `avi-system` in `cluster-amko`:
```
kubectl create ns avi-system
```
6. Create a kubeconfig file with the permissions to read the service and ingress/route objects for multiple clusters. See how to create a kubeconfig file for multiple clusters [here](#Multi-cluster-kubeconfig). Name this file `gslb-members` and generate a secret with the kubeconfig file in `cluster-amko` by following:
```
kubectl --kubeconfig my-config create secret generic gslb-config-secret --from-file gslb-members -n avi-system
```
**Note** that the permissions provided in the kubeconfig file above, for multiple clusters are important. They should contain permissions to at least `[get, list, watch]` on kubernetes services and ingresses (routes for openshift).

## Install using helm
The next step is to use helm to bootstrap amko:
```
helm install amko --generate-name --namespace=avi-system --set configs.gslbLeaderController="10.10.10.10" 
```
Use the [values.yaml](helm/amko/values.yaml) to edit values related to Avi configuration. Please refer to the [parameters](#parameters).

## Uninstall using helm
```
helm uninstall -n avi-system <release_name>
```
If a user needs to remove the already created GSLB services, one has to remove the GDP object first. This would prompt a deletion of all the GSLB Services selected via that GDP object.

```
kubectl delete gdp -n avi-system <gdp_name>
```

## parameters
| **Parameter**                                                 | **Description**                                                                                                          | **Default**                           |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------- |
| `configs.gslbLeaderController`                                | GSLB leader controller version                                                                                           | 18.2.9                                |
| `gslbLeaderCredentials.username`                              | GSLB leader controller username                                                                                          | `admin`                               |
| `gslbLeaderCredentials.password`                              | GSLB leader controller password                                                                                          | `avi123`                              |
| `configs.memberClusters.clusterContext`                       | K8s member cluster context for GSLB                                                                                      | `cluster1-admin` and `cluster2-admin` |
| `configs.refreshInterval`                                     | The time interval which triggers a AVI cache refresh                                                                     | 120 seconds                           |
| `configs.logLevel`                                            | Log level to be used                                                                                                     | `INFO`                                |
| `globalDeploymentPolicy.appSelector.label{.key,.value}`       | Selection criteria for applications, label key and value are provided                                                    | Nil                                   |
| `globalDeploymentPolicy.namespaceSelector.label{.key,.value}` | Selection criteria for namespaces, label key and value are provided                                                      | Nil                                   |
| `globalDeploymentPolicy.matchClusters`                        | List of clusters (names must match the names in configs.memberClusters) from where the objects will be selected          | Nil                                   |
| `globalDeploymentPolicy.trafficSplit`                         | List of weights for clusters (names must match the names in configs.memberClusters), each weight must range from 1 to 20 | Nil                                   |

## Use the GSLBConfig CRD
A CRD has been provided to add the GSLB configuration. The name of the object is GSLBConfig and it has the following parameters:
```yaml
apiVersion: "avilb.k8s.io/v1alpha1"
kind: "GSLBConfig"
metadata:
  name: "gslb-policy-1"
  namespace: "avi-system"
spec:
  gslbLeader:
    credentials: gslb-avi-secret
    controllerVersion: 18.2.9
    controllerIP: 10.10.10.10
  memberClusters:
    - clusterContext: cluster1-admin
    - clusterContext: cluster2-admin
  refreshInterval: 1800
  logLevel: "INFO"
```
1. `apiVersion`: The api version for this object has to be `avilb.k8s.io/v1alpha1`.
2. `kind`: the object kind is `GSLBConfig`.
3. `metadata.name`: Can be anything, but it has to be specified in the GDP object.
4. `metada.namespace`: By default, this object is supposed to be created in `avi-system`.
5. `spec.gslbLeader.credentials`: A secret object has to be created for (`helm install` does that automatically) the GSLB Leader cluster. The username and password have to be provided as part of this secret object. Refer to `username` and `password` in [parameters](#parameters).
6. `spec.gslbLeader.controllerVersion`: The version of the GSLB leader cluster.
7. `spec.gslbLeader.controllerIP`: The GSLB leader IP address or the hostname along with the port number, if any.
8. `spec.memberClusters`: The kubernetes/openshift cluster contexts which are part of this GSLB cluster. See [here](#Multi-cluster kubeconfig) to create contexts for multiple kubernetes clusters.
9.  `spec.refreshInterval`: This is an internal cache refresh time interval, on which syncs up with the AVI objects and checks if a sync is required.
10. `spec.logLevel`: Specify the required types of logs that should be printed by AMKO. There are currently 4 supported types: `INFO`, `DEBUG`, `WARN` and `ERROR`.

**Few Notes**:
- Only one GSLBConfig object is allowed.
- If using `helm install`, the GSLB Config object is created, just provide the right parameters in `values.yml`.
- Once this object is defined and is accepted, it can't be changed (as of now). The only allowable edit is for the `logLevel` field. For all other fields, if changed, the changes will not take any effect. For the changes to take effect, one has to restart the AMKO pod.

## Selecting kubernetes/openshift objects from different clusters
A CRD called GlobalDeploymentPolicy allows users to select kubernetes/openshift objects based on certain rules. This GDP object has to be created on the same system wherever the GSLBConfig object was created and `amko` is running. The selection policy applies to all the clusters which are mentioned in the GDP object. A typical GlobalDeploymentPolicy looks like this:

```yaml
apiVersion: "amko.vmware.com/v1alpha1"
kind: "GlobalDeploymentPolicy"
metadata:
  name: "global-gdp"
  namespace: "avi-system"   // a cluster-wide GDP
spec:
  matchRules:
    appSelector:
      label:
        app: gslb
    namespaceSelector:
      label:
        app: gslb
 
  matchClusters:
    - cluster: cluster1-admin    // cluster names are kubernetes cluster contexts
    - cluster: cluster2-admin
 
  trafficSplit:
    - cluster: cluster1
      weight: 8
    - cluster: cluster2
      weight: 2
```
1. `namespace`: an important piece here, as a GDP object created in `avi-system` namespace is recognised and all other GDP objects created in other namespaces are ignored.
2. `matchRules`: List of selection policy rules. If a user wants to select certain objects in a namespace (mentioned in `namespace`), they have to add those rules here. A typical `matchRule` looks like:
```yaml
matchRules:
    appSelector:                       // application selection criteria
      label:
        app: gslb                       // kubernetes/openshift label key-value
    namespaceSelector:                 // namespace selection criteria
      label:
        ns: gslb                        // kubernetes/openshift label key-value
```
A combination of appSelector and namespaceSelector will decide which objects will be selected for GSLB service consideration.
- appSelector: Selection criteria only for applications:
  * label: will be used to match the ingress/service type load balancer labels (key:value pair).
- namespaceSelector: Selection criteria only for namespaces:
  * label: will be used to match the namespace labels (key:value pair).

AMKO supports the following combinations for GDP matchRules:
| **appSelector** | **namespaceSelector** | **Result**                                                                                         |
| --------------- | --------------------- | -------------------------------------------------------------------------------------------------- |
| yes             | yes                   | Select all objects satisfying appSelector and from the namespaces satisfying the namespaceSelector |
| no              | yes                   | Select all objects from the selected namespaces (satisfying namespaceSelector)                     |
| yes             | no                    | Select all objects satisfying the appSelector criteria from all namespaces                         |
| no              | no                    | No objects selected (default action)                                                               |

Example Scenarios:

> Select objects with label `app:gslb` from all the namespaces:
```yaml
  matchRules:
    appSelector:
      label:
        app: gslb
```

> Select objects with label `app:gslb` and from namespaces labelled `ns:prod`:
```yaml
matchRules:
    appSelector:
      label:
        app: gslb
    namespaceSelector:
      label:
        ns: prod
```

3. `matchClusters`: List of clusters on which the above `matchRules` will be applied on. The member object of this list are cluster contexts of the individual k8s/openshift clusters.

4. `trafficSplit` is required if we want to route a certain percentage of traffic to certain objects in a certain cluster. These are weights and the range for them is 1 to 20.

**Few Notes**
- A GDP object must be created in the `avi-system` namespace. GDP objects in all ther namespaces will *not* be considered. For now, AMKO supports only one GDP object in the entire cluster. Any other additonal GDP objects will be ignored.
- A GDP object is created as part of `helm install`. User can then edit this GDP object to modify their selection of objects.
- GDP objects are editable. Changes made to a GDP object will be reflected on the AVI objects in the runtime, if applicable.
- Deletion of a GDP rule will trigger all the objects to be again checked against the remaining set of rules.
- Deletion of a cluster member from the `matchClusters` will trigger deletion of objects selected from that cluster in AVI.

## Supported Objects
AMKO supports selection of these kind of objects:
* Openshift Routes
* Kubernetes Ingresses
* Openshift/Kubernetes Service type Load Balancer

No other objects are supported.

## Multi-cluster kubeconfig
* The structure of a kubeconfig file looks like:
```yaml
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: <ca.crt data>
    server: https://10.10.10.10:6443
  name: cluster1
- cluster:
    certificate-authority-data: <ca.crt data>
    server: https://10.10.10.11:6443
  name: cluster2
contexts:
- context:
    cluster: cluster1
    namespace: default
    user: admin1
  name: cluster1-admin
- context:
    cluster: cluster2
    namespace: default
    user: admin2
  name: cluster2-admin
current-context: cluster2-admin
kind: Config
preferences: {}
users:
- name: admin1
  user:
    client-certificate-data: <client.crt>
    client-key-data: <client.key>
- name: admin2
  user:
    client-certificate-data: <client.crt>
    client-key-data: <client.key>
```
* The above example is for two clusters. Obtain the server addresses, ca.crts, client certs and client keys for the required clusters.
* The names `cluster1-admin` and `cluster2-admin` are the respective cluster contexts for both these clusters.

## Build and Test
Use:
```
make docker
```
to build and the image that will be generated will be named: `amko:latest`.

Use:
```
make test
```
to run the test cases.

### HA Cloud
HACloud - Federation of services across multiple kubernetes clusters which are typically within same region, without using DNS based load balancing. 
