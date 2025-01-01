# Tenancy support in AMKO

* This feature allows AMKO to create GSLB objects in user-specified tenants in Avi. The expected isolation and administrative restrictions of a multi-tenant architecture in NSX Advanced Load Balancer GSLB extend to AMKO.

* AMKO will determine the tenant to create GSLB objects from `ako.vmware.com/tenant-name` annotation value specified in the namespace of Kubernetes/openshift objects.

* If `ako.vmware.com/tenant-name` annotation is empty or missing AMKO will determine tenant from `gslbLeader.tenant` field of [GSLBConfig](crds/gslbconfig.md#gslbconfig-for-amko) CRD.

* The `ako.vmware.com/tenant-name` annotation must be same across corresponding namespaces of Kubernetes/openshift objects in the member clusters.

* All references to AVI objects in GDP and GSLBHostRule CRD should be accessible in the tenant associated with the namespace by the AMKO User.If they are not accesible CRD would transition to error status and won't be applied to GSLB service.

**Note:** In case of tenant update in namespace for already created GSLB objects AMKO will create GSLB objects in new tenant only after tenant is updated in namespaces across all member clusters.


## Example with GSLB services in multiple tenants in AMKO

In this example AMKO will create GSLB Services in `tenant1` and `tenant2` tenant for Kubernetes/openshift objects in `n1` and `n2` namespace respectively. For namespace which are missing the annotation GSLB service will be created in the tenant where AMKO is installed.

* Edit namespace in all member clusters to add the `ako.vmware.com/tenant-name` annotation
```
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    ako.vmware.com/tenant-name: tenant1
  name: n1
  ---
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    ako.vmware.com/tenant-name: tenant2
  labels:
    kubernetes.io/metadata.name: avi-system
  name: n2
  ```
  * This will enable all the resources in a namespace to use the annotated tenant.With above configuration AMKO and AKO will create the corresponding avi-objects as per below table:

  | **Namespace** | **Tenant** |
| --------- | ----------- |
| `n1` | `tenant1` |
| `n2` | `tenant2` |
| other | default AMKO tenant |


