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
  - Support for [federation](docs/federation.md). It has to be enabled during installation.
  - A new custom resource `AMKOCluster` for federation configuration.
  - AMKO can now boot up, even if one of the member clusters is unreachable. If the cluster is available later on, AMKO will start it's informers.

### Bugs fixed:
  - Fix parsing of TTL and hash mask after creating GslbServices.
  - Fix an unnecessary creation and deletion of path based health monitors, even if health monitor references are given.
  - Fix unnecessary updates to GslbServices due to incorrect parsing of GslbServices.
  - AMKO should panic if it can't find out the GSLB leader details.
