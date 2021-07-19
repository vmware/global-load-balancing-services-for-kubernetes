## Creating a kubeconfig file for multi cluster access
To access a kubernetes/openshift cluster via a kubeconfig file, we need a cluster context to be defined. Each cluster context is a combination of:
* cluster
* namespace
* user

To check the existing config:
```
kubectl config view
```

Each cluster has a set of cluster contexts already defined in the default kubeconfig and it can be viewed using:
```
kubectl config get-contexts

CURRENT   NAME                          CLUSTER      AUTHINFO           NAMESPACE
*         kubernetes-admin@kubernetes   kubernetes   kubernetes-admin   
```

The cluster context defines the access parameters for a user on how they can access a cluster. Follow the next section to understand a kubeconfig file.

Alternatively, if you already understand how a kubeconfig file works, jump to [this section](#complete-kubeconfig) to create a multi-cluster config.

## Anatomy of a kubeconfig file
To view the existing kubeconfig for a cluster, either check the $HOME/.kube/config file or do this:
```
kubectl config view
```
Here's how a kubeconfig file looks like:

```yaml
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: <ca.cert>
    server: https://10.10.10.10:6443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: <client.cert>
    client-key-data: <client.key>
```

It has 3 sections:
* clusters: This section contains a list of kubernetes/openshift clusters.
  * cluster.certificate-authority-data: CA cert of the server
  * cluster.server: URL of the kubernetes/openshift API server
  * name: name of this cluster that it should be identified with.
* users: This section is where we define a list of users and their credentials. The user credentials determine what resources and verbs a user has access to in a given cluster.
  * user.client-certificate-data: client certificate for the user.
  * user.client-key-data: client key for the user.
  * name: name of this user that it should be identified with.
* contexts: This is where we define the cluster accesses by associating a cluster, a user and a namespace. This cluster context is then used to access the resources of a kubernetes/openshift cluster.
  * context.cluster: Name of the cluster as defined in the `clusters` section.
  * context.user: Name of the user as defined in the `users` section.
  * name: name of this context that it should be identified with.

### Choosing a cluster context
A kubernetes/openshift client has to run within a cluster context. `kubectl` runs with a pre-defined and a default set of kubeconfig contexts. To see the list of available contexts with `kubectl`, use this:
```
kubectl config get-contexts

CURRENT   NAME                          CLUSTER      AUTHINFO           NAMESPACE
*         kubernetes-admin@kubernetes   kubernetes   kubernetes-admin   
          user@kubernetes               kubernetes   kubernetes-user
```

To use a context out of this list, just use:
```
kubectl config use-context user@kubernetes
```

## Creating a multi cluster kubeconfig file
So, with the understanding of the cluster contexts, let's now see, for two clusters `cluster1` and `cluster2`, how we can create cluster contexts for both, to access the required resources in a single kubeconfig file. The auth information can be obtained from the respective clusters' kubeconfigs.

#### clusters
In the `clusters` section, we define two clusters: `cluster1` and `cluster2`. For each of them, we provide the API server's URL, their ca.certs and their names. The names are user-defined.
```yaml
clusters:
- cluster:
    certificate-authority-data: <cluster1-ca.cert>
    server: https://10.10.10.10:6443
  name: cluster1
- cluster:
    certificate-authority-data: <cluster2-ca.cert>
    server: https://10.10.10.11:6443
  name: cluster2
```

#### users
In the `users` section, we define two users, one for each cluster: `c1-admin` is for `cluster1`. Again, the name `c1-admin` is user-defined and can be anything (just has to be unique in the `users` section). We also obtain the `client.cert` and `client.key` data for this user from cluster 1's environment. Similarly, we also define another user which is for `cluster2`. We name this auth information as `c2-admin`.
```yaml
users:
- name: c1-admin
  user:
    client-certificate-data: <client.cert>
    client-key-data: <client.key>
- name: c2-admin
  user:
    client-certificate-data: <client.cert>
    client-key-data: <client.key>
```

#### contexts
After defining the clusters and the users, its now time to define the contexts. We associate the `cluster1` cluster with user `c1-admin` and name this context as `cluster1-admin`. We also associate `cluster2` cluster with the user `c2-admin` and name this context as `cluster2-admin`.
```yaml
contexts:
- context:
    cluster: cluster1
    user: c1-admin
  name: cluster1-admin
- context:
    cluster: cluster2
    user: c2-admin
  name: cluster2-admin
```

And, we are done! Let's combine all of the above into a single config file.

#### complete kubeconfig
```yaml
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: <cluster1-ca.cert>
    server: https://10.10.10.10:6443
  name: cluster1
- cluster:
    certificate-authority-data: <cluster2-ca.cert>
    server: https://10.10.10.11:6443
  name: cluster2

contexts:
- context:
    cluster: cluster1
    user: c1-admin
  name: cluster1-admin
- context:
    cluster: cluster2
    user: c2-admin
  name: cluster2-admin

kind: Config
preferences: {}

users:
- name: c1-admin
  user:
    client-certificate-data: <client.cert>
    client-key-data: <client.key>
- name: c2-admin
  user:
    client-certificate-data: <client.cert>
    client-key-data: <client.key>
```
Let's save this file as `gslb-members`.

To verify that contexts are fine, we can use this with the `kubectl` client from anywhere (as long as both the API servers are reachable):
```
kubectl --kubeconfig gslb-members config get-contexts

CURRENT   NAME             CLUSTER    AUTHINFO     NAMESPACE
          cluster1-admin   cluster1   c1-admin     default
          cluster2-admin   cluster2   c2-admin     default
```

This file can then be used to create a secret to be used by the AMKO pod.