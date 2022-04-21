# iksv2-cloud-controller-manager

### Annotation 정보
#### Protocol 관련 Annotaion
  - "service.beta.kubernetes.io/kinx-load-balancer-backend-protocol"
    - (Reqired) terminated_https, http, tcp
  
  - "service.beta.kubernetes.io/kinx-load-balanver-tls-container-ids"
    - kinx-load-balancer-backend-protocol : terminated_https
    - barbican ssl 인증서 id
    - ,(comma)로 구분하여 복수개 설정 가능
    
  - "service.beta.kubernetes.io/kinx-load-balancer-low-tlsv"
    - kinx-load-balancer-backend-protocol : terminated_https

  - "service.beta.kubernetes.io/kinx-load-balancer-redirect-http"
    - kinx-load-balancer-backend-protocol : http

  - "service.beta.kubernetes.io/kinx-load-balancer-proxy-protocol"
    - kinx-load-balancer-backend-protocol : tcp

#### Health Monitor Annotation
-  health monitor 유형 : tcp nodeport 고정
  - "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-interval"
    - default 5
  - "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-retry"
    - default 3
  - "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-timeout"
    - default 5
  

### local에서 debugging 하는 방법 (vscode 기준)
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
          "--cloud-provider=kinx",
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

### build & push
1. Makefile의 Version, Registry 등 필요한 데이터 수정
2. docker & golang 설치
3. make build-images 실행
4. make push-images 실행

### Reference

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
