package kinx

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

type Instances struct{}

func (k *Kinx) Instances() (cloudprovider.Instances, bool) {
	return &Instances{}, false
}

// NodeAddresses implements Instances.NodeAddresses
func (i *Instances) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	return []v1.NodeAddress{}, cloudprovider.NotImplemented
}

// NodeAddressesByProviderID returns the node addresses of an instances with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	return []v1.NodeAddress{}, nil
}

// InstanceID returns the cloud provider ID of the specified instance.
func (i *Instances) InstanceID(ctx context.Context, name types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

// InstanceType returns the type of the specified instance.
func (i *Instances) InstanceType(ctx context.Context, name types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

// InstanceTypeByProviderID returns the cloudprovider instance type of the node with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	return "", cloudprovider.NotImplemented
}

// AddSSHKeyToAllInstances is not implemented for OpenStack
func (i *Instances) AddSSHKeyToAllInstances(ctx context.Context, user string, keyData []byte) error {
	return cloudprovider.NotImplemented
}

// CurrentNodeName implements Instances.CurrentNodeName
// Note this is *not* necessarily the same as hostname.
func (i *Instances) CurrentNodeName(ctx context.Context, hostname string) (types.NodeName, error) {
	return types.NodeName(""), cloudprovider.NotImplemented
}

// InstanceExistsByProviderID returns true if the instance with the given provider id still exist.
// If false is returned with no error, the instance will be immediately deleted by the cloud controller manager.
func (i *Instances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	return true, cloudprovider.NotImplemented
}

// InstanceShutdownByProviderID returns true if the instances is in safe state to detach volumes
func (i *Instances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	return true, cloudprovider.NotImplemented
}
