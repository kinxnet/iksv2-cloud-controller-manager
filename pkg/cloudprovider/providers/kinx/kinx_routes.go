package kinx

import (
	"context"

	cloudprovider "k8s.io/cloud-provider"
)

type Routes struct{}

// Routes initializes routes support
func (k *Kinx) Routes() (cloudprovider.Routes, bool) {
	return &Routes{}, false
}

// ListRoutes lists all managed routes that belong to the specified clusterName
func (r *Routes) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	var routes []*cloudprovider.Route
	return routes, cloudprovider.NotImplemented
}

// CreateRoute creates the described managed route
func (r *Routes) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}

// DeleteRoute deletes the specified managed route
func (r *Routes) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	return cloudprovider.NotImplemented
}
