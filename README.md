# iksv2-cloud-controller-manager

### TLS Terminated Loadbalancer 생성
- manifest 예시
  ``` yaml
  apiVersion: v1
  kind: Service
  metadata:
    annotations:
      loadbalancer.openstack.org/tls-container-ids: {barbican secret container id}, {barbican secret container id}, ...
  spec:
    ports:
      - port: 443
        targetPort: {TargetPort}
    type: LoadBalancer
  ```
- tls-container를 설정할 경우 Listener의 Protocol은 TERMINATED_HTTPS로 고정됨.
- annotations *loadbalancer.openstack.org/tls-container-ids* 값은 여러개 설정 가능(comma로 구분)
- tls terminated Loadbalancer <-> 일반 Loadbalancer 전환의 경우 서비스 삭제 후 재생성 필요

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
