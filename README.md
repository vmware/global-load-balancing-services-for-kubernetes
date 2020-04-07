# AMKO: Avi Multi Kubernetes Operator

## What's AMKO?
AMKO is a project that is used to provide multi cluster load balancing for applications - GSLB and HACloud features.

GSLB - Load balancing across instances of the application that have been deployed to multiple locations (typically, multiple data centers and/or public clouds). Avi uses the Domain Name System (DNS) for providing the optimal destination information to the user clients 

## Prerequisites
Few things are needed before we can kickstart AMKO:
1. Atleast one kubernetes/openshift cluster.
2. Atleast one AVI controller which manages the above kubernetes/openshift cluster. Additional controllers with openshift/kubernetes clusters can be added. 
3. Designate one controller (site) as the GSLB leader. Enable GSLB on the required controller and the other controllers (sites) as the follower nodes.
4. Designate one openshift/kubernetes cluster which will be communicating with the GSLB leader. All the configs for `amko` will be added to this cluster. We will call this cluster, `cluster-amko`.
5. Create a namespace `avi-system` in `cluster-amko`:
```
kubectl create ns avi-system
```
6. Create a kubeconfig file with the permissions to read the service and ingress/route objects for multiple clusters. See how to create a kubeconfig file for multiple clusters [here](#Multi-cluster-kubeconfig)
Generate a secret with the kubeconfig file in `cluster-amko`:
```
kubectl --kubeconfig my-config create secret generic gslb-config-secret --from-file gslb-members -n avi-system
```
**Note** that the permissions provided in the kubeconfig file above, for multiple clusters are important. They should contain permissions to at least `[get, list, watch]` on kubernetes services and ingresses (routes for openshift).

## Install using helm
The next step is to use helm to bootstrap amko:
```
helm install amko --generate-name --namespace=avi-system --set configs.gslbLeaderHost="10.10.10.10" 
```
Use the [values.yaml](helm/amko/values.yaml) to edit values related to Avi configuration. Please refer to the [parameters](#parameters).

## Uninstall using helm
Since we provided a dynamic name with `helm install`, we have to first find out the name of the helm installation:
```
helm list -n avi-system    // note the name of amko instance
helm uninstall -n avi-system <amko_instance>
```
## parameters
| **Parameter**                | **Description**         | **Default**                      |
|-----------------------------|------------------------|------------------------------------|
|`configs.gslbLeaderVersion`  | GSLB leader controller version | 18.2.7                     |
|`configs.gslbLeaderSecret`   | GSLB leader credentials secret name in `avi-system` namespace |  `avi-secret` |
|`configs.gslbLeaderCredentials.username` | GSLB leader controller username | `admin` |
|`configs.gslbLeaderCredentials.password` | GSLB leader controller password | `avi123`|
|`configs.memberClusters.clusterContext` | K8s member cluster context for GSLB | `cluster1-admin` and `cluster2-admin` |
|`configs.refreshInterval` | The time interval which triggers a AVI cache refresh | 120 seconds |
|`configs.clusterMembers.secret` | The name of the secret which is created with kubeconfig as the data | `gslb-config-secret`|
|`configs.clusterMembers.key` | The name of the field (key) inside the secret `configs.clusterMembers.secret` data | `gslb-members`|

## GSLB configuration to kubernetes cluster
A CRD has been provided to add the GSLB configuration. The name of the object is GSLBConfig and it has the following parameters:
```
apiVersion: "avilb.k8s.io/v1alpha1"
kind: "GSLBConfig"
metadata:
  name: "gslb-policy-1"
  namespace: "avi-system"
spec:
  gslbLeader:
    credentials: gslb-avi-secret
    controllerVersion: 18.2.7
    controllerIP: 10.79.171.1
  memberClusters:
    - clusterContext: cluster1-admin
    - clusterContext: cluster2-admin
  globalServiceNameSource: HOSTNAME
  domainNames:
    - avi-container-dns.internal
  refreshInterval: 60
```
1. `apiVersion`: The api version for this object has to be `avilb.k8s.io/v1alpha1`.
2. `kind`: the object kind is `GSLBConfig`.
3. `name`: Can be anything, but it has to be specified in the GDP object.
4. `namespace`: By default, this object is supposed to be created in `avi-system`.
5. `gslbLeader.credentials`: A secret object has to be created for (`helm install` does that automatically) the GSLB Leader cluster. The username and password have to be provided as part of this secret object. Refer to `username` and `password` in [parameters](#parameters).
6. `gslbLeader.controllerVersion`: The version of the GSLB leader cluster.
7. `gslbLeader.controllerIP`: The GSLB leader IP address or the hostname along with the port number, if any.
8. `memberClusters`: The kubernetes/openshift cluster contexts which are part of this GSLB cluster. See [here](#Multi-cluster kubeconfig) to create contexts for multiple kubernetes clusters.
9. `globalServiceNameSource`: The basis on which a GSLB service is created and named. For now, the supported type is `HOSTNAME`. This means that the any ingress which shares a host name across clusters will be placed on the same GSLB service.
10. `domainNames`: Supported GSLB subdomains.
11. `refreshInterval`: This is an internal cache refresh time interval, on which syncs up with the AVI objects and checks if a sync is required.

**Few Notes**:
- Only one GSLBConfig object is allowed.
- If using `helm install`, the GSLB config object will be created for you, just provide the right parameters in `values.yml`.
- Once this object is defined and is accepted, it can't be changed (as of now). If changed, the changes will not take any effect.
- The name for the GSLBConfig object is also mentioned in the GDP object.

## Selecting kubernetes/openshift objects from different clusters
A CRD called GlobalDeploymentPolicy allows users to select kubernetes/openshift objects based on certain rules. This GDP object has to be created on the same system wherever the GSLBConfig object was created and `amko` is running. The selection policy applies to all the clusters which are mentioned in the GDP object. A typical GlobalDeploymentPolicy looks like this:
```
apiVersion: "avilb.k8s.io/v1alpha1"
kind: "GlobalDeploymentPolicy"
metadata:
  name: "green-gdp"
  namespace: "green"
spec:
  matchRules:
    - object: INGRESS
      label:
        key: "app"
        value: "gslb"
      op: EQUALS
    - object: LBSVC
      label:
        key: "app"
        value: "gslb"
      op: EQUALS
  matchClusters:
    - clusterContext: cluster1-admin
    - clusterContext: cluster2-admin
 
  gslbConfig: "gslb-policy-1"
  trafficSplit:
    - cluster: cluster1-admin
      weight: 8
    - cluster: cluster2-admin
      weight: 2
  gslbAlgorithm: "RoundRobin"
```
1. `namespace`: an important piece here, as a GDP object created in this namespace will only apply to the objects in this namespace across all the member clusters.
2. `matchRules`: List of selection policy rules. If a user wants to select certain objects in a namespace (mentioned in `namespace`), they have to add those rules here. A typical `matchRule` looks like:
```
object: K8S/openshift object     // LBSVC, INGRESS, ROUTE
label:
    key: <label key>             // typical k8s/openshift label key
    value: <label value>         // typical k8s/openshift label value
op: <operator>                   // EQUALS, NOTEQUALS
```
With each rule, we specify the following:
- object type, this specifies which k8s/openshift object we want to select. Support types are:
  * Service of type Load balancer (LBSVC)
  * Kubernetes Ingresses (INGRESS)
  * Openshift Routes (ROUTE)
 - label, specify what labels we want to select. Only the objects specified in `object` with matching `key/value` pair will be selected.
 - op, specify the operation. For labels, it is either EQUALS/NOTEQUALS.
 
 The `matchRules` are a set of AND (&&) based rules. For same objects, if different labels let's say, `app:gslb` and `app:test` is specified in the `matchRules`, we select those objects which have both of these labels.

3. `matchClusters`: List of clusters on which the above `matchRules` will be applied on. The member object of this list are cluster contexts of the individual k8s/openshift clusters.
4. `gslbConfig` is the name of the GSLBConfig object created in the `avi-system` namespace.
5. `trafficSplit` is required if we want to route a certain percentage of traffic to certain objects in a certain cluster. These are weights and the range for them is 1 to 20.

**Few Notes**
- GDP objects are per-namespace. This means that a user can create only one GDP object per-namespace.
- No GDP objects are created as part of `helm install`. User has to create a GDP object to start selecting objects for GSLB'ing.
- GDP objects are editable. Changes made to a GDP object will be reflected on the AVI objects, if applicable.
- Deletion of a GDP rule will trigger all the objects to be again checked against the remaining set of rules.
- Deletion of a cluster member from the `matchClusters` will trigger deletion of objects selected from that cluster in AVI.

## Multi-cluster kubeconfig
* The structure of a kubeconfig file looks like:
```
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
