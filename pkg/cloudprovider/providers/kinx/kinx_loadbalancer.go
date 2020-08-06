package kinx

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

// LbaasV2 is a LoadBalancer implementation for Neutron LBaaS v2 API
type LBaasV2 struct{}

func (k *Kinx) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return &LBaasV2{}, true
}

func (lbaas *LBaasV2) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	status := &corev1.LoadBalancerStatus{}
	return status, true, cloudprovider.NotImplemented
}

// GetLoadBalancerName returns the constructed load balancer name.
func (lbaas *LBaasV2) GetLoadBalancerName(ctx context.Context, clusterName string, service *corev1.Service) string {
	name := fmt.Sprintf("kube_service_%s_%s_%s", clusterName, service.Namespace, service.Name)
	return cutString(name)
}

// cutString makes sure the string length doesn't exceed 255, which is usually the maximum string length in OpenStack.
func cutString(original string) string {
	ret := original
	if len(original) > 255 {
		ret = original[:255]
	}
	return ret
}

// EnsureLoadBalancer creates a new load balancer or updates the existing one.
func (lbaas *LBaasV2) EnsureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	klog.Info("Hello LoadBalancer!")
	status := &corev1.LoadBalancerStatus{}
	return status, cloudprovider.NotImplemented
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (lbaas *LBaasV2) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	return cloudprovider.NotImplemented
}

// EnsureLoadBalancerDeleted deletes the specified load balancer
func (lbaas *LBaasV2) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	return nil
}
