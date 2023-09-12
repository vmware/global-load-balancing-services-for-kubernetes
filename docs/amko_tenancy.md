# Tenancy support in AMKO

This feature allows AMKO to create GSLB object in a user-specific tenant in Avi.The expected isolation and administrative restrictions of a multi-tenant architecture in NSX Advanced Load Balancer GSLB extend to AMKO

## Tenant Context

AVI non admin tenants primarily operate in 2 modes, **provider context** and **tenant context**.

### Provider Context

Service Engine Groups are shared with `admin` tenant. All the other objects like Virtual Services and Pools are created within the tenant. Requires `config_settings.se_in_provider_context` flag to be set to `True` when creating tenant. 

### Tenant Context

Service Engines are isolated from `admin` tenant. A new `Default-Group` is created within the tenant. All the objects including Service Engines are created in tenant context. Requires `config_settings.se_in_provider_context` flag to be set to `False` when creating tenant. 

## Steps to enable Tenancy in AMKO

In this example we will run AMKO in `billing` tenant.

### 1. Install AKO in required Tenant.
* Follow the steps [here](https://avinetworks.com/docs/ako/1.10/ako-tenancy/) to run AKO in a specific Tenant `billing`.

**Note**  If AKO in all sites are not running in same tenant make sure to [configure tenancy scope on AVI controller](https://docs.vmware.com/en/VMware-NSX-Advanced-Load-Balancer/30.1/GSLB-Guide/GUID-3EEBA58D-6FFA-48D8-BD0B-A7392085F289.html?hWord=N4IghgNiBcIC4FMB2YlwPoGcDGB7ADgiAL5A). 
### 2. Add required permissions to AMKO user.
* AMKO User need to have below permissions in order to ceate GSLB service :

| **Permission** | **AccessRight** |
| --------- | ----------- |
| `GSLB configuration` | Read access to everything in the GSLB configuration relevant to the tenant |
| `GSLB services` | Write access to all GSLB services in all tenants to which this user is assigned |
| `GSLB geolocation database` | Read access to geolocation database |
* To achieve this AMKO User can be assigned [`ako-tenant`](roles/ako-tenant.json) role in the `billing` tenant.
### 3. AMKO installation

* In **AMKO**, Set the `configs.tenant` to the tenant `billing` created in the earlier steps.


