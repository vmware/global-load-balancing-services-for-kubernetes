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