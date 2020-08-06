package kinx

import (
	cloudprovider "k8s.io/cloud-provider"
)

// Clusters is a no-op
func (k *Kinx) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}
