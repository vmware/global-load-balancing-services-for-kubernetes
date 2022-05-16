## GSLBHostRule CRD for AMKO
The `GSLBHostRule` CR allows users to override certain properties of a specific GslbService object on the Avi Controller created by AMKO.

A typical `GSLBHostRule` looks like this:
```yaml
apiVersion: amko.vmware.com/v1alpha1
kind: GSLBHostRule
metadata:
  name: gslb-host-rule-1
  namespace: avi-system
spec:
  fqdn: foo.avi.internal
  sitePersistence:
    enabled: true
    profileRef: "gap-1"  # only enabled for secure ingresses/routes
  thirdPartyMembers:
  - site: non-avi-site
    vip: 10.10.10.10
  healthMonitorRefs:
  - hm1
  - hm2
  trafficSplit:
  - cluster: k8s
    weight: 15
  - cluster: oshift
    weight: 5
  ttl: 30
```
1. `namespace`: namespace of this object must be `avi-system`.

2. `fqdn`: FQDN of the GslbService.

3. `sitePersistence`: Enable Site Persistence for client requests. Set the `enabled` flag as `true` and add a `profileRef` for a pre-created Application Persistence Profile created on the Avi Controller. Please follow the steps [here](https://avinetworks.com/docs/20.1/gslb-site-cookie-persistence/#outline-of-steps-to-be-taken) to create a federated Application Persistence Profile on the Avi Controller.

**Note** that site persistence is **disabled** on GslbServices created for **insecure** ingresses/routes, irrespective of this field.
If this field is not provided in `GSLBHostRule`, the site persistence property will be inherited from the GDP object.

4. `thirdPartyMembers`: To add one or more third party members to a GS from a non-avi site (third party site) for the purpose of maintenance, specify a list of those members. For each member, provide the site name in `site` and IP address in `vip`. Please refer [here](https://avinetworks.com/docs/20.1/gslb-third-party-site-configuration-and-operations/#associating-third-party-services-with-third-party-sites) to see how to add third party sites to existing Gslb configuration. **Note** that, to add third party members, set the `enable` flag in `sitePersistence` to false for this object. If site persistence is enabled for a GSLB Service, third party members can't be added.

**Note** that the site must be added to the GSLB leader as a 3rd party site before adding the member here.

5. `healthMonitorRefs`: If a GslbService requires some custom health monitoring, the user can create a federated custom health monitor in the Avi Controller and provide the ref(s) here. To add a custom health monitor, follow the steps [here](https://avinetworks.com/docs/20.1/avi-gslb-service-and-health-monitors/#configuring-health-monitoring). If no custom health monitor refs have been added, the `healthMonitorTemplate` from the `GDP`/`GSLBHostRule` object will be inherited or `healthMonitorRefs` from the GDP object will be inherited.

6. `healthMonitorTemplate`: If a GslbService requires customization of the health monitor settings, the user can create a federated custom health monitor template in the Avi Controller and provide the name of it here. To add a health monitor template, follow the steps [here](https://avinetworks.com/docs/20.1/avi-gslb-service-and-health-monitors/#configuring-health-monitoring). Currently, the `Client Request Header` and `Response Code` of the health monitor template are inherited. If no custom health monitor template has been added, the `healthMonitorRefs` from the `GDP`/`GSLBHostRule` object will be inherited or `healthMonitorTemplate` from the GDP object will be inherited.

**Note** User can provide either `healthMonitorRefs` or `healthMonitorTemplate` in the `GSLBHostRule` objects. The health monitor template added in the controller must be of type HTTP/HTTPS.

7. `trafficSplit`: Specify traffic steering to member clusters/sites. The traffic is then split proportionately between two different clusters. Weight for each cluster must be provided between 1 to 20. If not added, GDP object's traffic split applies on this GslbService.

8. `ttl`: Override the default `ttl` value specified on the GDP object using this field.

9. `poolAlgorithmSettings`: Override the default GslbService algorithm provided in the GDP object. Refer to [pool algorithm settings](#pool-algorithm-settings) for details. If this field is absent, GDP's pool algorithm's settings apply on this GslbService.

## Pool Algorithm Settings
The pool algorithm settings for GslbService(s) can be specified via the `GDP` or a `GSLBHostRule` objects. The GslbService uses the algorithm settings to distribute the traffic accordingly. To set the required settings, following fields must be used:
```yaml
  poolAlgorithmSettings:
    lbAlgorithm:
    hashMask:
    geoFallback:
      lbAlgorithm:
      hashMask:
```

`lbAlgorithm` is used to specify the name of the algorithm. Supported algorithms are:
1. GSLB_ALGORITHM_CONSISTENT_HASH (needs the hash mask in the `hashMask` field).
2. GSLB_ALGORITHM_GEO (needs the fallback algorithm settings to be specified in `geoFallback` feilds)
3. GSLB_ALGORITHM_ROUND_ROBIN (default)
4. GSLB_ALGORITHM_TOPOLOGY

If `GSLB_ALGORITHM_GEO` is set as the main algorithm, the user needs to specify the `geoFallback` settings. `geoFallback.lbAlgorithm` can have either of the two values:
1. GSLB_ALGORITHM_CONSISTENT_HASH (needs the hash mask in `geoFallback.hashMask`)
2. GSLB_ALGORITHM_ROUND_ROBIN

For more details on the algorithm that best fits the user needs and it's configuration on the Avi Controller, follow [this](https://avinetworks.com/docs/20.1/gslb-architecture-terminology-object-model/#load-balancingalgorithms-for-gslb-pool-members) link.

## Caveats:
* Site Persistence cannot be enabled for the GslbServices which have insecure ingresses or routes as the members.
