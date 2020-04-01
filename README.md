# AMKO: Avi Multi Kubernetes Operator

## What's AMKO?
AMKO is a project that is used to provide multi cluster load balancing for applications - GSLB and HACloud features.

GSLB - Load balancing across instances of the application that have been deployed to multiple locations (typically, multiple data centers and/or public clouds). Avi uses the Domain Name System (DNS) for providing the optimal destination information to the user clients 

## Prerequisites
Few things are needed before we can kickstart AMKO:
1. Atleast one kubernetes/openshift cluster.
2. Atleast one AVI controller which manages the above kubernetes/openshift cluster. Additional controllers with openshift/kubernetes clusters can be added. 
3. Designate one controller (site) as the GSLB leader. Enable GSLB on the required controller and the other controllers (sites) as the follower nodes.
4. Designate one openshift/kubernetes cluster which will be talking to the GSLB leader. All the configs for `amko` will be added to this cluster. We will call this cluster, `cluster-amko`.
5. Create a namespace `avi-system` in `cluster-amko`:
```
kubectl create ns avi-system
```
6. Create a kubeconfig file with the permissions to read the service and ingress/route objects for multiple clusters. See how to create a kubeconfig file for multiple clusters [here](#Multi-cluster-kubeconfig)
Generate a secret with the kubeconfig file in `cluster-amko`:
```
kubectl --kubeconfig my-config create secret generic gslb-config-secret --from-file gslb-members -n avi-system
```

## Install using helm
The next step is to use helm to bootstrap amko:
```
helm install amko --generate-name --namespace=avi-system --set configs.gslbLeaderHost="10.10.10.10" 
```
Use the [values.yaml](helm/amko/values.yaml) to edit values related to Avi configuration. Please refer to the [parameters](#parameters).

## Uninstall using helm

## parameters
| **Parameter**                | **Description**         | **Default**                      |
|-----------------------------|------------------------|------------------------------------|
|`configs.gslbLeaderVersion`  | GSLB leader controller version | 18.2.7                     |
|`configs.gslbLeaderSecret`   | GSLB leader credentials secret name in `avi-system` namespace |  `avi-secret` |
|`configs.gslbLeaderCredentials.username` | GSLB leader controller username | `admin` |
|`configs.gslbLeaderCredentials.password` | GSLB leader controller password | `avi123`|

## Multi-cluster kubeconfig
* The basic structure of a kubeconfig file looks like:
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

### HA Cloud
HACloud - Federation of services across multiple kubernetes clusters which are typically within same region, without using DNS based load balancing. 
