# Tenancy support in AMKO

This feature allows AMKO to create GSLB object in a user-specified tenant in Avi.The expected isolation and administrative restrictions of a multi-tenant architecture in NSX Advanced Load Balancer GSLB extend to AMKO
## Steps to enable Tenancy in AMKO

In this example we will run AMKO in `billing` tenant.

### 1. Install AKO in required Tenant.
* Follow the steps [here](https://avinetworks.com/docs/ako/1.10/ako-tenancy/) to run AKO in a specific Tenant `billing`.

**Note:**  AKO in all sites should be running in same tenant as AMKO.
### 2. Add required permissions to AMKO user.
* AMKO User need to have below permissions in order to ceate GSLB service :

| **Permission** | **AccessRight** |
| --------- | ----------- |
| `GSLB configuration` | Read access to everything in the GSLB configuration relevant to the tenant |
| `GSLB services` | Write access to all GSLB services in all tenants to which this user is assigned |
| `GSLB geolocation database` | Read access to geolocation database |
* To achieve this AMKO User can be assigned [`amko-tenant`](roles/amko-tenant.json) role in the `billing` tenant.
### 3. AMKO installation

* In **AMKO**, Set the `configs.tenant` field in values.yaml  to the tenant `billing` created in the earlier steps.


