# Change Log
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [1.0.7] - 2026-06-01
### Fixed
- `service.beta.kubernetes.io/kinx-load-balancer-backend-protocol` annotation에 대문자/혼합 케이스(예: `HTTP`, `Tcp`) 입력 시 빈 프로토콜로 LB 생성이 실패하던 문제 수정 (iksv2-api#13)
- annotation 값을 읽은 직후 `strings.ToLower`로 단일 정규화하여 `getListenerProtocol` / `getPoolProtocol` switch 문과의 대소문자 불일치 및 기존 리스너 갱신 시 잘못된 conflict 경고 로그 해소 (iksv2-api#13)

## [1.0.6] - 2026-05-14
### Fixed
- `vbom.ml/util` 도메인 소멸(404)로 인한 `go mod download` 실패 해결 — `go.mod` replace 디렉티브로 `github.com/fvbommel/util` 미러 우회 적용 (iksv2-api#8)
- `kinx_loadbalancer.go` 내 `lbProviderPublic` / `lbProviderPrivate` 상수 중복 선언으로 인한 컴파일 오류 제거 (iksv2-api#8)

## [1.0.5] - 2026-04-16
### Added
- 서비스별 `loadbalancer.openstack.org/lb-method` 어노테이션 지원 — cloud config 기본값을 서비스 단위로 재정의 가능
- `service.beta.kubernetes.io/openstack-internal-load-balancer` 어노테이션 지원 — `true` 설정 시 lb-provider를 Private 으로 전환

### Changed
- ProviderName을 `"kinx"`에서 `"openstack"`으로 변경