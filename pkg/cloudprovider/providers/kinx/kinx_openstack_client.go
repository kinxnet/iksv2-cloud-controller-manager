package kinx

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
)

// NewNetworkV2 creates a ServiceClient that may be used with the neutron v2 API
func (k *Kinx) NewNetworkV2() (*gophercloud.ServiceClient, error) {
	network, err := openstack.NewNetworkV2(k.openstackProvider, gophercloud.EndpointOpts{
		Region: k.region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find network v2 endpoint for region %s: %v", k.region, err)
	}
	return network, nil
}

// NewComputeV2 creates a ServiceClient that may be used with the nova v2 API
func (k *Kinx) NewComputeV2() (*gophercloud.ServiceClient, error) {
	compute, err := openstack.NewComputeV2(k.openstackProvider, gophercloud.EndpointOpts{
		Region: k.region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find compute v2 endpoint for region %s: %v", k.region, err)
	}
	return compute, nil
}

// NewBlockStorageV3 creates a ServiceClient that may be used with the Cinder v3 API
func (k *Kinx) NewBlockStorageV3() (*gophercloud.ServiceClient, error) {
	storage, err := openstack.NewBlockStorageV3(k.openstackProvider, gophercloud.EndpointOpts{
		Region: k.region,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize cinder v3 client for region %s: %v", k.region, err)
	}
	return storage, nil
}

// NewLoadBalancerV2 creates a ServiceClient that may be used with the Neutron LBaaS v2 API
func (k *Kinx) NewLoadBalancerV2() (*gophercloud.ServiceClient, error) {
	var lb *gophercloud.ServiceClient
	var err error
	lb, err = openstack.NewNetworkV2(k.openstackProvider, gophercloud.EndpointOpts{
		Region: k.region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find load-balancer v2 endpoint for region %s: %v", k.region, err)
	}
	return lb, nil
}
