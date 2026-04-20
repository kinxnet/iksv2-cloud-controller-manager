# Change Log
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [1.0.5] - 2026-04-16
### Added
- 서비스별 `loadbalancer.openstack.org/lb-method` 어노테이션 지원 — cloud config 기본값을 서비스 단위로 재정의 가능
- `service.beta.kubernetes.io/openstack-internal-load-balancer` 어노테이션 지원 — `true` 설정 시 lb-provider를 Private 으로 전환

### Changed
- ProviderName을 `"kinx"`에서 `"openstack"`으로 변경