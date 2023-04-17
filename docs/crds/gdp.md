## GlobalDeploymentPolicy CRD for AMKO
A CRD called GlobalDeploymentPolicy allows users to select kubernetes/openshift objects based on certain rules. The selection policy applies to all the clusters which are mentioned in the GDP object.

**Note** that `v1alpha1` for the GDP object is deprecated now and AMKO won't honor any changes in the `v1alpha1` version of a GDP object.

A typical GlobalDeploymentPolicy looks like this:

```yaml
apiVersion: "amko.k8s.io/v1alpha2"
kind: "GlobalDeploymentPolicy"
metadata:
  name: "global-gdp"
  namespace: "avi-system"   // a cluster-wide GDP
spec:
  matchRules:
    appSelector:
      label:
        app: gslb
    namespaceSelector:
      label:
        ns: gslb
 
  matchClusters:
    - cluster: cluster1-admin    // cluster names are kubernetes cluster contexts
    - cluster: cluster2-admin
 
  trafficSplit:
    - cluster: cluster1
      weight: 8
      priority: 2
    - cluster: cluster2
      weight: 2
      priority: 3

  ttl: 10

  healthMonitorRefs:
  - my-health-monitor1

  sitePersistenceRef: gap-1

  poolAlgorithmSettings:
    lbAlgorithm: GSLB_ALGORITHM_ROUND_ROBIN
```
1. `namespace`: namespace of this object must be `avi-system`.
2. `matchRules`: This allows users to select objects using either application labels (configured as labels on Ingress/Route objects) or via namespace labels (configured as labels on the namespace objects). `matchRules` are defined as:
```yaml
matchRules:
    appSelector:                       // application selection criteria
      label:
        app: gslb                       // kubernetes/openshift label key-value
    namespaceSelector:                 // namespace selection criteria
      label:
        ns: gslb                        // kubernetes/openshift label key-value
```
A combination of appSelector and namespaceSelector will decide which objects will be selected for GSLB service consideration.
- appSelector: Selection criteria only for applications:
  * label: will be used to match the ingress/service type load balancer labels (key:value pair).
- namespaceSelector: Selection criteria only for namespaces:
  * label: will be used to match the namespace labels (key:value pair).

AMKO supports the following combinations for GDP matchRules:
| **appSelector** | **namespaceSelector** | **Result**                                                                                         |
| --------------- | --------------------- | -------------------------------------------------------------------------------------------------- |
| yes             | yes                   | Select all objects satisfying appSelector and from the namespaces satisfying the namespaceSelector |
| no              | yes                   | Select all objects from the selected namespaces (satisfying namespaceSelector)                     |
| yes             | no                    | Select all objects satisfying the appSelector criteria from all namespaces                         |
| no              | no                    | No objects selected (default action)                                                               |

Example Scenarios:

> Select objects with label `app:gslb` from all the namespaces:
```yaml
  matchRules:
    appSelector:
      label:
        app: gslb
```

> Select objects with label `app:gslb` and from namespaces labelled `ns:prod`:
```yaml
matchRules:
    appSelector:
      label:
        app: gslb
    namespaceSelector:
      label:
        ns: prod
```

3. `matchClusters`: List of clusters on which the above `matchRules` will be applied on. The member object of this list are cluster contexts of the individual k8s/openshift clusters.

4. `trafficSplit` is required if we want to route a percentage of traffic to objects in a given cluster. Weights for these clusters range from 1 to 20. `trafficSplit` can also be used to prioritize certain clusters before others. Maximum value for priority is 100 and default is 10. Let's say two clusters are given a priority of 20 and a third cluster is added with a priority of 10. The third cluster won't be routed any traffic unless both cluster1 and cluster2 (with priority 20) are down.

5. `ttl`: Use this flag to set the Time To Live value. The value can range from 1-86400 seconds. This determines the frequency with which clients need to obtain fresh steering information for client requests. If none is specified in the GDP object, the value defaults to the one specified in the DNS application profile.

6. `healthMonitorRefs`: Provide federated custom health monitors. If this option is used and refs are specified, the default path based health monitoring will be deleted for the GslbServices. If no custom health monitors are specified, `healthMonitorTemplate` from the GDP object will be inherited or AMKO sets the default health monitors for all GslbServices.

   ```yaml
    healthMonitorRefs:
    - my-health-monitor1
   ```

7. `healthMonitorTemplate`: If a GslbService requires customization of the health monitor settings, the user can create a federated custom health monitor template in the Avi Controller and provide the name of it here. To add a health monitor template, follow the steps [here](https://avinetworks.com/docs/20.1/avi-gslb-service-and-health-monitors/#configuring-health-monitoring). Currently, the `Client Request Header` and `Response Code` of the health monitor template are inherited. If no custom health monitor template has been added, the `healthMonitorRefs` from the GDP object will be inherited or AMKO sets the default health monitors.

   ```yaml
    healthMonitorTemplate: my-health-monitor-template-1
   ```

   **Note** User can provide either `healthMonitorRefs` or `healthMonitorTemplate` in the `GDP` objects. The health monitor template added in the controller must be of type HTTP/HTTPS.

8. `sitePersistenceRef`: Provide an Application Persistence Profile ref (pre-created in Avi Controller). This has to be a federated profile. Please follow the steps [here](https://avinetworks.com/docs/20.1/gslb-site-cookie-persistence/#outline-of-steps-to-be-taken) to create a federated Application Persistence Profile on the Avi Controller. If no reference is provided, Site Persistence is disabled.

9. `poolAlgorithmSettings`: Provide the GslbService pool algorithm settings. Refer to [pool algorithm settings](gslbhostrule.md#pool-algorithm-settings) for details. If this field is absent, the default is assumed as Round Robin algorithm.

10. `downResponse`: Specifies the response to the client query when the GSLB service is DOWN. If this field is absent, the GSLB service will be configured with `GSLB_SERVICE_DOWN_RESPONSE_NONE`. Refer to [down response settings](gslbhostrule.md#down-response-settings) for details.

### Notes
* Only one `GDP` object is allowed.

* If using `helm install`, a `GDP` object is created by picking up values from `values.yaml` file. User can then edit this GDP object to modify their selection of objects.

* `trafficSplit`, `ttl`, `sitePersistence` and `healthMonitorRefs` provided in the GDP object are applicable on all the GslbServices. These properties, however, can be overridden via `GSLBHostRule` created for a GslbService. More details [here](gslbhostrule.md).

* Site Persistence, if specified, will only be enabled for the GslbServices which have secure ingresses or secure routes as the members and will be disabled for all other cases.

* HTTP Health Monitors can't be used for GslbServices with HTTPS objects as the members.
