# Recommended cluster configuration for AMKO

There can be a couple of deployment patterns for AMKO:
1. AMKO in a config cluster
2. AMKO in any one of the member clusters

## Deployment Configuration where AMKO is deployed on a config cluster

Ideally, AMKO should be deployed in its own cluster, which would be known as the `config-cluster`. This `config-cluster` can be a minimalistic kubernetes cluster. This cluster should also be Highly Available. See this [documentation](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/high-availability/) on creating highly available clusters for kubernetes. This is to ensure that the AMKO configurations are not lost in case of a cluster failure scenario.

 ![Alt text](../images/amko_config.png?raw=true "amko deployment configuration 1")

AMKO can be installed via `helm` on the `config-cluster` which would install the related config objects like:
- A `GSLBConfig` object
- A `GDP` object

Any additional configuration objects like the `GSLBHostRule` objects have to be created on this `config-cluster`.

**Note** that the `config-cluster` must have connectivity to the kubernetes API server of all the member clusters (as depicted above in site 1, site 2 and site 3).

Alternatively, AMKO can also be deployed in any of the member clusters.

## Deployment configuration where AMKO is deployed in a member cluster
AMKO can also be deployed on any one of the member clusters, as long as this cluster has connectivity to all the other member clusters' kubernetes API servers.

 ![Alt text](../images/amko_ako.jpg?raw=true "amko deployment configuration 2")

To avoid disruption in case of a site failure where AMKO was deployed, it is recommended that the cluster where AMKO is running has a DR solution pre-configured. This can be achieved by using existing tools like [Velero](https://velero.io/docs/v1.6/). These tools can be scheduled to take regular snapshots of the current cluster state for specific namespaces. These snapshots can be later restored into a different cluster. `Velero` is just an example here and any alternative `etcd` backup tooling can be used to backup the AMKO config objects.

It is recommended to take backups of the following configuration objects for AMKO:
1. `GSLBConfig`
2. `GDP`
3. `GSLBHostRule`

The `GSLBConfig` object `gc-1` and `GDP` object `global-gdp` can be found in the `avi-system` namespace. `GSLBHostRule` objects can be created in any namespace and don't have a namespace limitation.

## Recover a failed instance of AMKO

There can be scenarios where a cluster which is running the AMKO pod fails, or the user loses control over the cluster due to some network disruption.
It is always recommended to take backups of the following configuration objects for AMKO at regular intervals:
1. `GSLBConfig`
2. `GDP`
3. `GSLBHostRule`

To get a list of all such objects and store them into files, use the following commands:
```
kubectl get gslbconfig -n avi-system -o yaml > gc.yaml
kubectl get gdp -n avi-system -o yaml > gdp.yaml
kubectl get gslbhostrule --all-namespaces -o yaml > gslbhostrule.yaml
```
These files can then be used to restore the configuration state of AMKO in any of the other clusters.

**Note** that AMKO needs only the above configuration for recovery scenarios.

### Deployment Scenario
Now, let's consider a scenario where there are 3 sites, and each site has some clusters:
1. Site 1:
    - clusterA (AMKO is currently hosted here)
    - clusterB
2. Site 2:
    - clusterC
3. Site 3:
    - clusterD

There can be two types of failures where AMKO is running:
1. `Site failure`: Entire site `Site 1` fails.
2. `Cluster failure`: Cluster `clusterA` fails inside `Site 1`.

#### Step 1: Switch to a different cluster
Select a new cluster from the [Deployment Scenario](#Deployment-Scenario). In the event of `Site failure`, the user can choose a cluster from `Site 2` or `Site 3`. In the event of `Cluster failure`, the user can choose `clusterB` as the new cluster to deploy AMKO. For this example, let's consider that `clusterD` in `site 3` was chosen.

#### Step 2: Install AMKO on the new cluster
While installing the AMKO instance via helm, keep the `gslbLeaderController` field **empty** in the `values.yaml` file. This field will get updated later. Use the steps [here](../README.md#Install-using-helm) to then install AMKO.

**Note** that to avoid any conflicting scenarios (multiple AMKOs running together with same the configuration), ensure that only instance of AMKO is running with the same configuration.

#### Step 3: Restore the AMKO configs to the new cluster
Once AMKO is installed, the user can then restore the backed up configuration `GSLBConfig`, `GDP` and `GSLBHostRule` objects on this cluster `clusterD`:
```
kubectl apply -f gc.yaml
kubectl apply -f gdp.yaml
kubectl apply -f gslbhostrule.yaml
```

#### Step 4: Update configuration
Since we are using the existing objects as is, AMKO may not restart if the member clusters are in failed state. In that case, remove the member clusters which are in failed state, from:
1. `GSLBConfig` object `gc-1`
2. `GDP` object `global-gdp`

So, if `site 1` failed, remove `clusterA` and `clusterB` from the above objects. If just `clusterA` failed, remove only `clusterA` from the above objects.

Once the above objects are updated, AMKO should then be restarted:
```
kubectl delete pod -n avi-system amko-0
```

**Note** that the user can perform Step 2 before-hand on a certain set of chosen clusters as standby clusters. In case of failures, the config objects for AMKO can then be restored on one of these standy clusters, updated and AMKO can be restarted.