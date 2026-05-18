# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Update `envoy-loadtesting` to allow pushing k6 runner pods' metrics to mimir.
- Authenticate k6 runner pods' prometheus remote-write against mimir by mirroring `kube-system/alloy-metrics` credentials into a `k6-prometheus-rw-credentials` secret consumed via `envFrom`.

## [0.3.0] - 2026-04-30

### Changed

- Update kong setup to make it work with Gateway API.
- Add kong app and user values configmap to kustomization.
- Update `envoy-loadtesting` so that kong and nginx are not deployed at the same time on the workload cluster. Only one of those is selected by the user.

## [0.2.1] - 2026-04-16

### Fixed

- Fixed kong support.

## [0.2.0] - 2026-04-15

### Added

- Add support for Kong managed ingresses in the chart.
- Add Kong as an additional app in `envoy-loadtesting`.

### Changed

- Removed custom clusterissuers in helm chart.

## [0.1.0] - 2026-04-08

- Initial version of the app.

[Unreleased]: https://github.com/giantswarm/microservices-demo-app/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/giantswarm/microservices-demo-app/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/giantswarm/microservices-demo-app/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/giantswarm/microservices-demo-app/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/microservices-demo-app/releases/tag/v0.1.0
