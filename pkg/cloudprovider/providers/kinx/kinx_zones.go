package kinx

import (
	"context"

	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"

	"github.com/kinxnet/cloud-provider-kinx/pkg/util/metadata"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

// Zones indicates that we support zones
func (k *Kinx) Zones() (cloudprovider.Zones, bool) {
	klog.V(1).Info("Claiming to support Zones")
	return k, true
}

// GetZone returns the current zone
func (k *Kinx) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	md, err := metadata.Get(k.metadataOpts.SearchOrder)
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: md.AvailabilityZone,
		Region:        k.region,
	}
	klog.V(4).Infof("Current zone is %v", zone)
	return zone, nil
}

// GetZoneByProviderID implements Zones.GetZoneByProviderID
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (k *Kinx) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	compute, err := k.NewComputeV2()
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	var serverWithAttributesExt ServerAttributesExt
	if err := servers.Get(compute, instanceID).ExtractInto(&serverWithAttributesExt); err != nil {
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: serverWithAttributesExt.AvailabilityZone,
		Region:        k.region,
	}
	klog.V(4).Infof("The instance %s in zone %v", serverWithAttributesExt.Name, zone)
	return zone, nil
}

// GetZoneByNodeName implements Zones.GetZoneByNodeName
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (k *Kinx) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	compute, err := k.NewComputeV2()
	if err != nil {
		return cloudprovider.Zone{}, err
	}

	srv, err := getServerByName(compute, nodeName)
	if err != nil {
		if err == ErrNotFound {
			return cloudprovider.Zone{}, cloudprovider.InstanceNotFound
		}
		return cloudprovider.Zone{}, err
	}

	zone := cloudprovider.Zone{
		FailureDomain: srv.AvailabilityZone,
		Region:        k.region,
	}
	klog.V(4).Infof("The instance %s in zone %v", srv.Name, zone)
	return zone, nil
}
