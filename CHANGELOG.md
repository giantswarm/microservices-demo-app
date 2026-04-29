# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Update kong setup to make it work with Gateway API.
- Add kong app and user values configmap to kustomization.

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

[Unreleased]: https://github.com/giantswarm/microservices-demo-app/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/giantswarm/microservices-demo-app/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/giantswarm/microservices-demo-app/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/giantswarm/microservices-demo-app/releases/tag/v0.1.0
