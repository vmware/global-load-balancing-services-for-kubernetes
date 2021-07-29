# AMKO: Avi Multicluster Kubernetes Operator

### Run AMKO

AMKO is a kubernetes operator used for multi-cluster application load balancing for Kubernetes workloads.

 ![Alt text](images/amko_ss.png?raw=true "amko architecture")

AMKO is aware of the following object types:
- Kubernetes
  * Ingress
  * Service type load balancer

- Openshift
  * Routes
  * Service type load balancer

#### Dependencies
For Kubernetes clusters:
| **Components** | **Version** |
| -------------- | ----------- |
| Kubernetes     | 1.16+       |
| AKO            | 1.4.3       |
| AVI Controller | 20.1.4-2p3+ |

For openshift clusters:
| **Components** | **Version** |
| -------------- | ----------- |
| Openshift      | 4.4+        |
| AKO            | 1.4.3       |
| AVI Controller | 20.1.4-2p3+ |

#### Pre-requisites
To kick-start AMKO, we need:
1. Atleast one kubernetes/openshift cluster.
2. Atleast one AVI controller.
3. AMKO assumes that it has connectivity to all the member clusters' kubernetes API servers. Without this, AMKO won't be able to watch over the kubernetes and openshift resources in the member clusters.
4. Before deploying AMKO, one of the clusters have to be designated as the leader and rest of the clusters as followers. AMKO has to be deployed on all clusters (wherever federation is required). This is to ensure that the leader cluster's AMKO would federate the AMKO config objects like `GSLBConfig` and `GlobalDeploymentPolicy` objects to all follower clusters. See [this](federation.md) for more details on the specifics of federation and how to recover from a disaster recovery scenario.
5. On all clusters, create a namespace `avi-system`:
   ```
   $ kubectl create ns avi-system
   ```

6. Create a kubeconfig file with the permissions to read the service and the ingress/route objects for all the member clusters. Follow [this tutorial](kubeconfig.md) to create a kubeconfig file with multi-cluster access. Name this file `gslb-members` and generate a secret with the kubeconfig file in `cluster-amko` by following:
   ```
   $ kubectl create secret generic gslb-config-secret --from-file gslb-members -n avi-system
   ```
This has to be done for all the member clusters wherever AMKO is going to be deployed.

*Note* that the permissions provided in the kubeconfig file for all the clusters must have atleast the permissions to `[get, list, watch]` on:
   * Kubernetes ingress and service type load balancers.
   * Openshift routes and service type load balancers.
AMKO also needs permissisons to `[create, get, list, watch, update, delete]` on:
   * GSLBConfig object
   * GlobalDeploymentPolicy object
#### Install using helm
*Note* that only helm v3 is supported.

Following steps have to be executed on all member clusters:

1. Create the `avi-system` namespace:
   ```
   $ kubectl create ns avi-system
   ```

2. Add this repository to your helm client:
   ```
   $ helm repo add amko https://projects.registry.vmware.com/chartrepo/ako

   ```
   Note: The helm charts are present in VMWare's public harbor repository

3. Search the available charts for AMKO:
   ```
   $ helm search repo

   NAME     	CHART VERSION    	APP VERSION      	DESCRIPTION
   ako/amko	1.4.2	            1.4.2	            A helm chart for Avi Multicluster Kubernetes Operator

   ```

4. Use the `values.yaml` from this repository to provide values related to Avi configuration. To get the values.yaml for a release, run the following command

   ```
   helm show values ako/amko --version 1.4.2 > values.yaml

   ```
   Values and their corresponding index can be found [here](#parameters)

5. To configure federation via `values.yaml`:
   * Set `configs.federation.currentClusterIsLeader` to `true` for the leader cluster. For all follower clusters, set this to `false`.
   * Set `configs.federation.currentCluster` to the current cluster context.
   * Add the member clusters to `configs.federation.memberClusters`.

6. Install AMKO:
   ```
   $ helm install  ako/amko  --generate-name --version 1.4.2 -f /path/to/values.yaml  --set configs.gsllbLeaderController=<leader_controller_ip> --namespace=avi-system
   ```
7. Check the installation:
   ```
   $ helm list -n avi-system

   NAME           	NAMESPACE 	REVISION	UPDATED                                	STATUS  	CHART                 	APP VERSION
   amko-1598451370	avi-system	1       	2020-08-26 14:16:21.889538175 +0000 UTC	deployed	amko-1.4.2	            1.4.2
   ```

#### Troubleshooting and Log collection
If you face any issues during installing/configuring/using AMKO, see if your problem is listed in the troubleshooting [page](docs/troubleshooting.md).

Follow [this](docs/troubleshooting.md#how-do-i-gather-the-amko-logs) to gather logs for tech-support in case of an unrecoverable failure.

#### Uninstall using helm
```
helm uninstall -n avi-system <amko-release-name>
```
If a user needs to remove the already created GSLB services, one has to remove the GDP object first. This will remove all the GSLB services selected via the GDP object.
```
kubectl delete gdp -n avi-system global-gdp
```
Also, delete the `avi-system` namespace:
```
kubectl delete ns avi-system
```

#### parameters
| **Parameter**                                    | **Description**                                                                                                          | **Default**                           |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------- |
| `configs.controllerVersion`                      | GSLB leader controller version                                                                                           | 20.1.4                                |
| `configs.federation.image.repository`            | Image repository for AMKO federator                                                                                         | projects.registry.vmware.com/ako/amko-federator|
| `configs.federation.image.pullPolicy`            | Image pull policy for AMKO federator                                                                                         | IfNotPresent|
| `configs.federation.currentCluster`            | Current cluster context (required)                                                                                         | Nil |
| `configs.federation.currentClusterIsLeader`            | Set to `true` if current cluster is the leader (required)                                                                                         | false |
| `configs.federation.memberClusters`            | member clusters on which federation should be done                                                                                    |  |
| `configs.gslbLeaderController`                         | GSLB leader site URL                                                                                                     | Nil                                   |
| `gslbLeaderCredentials.username`         | GSLB leader controller username                                                                                          | `admin`                               |
| `gslbLeaderCredentials.password`         | GSLB leader controller password                                                                                          |                               |
| `configs.memberClusters.clusterContext`          | K8s member cluster context for GSLB                                                                                      | `cluster1-admin` and `cluster2-admin` |
| `configs.refreshInterval`                        | The time interval which triggers a AVI cache refresh                                                                     | 1800 seconds                           |
| `configs.logLevel`                         | Log level to be used by AMKO to print the type of logs, supported values are `INFO`, `DEBUG`, `WARN` and `ERROR` | `INFO`                                   |
| `configs.useCustomGlobalFqdn`                         | Select the GslbService FQDN mode for AMKO. If set to `true`, AMKO observes the HostRules to look for mapping between local and global FQDNs | `false`                                   |
| `gdpConfig.appSelector.label{.key,.value}`       | Selection criteria for applications, label key and value are provided                                                    | Nil                                   |
| `gdpConfig.namespaceSelector.label{.key,.value}` | Selection criteria for namespaces, label key and value are provided                                                      | Nil                                   |
| `gdpConfig.matchClusters`                        | List of clusters (names must match the names in configs.memberClusters) from where the objects will be selected          | Nil                                   |
| `gdpConfig.trafficSplit`                         | List of weights for clusters (names must match the names in configs.memberClusters), each weight must range from 1 to 20 | Nil                                   |
| `gdpConfig.ttl`                         | Time To Live, ranges from 1-86400 seconds | Nil                                   |
| `gdpConfig.healthMonitorRefs`                         | List of health monitor references to be applied on all Gslb Services | Nil                                   |
| `gdpConfig.sitePersistenceRef`                         | Reference for a federated application persistence profile created on the Avi Controller | Nil                                   |
| `gdpConfig.poolAlgorithmSettings`   | Pool algorithm settings to be used by the GslbServices for traffic distribution across pool members. See [pool algorithm settings](crds/gslbhostrule.md#pool-algorithm-settings) to configure the appropriate settings. |          GSLB_ALGORITHM_ROUND_ROBIN         |


#### Custom resources
AMKO uses a custom resource to configure federation to member clusters:
- [AMKOCluster](federation.md#amkocluster-crd-to-control-federation)

AMKO uses the following custom resources to configure the GSLB services in the GSLB leader site:
1. [GSLBConfig](crds/gslbconfig.md)
2. [GlobalDeploymentPolicy](crds/gdp.md)

Follow the above links to take a look at the CRD objects and how to use them.

If AMKO is installed via `helm`, it by default creates one instance of each type in the `avi-system` namespace. To see these objects:
```
$ kubectl get amkocluster amkocluster-federation -n avi-system
NAME                       AGE
amkocluster-federation     45m

$ kubectl get gc -n avi-system gc-1
NAME            AGE
gc-1            45m

$ kubectl get gdp -n avi-system
NAME         AGE
global-gdp   46m
```

**Note** that, only one instance of each `GDP` and `GSLBConfig` is supported and AMKO *will* ignore other instances.

3. [GSLBHostRule](crds/gslbhostrule.md): Override specific GslbService properties. No instances are created by default (helm install). Users have to create these in the `avi-system` namespace. To see these objects:
```
$ kubectl get gslbhostrule -n avi-system
```

#### Editing runtime parameters of AMKO
The `GDP` object can be edited at runtime to change the application selection parameters, traffic split and the applicable clusters. AMKO will recognize these changes and will update the GSLBServices accordingly.

Changing only `logLevel` is permissible at runtime via an edit of the `GSLBConfig`. For all other changes to the `GSLBConfig`, the AMKO pod has to be restarted.

#### Choosing a mode of GslbService FQDN
There can be different requirements for a user to either use local FQDNs as the GslbService FQDNs, or use a different FQDN as the Global FQDN. Please see [this](docs/local_and_global_fqdn.md) to choose a mode fit for the use-case and enable accordingly.

#### Gslb Service Properties
Certain Gslb Service properties can be set and modified at runtime. If these are set through the GDP object, they are applied to all the Gslb Services. If a user wants to override specific properties, they can use a `GSLBHostRule` object for a GslbService.

| **Properties** | **Configured via** |
| -------------- | ---------------- |
| TTL      | `GDP`, `GSLBHostRule`        |
| Site Persistence            |  `GDP`, `GSLBHostRule`     |
| Custom Health Monitors | `GDP`, `GSLBHostRule`      |
| Third party members | `GSLBHostRule`      |
| Traffic Split| `GDP`, `GSLBHostRule`      |
| Pool Algorithm Settings | `GDP`, `GSLBHostRule`|

To set them, follow steps for [GlobalDeploymentPolicy](crds/gdp.md) and for [GSLBHostRule](crds/gslbhostrule.md).