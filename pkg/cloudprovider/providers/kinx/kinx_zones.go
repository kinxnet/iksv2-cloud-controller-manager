package kinx

import (
	"context"

	cloudprovider "k8s.io/cloud-provider"

	"k8s.io/apimachinery/pkg/types"
)

// Zones indicates that we support zones
func (k *Kinx) Zones() (cloudprovider.Zones, bool) {
	return k, false
}

// GetZone returns the current zone
func (k *Kinx) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	zone := cloudprovider.Zone{
		FailureDomain: "",
		Region:        "",
	}
	return zone, cloudprovider.NotImplemented
}

// GetZoneByProviderID implements Zones.GetZoneByProviderID
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (k *Kinx) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	zone := cloudprovider.Zone{
		FailureDomain: "",
		Region:        "",
	}
	return zone, cloudprovider.NotImplemented
}

// GetZoneByNodeName implements Zones.GetZoneByNodeName
// This is particularly useful in external cloud providers where the kubelet
// does not initialize node data.
func (k *Kinx) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	zone := cloudprovider.Zone{
		FailureDomain: "",
		Region:        "",
	}
	return zone, cloudprovider.NotImplemented
}
