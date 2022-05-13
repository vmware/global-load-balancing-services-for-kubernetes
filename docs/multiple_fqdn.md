# Multiple FQDN Support in AMKO using HostRules

AKO provides a CRD called [hostrule](https://github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/blob/master/docs/crds/hostrule.md) for virtual host properties. These hostrule objects can be utilised by AMKO for mutliple FQDN support and [local to global fqdn mapping](https://github.com/vmware/global-load-balancing-services-for-kubernetes/blob/master/docs/local_and_global_fqdn.md) in AMKO. <br>
Hostrules can be configured with `aliases` so that a specific application can have the ability to have multiple FQDNs configured under a specific route/ingress for the child VS, instead of having to create the route multiple times.

AMKO can reduce the number of GSLB Services by grouping related(FQDN and its aliases) FQDNs together into one GSLB Service. 

The functionality of AMKO changes based on how the hostrule is configured.

1. If AMKO is configured normally i.e., `useCustomGlobalFqdn: false` in gslb config, AMKO creates a GSLB Service for every distinct ingress/route hostname.
If there are hostrules with aliases existing for any fqdn, AMKO will add these aliases as a part of domain names in the GSLB Services
A sample hostrule object would look like this: 

```
spec:
  virtualhost:
    aliases:
    - alias1.com
    - alias2.com
    fqdn: foo.region1.com    #fqdn/hostname 
    fqdnType: Exact 
```

Aliases provided must be unique in and across clusters. 
If aliases are repeated in a cluster, AKO will reject the hostrule.
If aliases are repeated across clusters, AMKO will ignore such an alias. 


2. If AMKO is configured to use custom global fqdn i.e., `useCustomGlobalFqdn: true` in gslb config object, a GSLB Service is created for a fqdn, only if a corresponding hostrule exists. [Refer this](https://github.com/vmware/global-load-balancing-services-for-kubernetes/blob/master/docs/local_and_global_fqdn.md)

In such a case there can be 2 choices to the user. <br>
*  A user can choose to include the aliases mentioned in the hostrule as domain names of the GSLB Service. <br>
* A user can choose to ignore the aliases - default action taken by AMKO. 

A flag called `includeAliases` under `gslb` section of the hostrule lets the user decide. 
A sample hostrule object would look like this: 

```
spec:
  virtualhost:
    aliases:
    - alias1.com
    - alias2.com
    fqdn: foo.region1.com    #local-fqdn
    fqdnType: Exact
    gslb:
      fqdn: global-foo.com   #global-fqdn
      includeAliases: true   #defaultValue: false
```