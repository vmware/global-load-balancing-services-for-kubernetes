Multi Cluster Controller or MCC is a project that is used to provide multi cluster load balancing for applications - GSLB and HACloud features.

GSLB - Load balancing across instances of the application that have been deployed to multiple locations (typically, multiple data centers and/or public clouds). Avi uses the Domain Name System (DNS) for providing the optimal destination information to the user clients 

To generate a secret with the kubeconfig file:
kubectl --kubeconfig my-config create secret generic gslb-config-secret --from-file gslb-members -n avi-system


HACloud - Federation of services across multiple kubernetes clusters which are typically within same region, without using DNS based load balancing. 
