package kinx

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/mitchellh/mapstructure"

	"github.com/kinxnet/cloud-provider-kinx/pkg/util"
	"github.com/kinxnet/cloud-provider-kinx/pkg/util/errors"
	"github.com/kinxnet/cloud-provider-kinx/pkg/util/metadata"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

// Instances encapsulates an implementation of Instances for OpenStack.
type Instances struct {
	compute        *gophercloud.ServiceClient
	opts           MetadataOpts
	networkingOpts NetworkingOpts
}

type ServerAttributesExt struct {
	servers.Server
	availabilityzones.ServerAvailabilityZoneExt
}

const (
	instanceShutoff = "SHUTOFF"
)

// Instances returns an implementation of Instances for OpenStack.
func (k *Kinx) Instances() (cloudprovider.Instances, bool) {
	klog.V(4).Info("openstack.Instances() called")

	compute, err := k.NewComputeV2()
	if err != nil {
		klog.Errorf("unable to access compute v2 API : %v", err)
		return nil, false
	}

	klog.V(4).Info("Claiming to support Instances")

	return &Instances{
		compute:        compute,
		opts:           k.metadataOpts,
		networkingOpts: k.networkingOpts,
	}, true
}

// CurrentNodeName implements Instances.CurrentNodeName
// Note this is *not* necessarily the same as hostname.
func (i *Instances) CurrentNodeName(ctx context.Context, hostname string) (types.NodeName, error) {
	md, err := metadata.Get(i.opts.SearchOrder)
	if err != nil {
		return "", err
	}
	return types.NodeName(md.Name), nil
}

// AddSSHKeyToAllInstances is not implemented for OpenStack
func (i *Instances) AddSSHKeyToAllInstances(ctx context.Context, user string, keyData []byte) error {
	return cloudprovider.NotImplemented
}

// NodeAddresses implements Instances.NodeAddresses
func (i *Instances) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	klog.V(4).Infof("NodeAddresses(%v) called", name)

	addrs, err := getAddressesByName(i.compute, name, i.networkingOpts)
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("NodeAddresses(%v) => %v", name, addrs)
	return addrs, nil
}

// NodeAddressesByProviderID returns the node addresses of an instances with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	instanceID, err := instanceIDFromProviderID(providerID)

	if err != nil {
		return []v1.NodeAddress{}, err
	}

	server, err := servers.Get(i.compute, instanceID).Extract()

	if err != nil {
		return []v1.NodeAddress{}, err
	}

	interfaces, err := getAttachedInterfacesByID(i.compute, server.ID)
	if err != nil {
		return []v1.NodeAddress{}, err
	}

	addresses, err := nodeAddresses(server, interfaces, i.networkingOpts)
	if err != nil {
		return []v1.NodeAddress{}, err
	}

	return addresses, nil
}

// InstanceExistsByProviderID returns true if the instance with the given provider id still exist.
// If false is returned with no error, the instance will be immediately deleted by the cloud controller manager.
func (i *Instances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return false, err
	}

	_, err = servers.Get(i.compute, instanceID).Extract()
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// InstanceShutdownByProviderID returns true if the instances is in safe state to detach volumes
func (i *Instances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		return false, err
	}

	server, err := servers.Get(i.compute, instanceID).Extract()
	if err != nil {
		return false, err
	}

	// SHUTOFF is the only state where we can detach volumes immediately
	if server.Status == instanceShutoff {
		return true, nil
	}
	return false, nil
}

// InstanceID returns the cloud provider ID of the specified instance.
func (i *Instances) InstanceID(ctx context.Context, name types.NodeName) (string, error) {
	srv, err := getServerByName(i.compute, name)
	if err != nil {
		if err == ErrNotFound {
			return "", cloudprovider.InstanceNotFound
		}
		return "", err
	}
	// In the future it is possible to also return an endpoint as:
	// <endpoint>/<instanceid>
	return "/" + srv.ID, nil
}

// InstanceTypeByProviderID returns the cloudprovider instance type of the node with the specified unique providerID
// This method will not be called from the node that is requesting this ID. i.e. metadata service
// and other local methods cannot be used here
func (i *Instances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	instanceID, err := instanceIDFromProviderID(providerID)

	if err != nil {
		return "", err
	}

	server, err := servers.Get(i.compute, instanceID).Extract()

	if err != nil {
		return "", err
	}

	return srvInstanceType(server)
}

// InstanceType returns the type of the specified instance.
func (i *Instances) InstanceType(ctx context.Context, name types.NodeName) (string, error) {
	srv, err := getServerByName(i.compute, name)

	if err != nil {
		return "", err
	}

	return srvInstanceType(&srv.Server)
}

func srvInstanceType(srv *servers.Server) (string, error) {
	keys := []string{"name", "id", "original_name"}
	for _, key := range keys {
		val, found := srv.Flavor[key]
		if found {
			flavor, ok := val.(string)
			if ok {
				return flavor, nil
			}
		}
	}
	return "", fmt.Errorf("flavor name/id not found")
}

func getAddressesByName(client *gophercloud.ServiceClient, name types.NodeName, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	srv, err := getServerByName(client, name)
	if err != nil {
		return nil, err
	}

	interfaces, err := getAttachedInterfacesByID(client, srv.ID)
	if err != nil {
		return nil, err
	}

	return nodeAddresses(&srv.Server, interfaces, networkingOpts)
}

func getServerByName(client *gophercloud.ServiceClient, name types.NodeName) (*ServerAttributesExt, error) {
	opts := servers.ListOpts{
		Name: fmt.Sprintf("^%s$", regexp.QuoteMeta(mapNodeNameToServerName(name))),
	}

	pager := servers.List(client, opts)

	var s []ServerAttributesExt
	serverList := make([]ServerAttributesExt, 0, 1)

	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		if err := servers.ExtractServersInto(page, &s); err != nil {
			return false, err
		}
		serverList = append(serverList, s...)
		if len(serverList) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	if len(serverList) == 0 {
		return nil, ErrNotFound
	}

	return &serverList[0], nil
}

// mapNodeNameToServerName maps a k8s NodeName to an OpenStack Server Name
// This is a simple string cast.
func mapNodeNameToServerName(nodeName types.NodeName) string {
	return string(nodeName)
}

// If Instances.InstanceID or cloudprovider.GetInstanceProviderID is changed, the regexp should be changed too.
var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `:///([^/]+)$`)

// instanceIDFromProviderID splits a provider's id and return instanceID.
// A providerID is build out of '${ProviderName}:///${instance-id}'which contains ':///'.
// See cloudprovider.GetInstanceProviderID and Instances.InstanceID.
func instanceIDFromProviderID(providerID string) (instanceID string, err error) {

	// https://github.com/kubernetes/kubernetes/issues/85731
	if providerID != "" && !strings.Contains(providerID, "://") {
		providerID = ProviderName + "://" + providerID
	}

	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 2 {
		return "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"kinx:///InstanceID\"", providerID)
	}
	return matches[1], nil
}

// IP addresses order:
// * interfaces private IPs
// * access IPs
// * metadata hostname
// * server object Addresses (floating type)
func nodeAddresses(srv *servers.Server, interfaces []attachinterfaces.Interface, networkingOpts NetworkingOpts) ([]v1.NodeAddress, error) {
	addrs := []v1.NodeAddress{}

	// parse private IP addresses first in an ordered manner
	for _, iface := range interfaces {
		for _, fixedIP := range iface.FixedIPs {
			if iface.PortState == "ACTIVE" {
				isIPv6 := net.ParseIP(fixedIP.IPAddress).To4() == nil
				if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
					AddToNodeAddresses(&addrs,
						v1.NodeAddress{
							Type:    v1.NodeInternalIP,
							Address: fixedIP.IPAddress,
						},
					)
				}
			}
		}
	}

	// process public IP addresses
	if srv.AccessIPv4 != "" {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv4,
			},
		)
	}

	if srv.AccessIPv6 != "" && !networkingOpts.IPv6SupportDisabled {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeExternalIP,
				Address: srv.AccessIPv6,
			},
		)
	}

	if srv.Metadata[TypeHostName] != "" {
		AddToNodeAddresses(&addrs,
			v1.NodeAddress{
				Type:    v1.NodeHostName,
				Address: srv.Metadata[TypeHostName],
			},
		)
	}

	// process the rest
	type Address struct {
		IPType string `mapstructure:"OS-EXT-IPS:type"`
		Addr   string
	}

	var addresses map[string][]Address
	err := mapstructure.Decode(srv.Addresses, &addresses)
	if err != nil {
		return nil, err
	}

	var networks []string
	for k := range addresses {
		networks = append(networks, k)
	}
	sort.Strings(networks)

	for _, network := range networks {
		for _, props := range addresses[network] {
			var addressType v1.NodeAddressType
			if props.IPType == "floating" {
				addressType = v1.NodeExternalIP
			} else if util.Contains(networkingOpts.PublicNetworkName, network) {
				addressType = v1.NodeExternalIP
				// removing already added address to avoid listing it as both ExternalIP and InternalIP
				// may happen due to listing "private" network as "public" in CCM's config
				RemoveFromNodeAddresses(&addrs,
					v1.NodeAddress{
						Address: props.Addr,
					},
				)
			} else {
				if len(networkingOpts.InternalNetworkName) == 0 || util.Contains(networkingOpts.InternalNetworkName, network) {
					addressType = v1.NodeInternalIP
				} else {
					klog.V(5).Infof("Node '%s' address '%s' ignored due to 'internal-network-name' option", srv.Name, props.Addr)
					RemoveFromNodeAddresses(&addrs,
						v1.NodeAddress{
							Address: props.Addr,
						},
					)
					continue
				}
			}

			isIPv6 := net.ParseIP(props.Addr).To4() == nil
			if !(isIPv6 && networkingOpts.IPv6SupportDisabled) {
				AddToNodeAddresses(&addrs,
					v1.NodeAddress{
						Type:    addressType,
						Address: props.Addr,
					},
				)
			}
		}
	}

	return addrs, nil
}

// AddToNodeAddresses appends the NodeAddresses to the passed-by-pointer slice,
// only if they do not already exist
func AddToNodeAddresses(addresses *[]v1.NodeAddress, addAddresses ...v1.NodeAddress) {
	for _, add := range addAddresses {
		exists := false
		for _, existing := range *addresses {
			if existing.Address == add.Address && existing.Type == add.Type {
				exists = true
				break
			}
		}
		if !exists {
			*addresses = append(*addresses, add)
		}
	}
}

// RemoveFromNodeAddresses removes the NodeAddresses from the passed-by-pointer
// slice if they already exist.
func RemoveFromNodeAddresses(addresses *[]v1.NodeAddress, removeAddresses ...v1.NodeAddress) {
	var indexesToRemove []int
	for _, remove := range removeAddresses {
		for i := len(*addresses) - 1; i >= 0; i-- {
			existing := (*addresses)[i]
			if existing.Address == remove.Address && (existing.Type == remove.Type || remove.Type == "") {
				indexesToRemove = append(indexesToRemove, i)
			}
		}
	}
	for _, i := range indexesToRemove {
		if i < len(*addresses) {
			*addresses = append((*addresses)[:i], (*addresses)[i+1:]...)
		}
	}
}
