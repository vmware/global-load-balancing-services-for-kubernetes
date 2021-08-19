# Deriving GslbService FQDNs

## FQDN Modes
AMKO decides the FQDNs for the GslbServices based on two modes:
1. Default Mode: In this mode, the hostname(s) field in the status of the Ingress/Route/Service of type Loadbalancer object is used to determine the hostname of the GSLB Service. Each hostname uniquely maps to a GS FQDN automatically. For common hostname across clusters, a single GSLB Service is created with pool members from each cluster that share the hostname.

2. Custom Global Fqdn Mode: In this mode, AMKO checks the AKO HostRules to figure out the GslbService Fqdn. To expose an application via GSLB, the user must provide a mapping between the local fqdn and the global fqdn via AKO's HostRule object. See [this](https://github.com/vmware/load-balancer-and-ingress-services-for-kubernetes/blob/master/docs/crds/hostrule.md#configure-gslb-fqdn). If the user wishes to map two application instances (which have different FQDNs) in two different clusters, they have to create HostRules in both clusters specifying the mapping.

![Alt text](images/local_vs_global_fqdn.png?raw=true "local and global fqdn modes")

## How to specify the custom global fqdn mode
The user has to set the `useCustomGlobalFqdn` to `true` in the `GSLBConfig` object. This is a static operation and if changed while AMKO is already deployed, the changes won't take any effect. If `useCustomGlobalFqdn` was previously `false` and then set to `true`, and the user reboots AMKO, the pre-created GslbServices in Avi will be deleted.

## When to use the default mode
![Alt text](images/local_fqdn.png?raw=true "local fqdn usage")

Users will find it useful when they have two instances of an application deployed on two different clusters which have the same FQDNs. And, they would want to expose all these applications to be exposed as GslbServices in one shot.

## When to use Custom Global Fqdn mode
![Alt text](images/global_fqdn.png?raw=true "global fqdn usage")

If users have site local FQDNs and they would want to use DNS loadbalancing for these application instances a common GSLB FQDN can be used. Here the common GSLB FQDN maps the vips of the site local FQDNs as pool members.