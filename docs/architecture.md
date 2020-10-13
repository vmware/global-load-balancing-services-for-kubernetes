## AMKO: Architecture

The Avi Multi-cluster Kubernetes Operator is used to provide application load-balancing across multiple clusters using Avi's enterprise grade GSLB capabilities.

![Alt text](amko_arch.png?raw=true "amko architecture")

The AMKO controller ingests the member clusters' kubernetes API server object updates to construct corresponding GSLB services in the Avi GSLB leader controller. AMKO is deployed on one of the member clusters and has access to all the member clusters' kubernetes API server.