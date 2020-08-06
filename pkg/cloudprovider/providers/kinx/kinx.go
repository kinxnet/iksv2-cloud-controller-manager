package kinx

import (
	"io"

	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the kinx provider
	ProviderName = "kinx"
)

// Kinx is an implementation of cloud provider Interface for Kinx.
type Kinx struct {
	region string
}

func init() {
	RegisterMetrics()
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cloud, err := NewKinx()
		if err != nil {
			klog.Warningf("New Kinx client created failed with config: %v", err)
		}
		return cloud, err
	})
}

func NewKinx() (*Kinx, error) {
	kinx := Kinx{
		region: "Test",
	}
	return &kinx, nil
}

// Initialize passes a Kubernetes clientBuilder interface to the cloud provider
func (k *Kinx) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// ProviderName returns the cloud provider ID.
func (k *Kinx) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if the cluster has a clusterID
func (k *Kinx) HasClusterID() bool {
	return true
}
