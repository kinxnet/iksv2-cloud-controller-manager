# iksv2-cloud-controller-manager

## Annotation 정보

모든 Annotation의 prefix는 별도 표기가 없으면 `service.beta.kubernetes.io` 입니다.

### Protocol 관련 Annotation

| Annotation | 타입 | 기본값 | 유효값 | 설명 |
|---|---|---|---|---|
| `kinx-load-balancer-backend-protocol` | string | - **(필수)** | `http`, `tcp`, `terminated_https` | 백엔드 프로토콜 |
| `kinx-load-balancer-tls-container-ids` | string | - | Barbican 인증서 ID (`,`로 구분하여 복수 설정 가능) | TLS 인증서. `terminated_https`일 때만 사용 |
| `kinx-load-balancer-low-tlsv` | bool | `false` | `true` / `false` | TLS 1.0/1.1 비활성화. `terminated_https`일 때만 유효 |
| `kinx-load-balancer-redirect-http` | bool | `false` | `true` / `false` | HTTP → HTTPS 리다이렉트. `http`일 때만 유효 |
| `kinx-load-balancer-proxy-protocol` | bool | `false` | `true` / `false` | Proxy Protocol 활성화. `tcp`일 때만 유효 |

### Load Balancer 설정 Annotation

| Annotation | prefix | 타입 | 기본값 | 유효값 | 설명 |
|---|---|---|---|---|---|
| `openstack-internal-load-balancer` | `service.beta.kubernetes.io` | bool | `false` | `true` / `false` | `true`이면 lb-provider를 Private 으로 전환 |
| `lb-method` | `loadbalancer.openstack.org` | string | cloud config의 `lb-method` 값 | `ROUND_ROBIN`, `LEAST_CONNECTIONS`, `SOURCE_IP` | LB 알고리즘. 서비스별로 cloud config 기본값 재정의 가능 |

### Health Monitor Annotation

헬스체크 유형: TCP NodePort 고정

| Annotation | 타입 | 기본값 | 설명 |
|---|---|---|---|
| `kinx-load-balancer-healthcheck-interval` | int | `5` | 헬스체크 주기 (초) |
| `kinx-load-balancer-healthcheck-retry` | int | `3` | 헬스체크 최대 재시도 횟수 |
| `kinx-load-balancer-healthcheck-timeout` | int | `5` | 헬스체크 타임아웃 (초) |

---

## local에서 debugging 하는 방법 (vscode 기준)

> cloud-controller-manager 및 operator도 비슷한 방식으로 디버깅 가능할 듯

1. Kubernetes Cluster 구성
2. local에서 접속 가능한 .kube/config 구성
3. cloud-controller-manager daemonset 삭제(중복실행 방지)
4. cloud-config 작성
   - Kubernetes Cluster가 구성된 Openstack 인증정보 작성
   - auth-url에 local에서 접속이 가능해야 함.
    ``` ini
    [Global]
    auth-url=""
    username=""
    password=""
    region=""
    tenant-id=""
    tenant-name=""
    domain-name=""
    ```
5. .vscode/launch.json 작성
   - cloud-controller-manager deployment에 적용되어있는 args 적용
    ``` json
    // args 예
    {
      ...
      "program": "cmd/cloud-controller-manager",
      "args": [
          "--v=1",
          "--cloud-config=./cloud-config",
          "--cloud-provider=openstack",
          "--use-service-account-credentials=true",
          "--leader-elect=false",
          "--address=127.0.0.1",
          "--kubeconfig=/Users/{username}/.kube/config",
          "--authentication-kubeconfig=/Users/{username}/.kube/config",
      ]
      ...
    }
    ```
6. vscode 디버깅 시작

---

## build & push

### ko 사용 (권장)

[ko](https://ko.build)는 Dockerfile 없이 Go 바이너리를 OCI 이미지로 직접 빌드·푸시할 수 있는 도구입니다.

```bash
# ko 설치
go install github.com/google/ko@latest

# 이미지 빌드 (로컬 docker daemon에 로드)
make ko-build VERSION=v1.0.0

# 이미지 빌드 + 레지스트리 푸시
make ko-publish VERSION=v1.0.0 REGISTRY=ghcr.io/kinxnet/iksv2-cloud-controller-manager
```

### Docker 사용 (기존 방식)

```bash
# Makefile의 VERSION, REGISTRY 등 필요한 변수 수정 후 실행
make build-images
make push-images
```

---

## Reference

https://github.com/kubernetes/cloud-provider-openstack/blob/master/docs/getting-started-provider-dev.md

kubernetes package 를 go module에 추가하기 위한 이슈
https://github.com/kubernetes/kubernetes/issues/79384

How to develop kubernetes cloud provider

https://kubernetes.io/docs/tasks/administer-cluster/developing-cloud-controller-manager/

Cloud Provider Interface

https://github.com/kubernetes/cloud-provider/blob/master/cloud.go#L43-L68

- Loadbalancer
    - https://github.com/kubernetes/cloud-provider/blob/master/cloud.go#L132-L161

restart kube-apiserver

https://stackoverflow.com/questions/42674726/restart-kube-apiserver-when-provisioned-with-kubeadm


cloud contorller 관리

https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/
