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
    pkiProfileRef: pki-1 
  thirdPartyMembers:
  - site: non-avi-site
    vip: 10.10.10.10
    publicIP: 122.162.150.96
  healthMonitorRefs:
  - hm1
  - hm2
  trafficSplit:
  - cluster: k8s
    weight: 15
    priority: 10
  - cluster: oshift
    weight: 5
    priority: 10
   publicIP:
  - cluster: k8s
    ip: 160.10.1.1
  - cluster: oshift
    ip: 170.11.1.1
  ttl: 30
  controlPlaneHmOnly: false
```
1. `namespace`: namespace of this object must be `avi-system`.

2. `fqdn`: FQDN of the GslbService.

3. `sitePersistence`: Enable Site Persistence for client requests. Set the `enabled` flag as `true` and add a `profileRef` for a pre-created Application Persistence Profile created on the Avi Controller. Please follow the steps [here](https://avinetworks.com/docs/20.1/gslb-site-cookie-persistence/#outline-of-steps-to-be-taken) to create a federated Application Persistence Profile on the Avi Controller.

   `pkiProfileRef`: Provide an PKI Profile ref (pre-created in Avi Controller).This has to be a federated profile. It will be applied only if sitePersistence is enabled.

**Note** that site persistence is **disabled** on GslbServices created for **insecure** ingresses/routes, irrespective of this field.
If this field is not provided in `GSLBHostRule`, the site persistence property will be inherited from the GDP object.

4. `thirdPartyMembers`: To add one or more third party members to a GS from a non-avi site (third party site) for the purpose of maintenance, specify a list of those members. For each member, provide the site name in `site` and IP address in `vip`. Please refer [here](https://avinetworks.com/docs/20.1/gslb-third-party-site-configuration-and-operations/#associating-third-party-services-with-third-party-sites) to see how to add third party sites to existing Gslb configuration. Optional `publicIP` in IPv4 format can be added if `vip` IP is private and not accesible by client network .Please check [here](https://avinetworks.com/docs/latest/nat-aware-public-private-configuration) for more details.   **Note** that, to add third party members, set the `enable` flag in `sitePersistence` to false for this object. If site persistence is enabled for a GSLB Service, third party members can't be added.

**Note** that the site must be added to the GSLB leader as a 3rd party site before adding the member here.

5. `healthMonitorRefs`: If a GslbService requires some custom health monitoring, the user can create a federated custom health monitor in the Avi Controller and provide the ref(s) here. To add a custom health monitor, follow the steps [here](https://avinetworks.com/docs/20.1/avi-gslb-service-and-health-monitors/#configuring-health-monitoring). If no custom health monitor refs have been added, the `healthMonitorTemplate` from the `GDP`/`GSLBHostRule` object will be inherited or `healthMonitorRefs` from the GDP object will be inherited.

   ```yaml
    healthMonitorRefs:
    - my-health-monitor1
   ```

6. `healthMonitorTemplate`: If a GslbService requires customization of the health monitor settings, the user can create a federated custom health monitor template in the Avi Controller and provide the name of it here. To add a health monitor template, follow the steps [here](https://avinetworks.com/docs/20.1/avi-gslb-service-and-health-monitors/#configuring-health-monitoring). Currently, the `Client Request Header` and `Response Code` of the health monitor template are inherited. If no custom health monitor template has been added, the `healthMonitorRefs` from the `GDP`/`GSLBHostRule` object will be inherited or `healthMonitorTemplate` from the GDP object will be inherited.

   ```yaml
    healthMonitorTemplate: my-health-monitor-template-1
   ```

   **Note** User can provide either `healthMonitorRefs` or `healthMonitorTemplate` in the `GSLBHostRule` objects. The health monitor template added in the controller must be of type HTTP/HTTPS.

7. `trafficSplit`: Specify traffic steering to member clusters/sites. The traffic is then split proportionately between two different clusters. Weight for each cluster must be provided between 1 to 20. If not added, GDP object's traffic split applies on this GslbService.`trafficSplit` can also be used to prioritize certain clusters before others. Maximum value for priority is 100 and default is 10. Let's say two clusters are given a priority of 20 and a third cluster is added with a priority of 10. The third cluster won't be routed any traffic unless both cluster1 and cluster2 (with priority 20) are down.

8. `publicIP`: An optional public IP address (IPv4) can be specified for each site. This field is used to host the public IP address for the VIP, which gets NAT’ed to the private IP by a firewall. Please check [here](https://avinetworks.com/docs/latest/nat-aware-public-private-configuration) for more details.

9. `ttl`: Override the default `ttl` value specified on the GDP object using this field.

10. `poolAlgorithmSettings`: Override the default GslbService algorithm provided in the GDP object. Refer to [pool algorithm settings](#pool-algorithm-settings) for details. If this field is absent, GDP's pool algorithm's settings apply on this GslbService.

11. `downResponse`: Specifies the response to the client query when the GSLB service is DOWN. If this field is absent, GDP's down response settings would get applied on the GslbService. Refer to [down response settings](#down-response-settings) for details.

12. `controlPlaneHmOnly`: If this boolean flag is set to `true`, only control plane health monitoring will be done. AMKO will not add any `healthMonitorRefs` or create any data plane health monitors. It is `false` by default.


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

## Down Response Settings
Down Response specifies the response to the client query when the GSLB service is DOWN. The down response settings for GslbService(s) can be specified via the `GDP` or `GSLBHostRule` objects.
To following fields must be used to set the down response,:

```yaml
  downResponse:
    type:
    fallbackIP: # required only when the type is set as GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP
```

`type` is used to specify the type of response from DNS service towards the client when the GSLB service is DOWN. Supported types are:
1. GSLB_SERVICE_DOWN_RESPONSE_NONE - No response to the client query when the GSLB service.
2. GSLB_SERVICE_DOWN_RESPONSE_ALL_RECORDS - Respond with all the records to the client query when the GSLB.
3. GSLB_SERVICE_DOWN_RESPONSE_FALLBACK_IP - Respond with the given fallback IP address to the client query when GSLB service is down.
4. GSLB_SERVICE_DOWN_RESPONSE_EMPTY - Respond with an empty response to the client query when the GSLB service is down.

`fallbackIP` is the fallback IP address to use in A response to the client query when the GSLB service is DOWN.

## Caveats:
* Site Persistence cannot be enabled for the GslbServices which have insecure ingresses or routes as the members.
* If `pkiProfileRef` is empty but `sitePersistence.enabled` is set to true AMKO will apply a federated pki profile present on controller since pkiProfile is mandatory with site persistence starting with AVI controller 22.1.3 . GSLB service creation will fail if no federated pki Profile is present on controller.
