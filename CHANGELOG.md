# Change log:

All notable changes to this project will be documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
 

## AMKO-1.4.1-beta

### Changed:
  - `GlobalDeploymentPolicy` object structure changes. GDP `v1alpha1` is now deprecated and `v1alpha2` supported.
  - Log fixes for error reporting.

### Added:
  - AMKO support for selecting custom FQDNs for GSs via `GSLBConfig`.
  - AMKO support for `GSLBHostRule`.
  - AMKO support for GslbService properties: TTL, Site Persistence and Custom Health Monitors via `GlobalDeploymentPolicy` and `GSLBHostRule`.
  - AMKO support for adding third party site members via `GSLBHostRule`.

## AMKO-1.4.2

### Added:
  - Support for [federation](docs/AMKO/federation.md). It has to be enabled during installation.
  - A new custom resource `AMKOCluster` for federation configuration.
  - AMKO can now boot up, even if one of the member clusters is unreachable. If the cluster is available later on, AMKO will start it's informers.

### Bugs fixed:
  - Parsing error for TTL and hash mask fields after creation of GslbServices
  - Path based health monitors gets unnecessarily created and then deleted sometimes, even if custom health monitor refs are provided
  - GslbServices unnecessarily updated due to incorrect parsing of site persistence field
  - AMKO doesn't panic if the GSLB leader details couldn't be fetched

## AMKO-1.5.1

### Added:
  - Support for rebooting AMKO if the GSLB leader IP address is updated in the `GSLBConfig` object.

### Bugs fixed:
  - Fixed a status update race between re-sync interval goroutine and modification to `GSLBConfig` object.

## AMKO-1.5.2

### Updated:
  - Base image `photon:4.0` updated with fixes for latest vulnerabilities

## AMKO-1.6.1

### Added:
  - AMKO supports Kubernetes 1.22.
  - Support for multiple AMKO installations.
  - Support for GSLB pool property `priority` via `GlobalDeploymentPolicy` and `GSLBHostRule`.
  - Introduced support for broadcasting AMKO pod `Events` in order to enhance the observability and monitoring aspects.

### Changed:
 - Encode names of all HM objects except HM created for passthrough ingress/routes.


## AMKO-1.7.1

### Added:
  - Support for multiple FQDNs to a single GS using `HostRule` CRD.
  - Support for multi-cluster load balancing for applications deployed in the public cloud.
  - GSLB Monitors settings created by AMKO can now be customized via `GlobalDeploymentPolicy` and `GSLBHostRule`.

### Bugs fixed:
  - Fixed the issue of the AMKO pod does not respond periodically.
  - AMKO now takes into account the priority values given in the `GSLBHostRule` objects.


## AMKO-1.8.1

### Added:
  - Support for Kubernetes 1.24.

### Bugs fixed:
  - Fixed an issue of AMKO updating the health monitors with the wrong ports.