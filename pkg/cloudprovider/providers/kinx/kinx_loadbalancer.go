package kinx

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	"github.com/gophercloud/gophercloud/openstack/keymanager/v1/containers"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/loadbalancers"
	v2monitors "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/monitors"
	v2pools "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/pagination"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"

	cpoerrors "github.com/kinxnet/cloud-provider-kinx/pkg/util/errors"
	netsets "k8s.io/cloud-provider-openstack/pkg/util/net/sets"
	openstackutil "k8s.io/cloud-provider-openstack/pkg/util/openstack"
)

// Note: when creating a new Loadbalancer (VM), it can take some time before it is ready for use,
// this timeout is used for waiting until the Loadbalancer provisioning status goes to ACTIVE state.
const (
	defaultLoadBalancerSourceRanges = "0.0.0.0/0"
	// loadbalancerActive* is configuration of exponential backoff for
	// going into ACTIVE loadbalancer provisioning status. Starting with 1
	// seconds, multiplying by 1.2 with each step and taking 19 steps at maximum
	// it will time out after 128s, which roughly corresponds to 120s
	loadbalancerActiveInitDelay = 1 * time.Second
	loadbalancerActiveFactor    = 1.2
	loadbalancerActiveSteps     = 57

	// loadbalancerDelete* is configuration of exponential backoff for
	// waiting for delete operation to complete. Starting with 1
	// seconds, multiplying by 1.2 with each step and taking 13 steps at maximum
	// it will time out after 32s, which roughly corresponds to 30s
	loadbalancerDeleteInitDelay = 1 * time.Second
	loadbalancerDeleteFactor    = 1.2
	loadbalancerDeleteSteps     = 13

	activeStatus = "ACTIVE"
	errorStatus  = "ERROR"

	backendProtocolTerminatedHttps = "terminated_https"
	backendProtocolHttp            = "http"
	backendProtocolTcp             = "tcp"

	// annotation
	ServiceAnnotationBackendProtocol = "service.beta.kubernetes.io/kinx-load-balancer-backend-protocol"
	ServiceAnnotationTlsContainerIds = "service.beta.kubernetes.io/kinx-load-balancer-tls-container-ids"
	ServiceAnnotationRedirectHttp    = "service.beta.kubernetes.io/kinx-load-balancer-redirect-http"
	ServiceAnnotationProxyProtocol   = "service.beta.kubernetes.io/kinx-load-balancer-proxy-protocol"
	ServiceAnnotationLowTlsv         = "service.beta.kubernetes.io/kinx-load-balancer-low-tlsv"

	// health monitor annotation
	ServiceAnnotationHealthCheckInterval = "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-interval"
	ServiceAnnotationHealthCheckRetry    = "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-retry"
	ServiceAnnotationHealthCheckTimeout  = "service.beta.kubernetes.io/kinx-load-balancer-healthcheck-timeout"
)

// LbaasV2 is a LoadBalancer implementation for Neutron LBaaS v2 API
type LBaasV2 struct {
	LoadBalancer
}

func (k *Kinx) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	klog.V(4).Info("openstack.LoadBalancer() called")

	secret, err := k.NewKeyManagerV1()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack KeyManager client: %v", err)
		return nil, false
	}

	network, err := k.NewNetworkV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	compute, err := k.NewComputeV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Compute client: %v", err)
		return nil, false
	}

	lb, err := k.NewLoadBalancerV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack LoadBalancer client: %v", err)
		return nil, false
	}

	// LBaaS v1 is deprecated in the OpenStack Liberty release.
	// Currently kubernetes OpenStack cloud provider just support LBaaS v2.
	lbVersion := k.lbOpts.LBVersion
	if lbVersion != "" && lbVersion != "v2" {
		klog.Warningf("Config error: currently only support LBaaS v2, unrecognised lb-version \"%v\"", lbVersion)
		return nil, false
	}

	klog.V(1).Info("Claiming to support LoadBalancer")

	return &LBaasV2{LoadBalancer{secret, network, compute, lb, k.lbOpts}}, true
}

// GetLoadBalancer returns whether the specified load balancer exists and its status
func (lbaas *LBaasV2) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (*corev1.LoadBalancerStatus, bool, error) {
	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err == ErrNotFound {
		return nil, false, nil
	}
	if loadbalancer == nil {
		return nil, false, err
	}

	status := &corev1.LoadBalancerStatus{}

	portID := loadbalancer.VipPortID
	if portID != "" {
		floatIP, err := openstackutil.GetFloatingIPByPortID(lbaas.network, portID)
		if err != nil {
			return nil, false, fmt.Errorf("failed when trying to get floating IP for port %s: %v", portID, err)
		}
		if floatIP != nil {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: floatIP.FloatingIP}}
		} else {
			status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
		}
	}

	return status, true, nil
}

// GetLoadBalancerName returns the constructed load balancer name.
func (lbaas *LBaasV2) GetLoadBalancerName(ctx context.Context, clusterName string, service *corev1.Service) string {
	name := fmt.Sprintf("kube-service-%s-%s-%s", clusterName, service.Namespace, service.Name)
	return cutString(name)
}

// GetLoadBalancerLegacyName returns the legacy load balancer name for backward compatibility.
func (lbaas *LBaasV2) GetLoadBalancerLegacyName(ctx context.Context, clusterName string, service *corev1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(service)
}

// cutString makes sure the string length doesn't exceed 255, which is usually the maximum string length in OpenStack.
func cutString(original string) string {
	ret := original
	if len(original) > 255 {
		ret = original[:255]
	}
	return ret
}

func (lbaas *LBaasV2) createLoadBalancer(service *corev1.Service, name, clusterName string, lbClass *LBClass, internalAnnotation bool, vipPort string) (*loadbalancers.LoadBalancer, error) {
	createOpts := loadbalancers.CreateOpts{
		Name:        name,
		Description: fmt.Sprintf("Kubernetes external service %s/%s from cluster %s", service.Namespace, service.Name, clusterName),
		Provider:    lbaas.opts.LBProvider,
	}

	if vipPort != "" {
		createOpts.VipPortID = vipPort
	} else {
		if lbClass != nil && lbClass.SubnetID != "" {
			createOpts.VipSubnetID = lbClass.SubnetID
		} else {
			createOpts.VipSubnetID = lbaas.opts.SubnetID
		}

		if lbClass != nil && lbClass.NetworkID != "" {
			createOpts.VipNetworkID = lbClass.NetworkID
		} else if lbaas.opts.NetworkID != "" {
			createOpts.VipNetworkID = lbaas.opts.NetworkID
		} else {
			klog.V(4).Infof("network-id parameter not passed, it will be inferred from subnet-id")
		}
	}

	loadBalancerIP := service.Spec.LoadBalancerIP
	if loadBalancerIP != "" && internalAnnotation {
		createOpts.VipAddress = loadBalancerIP
	}

	loadbalancer, err := loadbalancers.Create(lbaas.lb, createOpts).Extract()
	if err != nil {
		return nil, fmt.Errorf("error creating loadbalancer %v: %v", createOpts, err)
	}

	// when NetworkID is specified, subnet will be selected by the backend for allocating virtual IP
	if (lbClass != nil && lbClass.NetworkID != "") || lbaas.opts.NetworkID != "" {
		lbaas.opts.SubnetID = loadbalancer.VipSubnetID
	}

	return loadbalancer, nil
}

// getFloatingNetworkIDForLB returns a floating-network-id for cluster.
func getFloatingNetworkIDForLB(client *gophercloud.ServiceClient) (string, error) {
	var floatingNetworkIds []string

	type NetworkWithExternalExt struct {
		networks.Network
		external.NetworkExternalExt
	}

	err := networks.List(client, networks.ListOpts{}).EachPage(func(page pagination.Page) (bool, error) {
		var externalNetwork []NetworkWithExternalExt
		err := networks.ExtractNetworksInto(page, &externalNetwork)
		if err != nil {
			return false, err
		}

		for _, externalNet := range externalNetwork {
			if externalNet.External {
				floatingNetworkIds = append(floatingNetworkIds, externalNet.ID)
			}
		}

		if len(floatingNetworkIds) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return "", ErrNotFound
		}

		if err == ErrMultipleResults {
			klog.V(4).Infof("find multiple external networks, pick the first one when there are no explicit configuration.")
			return floatingNetworkIds[0], nil
		}
		return "", err
	}

	if len(floatingNetworkIds) == 0 {
		return "", ErrNotFound
	}

	return floatingNetworkIds[0], nil
}

func waitLoadbalancerActiveProvisioningStatus(client *gophercloud.ServiceClient, loadbalancerID string) (string, error) {
	backoff := wait.Backoff{
		Duration: loadbalancerActiveInitDelay,
		Factor:   loadbalancerActiveFactor,
		Steps:    loadbalancerActiveSteps,
	}

	var provisioningStatus string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		loadbalancer, err := loadbalancers.Get(client, loadbalancerID).Extract()
		if err != nil {
			return false, err
		}
		provisioningStatus = loadbalancer.ProvisioningStatus
		if loadbalancer.ProvisioningStatus == activeStatus {
			return true, nil
		} else if loadbalancer.ProvisioningStatus == errorStatus {
			return true, fmt.Errorf("loadbalancer has gone into ERROR state")
		} else {
			return false, nil
		}

	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("loadbalancer failed to go into ACTIVE provisioning status within allotted time")
	}
	return provisioningStatus, err
}

// EnsureLoadBalancer creates a new load balancer or updates the existing one.
func (lbaas *LBaasV2) EnsureLoadBalancer(ctx context.Context, clusterName string, apiService *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	serviceName := fmt.Sprintf("%s/%s", apiService.Namespace, apiService.Name)

	klog.V(4).Infof("EnsureLoadBalancer(%s, %s)", clusterName, serviceName)

	backendProtocol := getStringFromServiceAnnotation(apiService, ServiceAnnotationBackendProtocol, "")
	if !isBackendProtocol(backendProtocol) {
		return nil, fmt.Errorf("\"%s\" is an unsupported protocol", backendProtocol)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("there are no available nodes for LoadBalancer service %s", serviceName)
	}

	// Get SubnetID automatically.
	// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
	subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0])
	if err != nil {
		klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", apiService.Namespace, apiService.Name, err)
		return nil, fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
			"and failed to find subnet-id from OpenStack: %v", apiService.Namespace, apiService.Name, err)
	}
	lbaas.opts.SubnetID = subnetID

	ports := apiService.Spec.Ports
	if len(ports) == 0 {
		return nil, fmt.Errorf("no ports provided to openstack load balancer")
	}

	klog.V(4).Infof("Ensure an external loadbalancer service")

	tlsContainerRef := ""
	sniContainerRefs := []string{}
	tlsContainerString := getStringFromServiceAnnotation(apiService, ServiceAnnotationTlsContainerIds, "")
	if tlsContainerString != "" {
		if lbaas.secret == nil {
			return nil, fmt.Errorf("failed to create a TLS Terminated loadbalancer because openstack keymanager client is not "+
				"initialized and default-tls-container-ref %q is set", tlsContainerString)
		}

		tlsContainerIds := regexp.MustCompile("[\\s]*,[\\s]*").Split(tlsContainerString, -1)
		if len(tlsContainerIds) > 0 {
			for i := 0; i < len(tlsContainerIds); i++ {
				container, err := containers.Get(lbaas.secret, tlsContainerIds[i]).Extract()
				if err != nil {
					return nil, fmt.Errorf("failed to get tls container %q: %v", tlsContainerIds[i], err)
				}

				klog.Infof("TLS container %q found", container.ContainerRef)

				tlsContainerIds[i] = container.ContainerRef
			}

			tlsContainerRef = tlsContainerIds[0]
			sniContainerRefs = tlsContainerIds[1:]
		}
	}

	// TODO Support for ManageSecurityGroups

	affinity := apiService.Spec.SessionAffinity
	var persistence *v2pools.SessionPersistence
	switch affinity {
	case corev1.ServiceAffinityNone:
		persistence = nil
	case corev1.ServiceAffinityClientIP:
		persistence = &v2pools.SessionPersistence{Type: "SOURCE_IP"}
	default:
		return nil, fmt.Errorf("unsupported load balancer affinity: %v", affinity)
	}

	// Use more meaningful name for the load balancer but still need to check the legacy name for backward compatibility.
	name := lbaas.GetLoadBalancerName(ctx, clusterName, apiService)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, apiService)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		if err != ErrNotFound {
			return nil, fmt.Errorf("error getting loadbalancer for Service %s: %v", serviceName, err)
		}

		klog.V(2).Infof("Creating loadbalancer %s", name)

		loadbalancer, err = lbaas.createLoadBalancer(apiService, name, clusterName, nil /* lbClass */, false /* internal */, "" /* portID */)
		if err != nil {
			return nil, fmt.Errorf("error creating loadbalancer %s: %v", name, err)
		}
	} else {
		klog.V(2).Infof("LoadBalancer %s already exists", loadbalancer.Name)
	}

	provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE, current provisioning status %s", provisioningStatus)
	}

	loadbalancer, err = loadbalancers.Get(lbaas.lb, loadbalancer.ID).Extract()
	if err != nil {
		return nil, fmt.Errorf("could not get loadbalancer object")
	}

	oldListeners, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return nil, fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.Name, err)
	}

	for portIndex, port := range ports {
		listener := getListenerForPort(oldListeners, port)
		connLimit := -1

		// get listener annotation
		redirectHttp, _ := getBoolFromServiceAnnotation(apiService, ServiceAnnotationRedirectHttp, false)
		lowTlsv, _ := getBoolFromServiceAnnotation(apiService, ServiceAnnotationLowTlsv, false)

		listenerProtocol := getListenerProtocol(backendProtocol)
		listenerDescription := getListenerDescription(listenerProtocol, redirectHttp, lowTlsv)
		if listener == nil {
			listenerCreateOpt := listeners.CreateOpts{
				Name:                   cutString(fmt.Sprintf("listener-%d-%s", portIndex, name)),
				Description:            listenerDescription,
				Protocol:               listenerProtocol,
				ProtocolPort:           int(port.Port),
				ConnLimit:              &connLimit,
				LoadbalancerID:         loadbalancer.ID,
				DefaultTlsContainerRef: tlsContainerRef,
				SniContainerRefs:       sniContainerRefs,
			}

			if tlsContainerRef != "" && listenerCreateOpt.Protocol != listeners.ProtocolTerminatedHTTPS {
				klog.Infof("Forcing to use %q protocol for listener because %q annotation is set", listeners.ProtocolTerminatedHTTPS, ServiceAnnotationTlsContainerIds)
				listenerCreateOpt.Protocol = listeners.ProtocolTerminatedHTTPS
			}

			klog.V(4).Infof("Creating listener for port %d using protocol: %s", int(port.Port), listenerProtocol)

			listener, err = openstackutil.CreateListener(lbaas.lb, loadbalancer.ID, listenerCreateOpt)
			if err != nil {
				return nil, fmt.Errorf("failed to create listener for loadbalancer %s: %v", loadbalancer.ID, err)
			}

			klog.V(4).Infof("Listener %s created for loadbalancer %s", listener.ID, loadbalancer.ID)
		} else {
			listenerChanged := false
			updateOpts := listeners.UpdateOpts{}

			if tlsContainerRef != listener.DefaultTlsContainerRef {
				klog.Infof("change tls-container-ref (%s) -> (%s)", listener.DefaultTlsContainerRef, tlsContainerRef)
				updateOpts.DefaultTlsContainerRef = &tlsContainerRef
				listenerChanged = true
			}

			if !reflect.DeepEqual(sniContainerRefs, listener.SniContainerRefs) {
				klog.Infof("change sni-container-ref (%s) -> (%s)", listener.SniContainerRefs, sniContainerRefs)
				updateOpts.SniContainerRefs = &sniContainerRefs
				listenerChanged = true
			}

			if listener.Description != listenerDescription {
				updateOpts.Description = &listenerDescription
				listenerChanged = true
			}

			if listenerChanged {
				if err := openstackutil.UpdateListener(lbaas.lb, loadbalancer.ID, listener.ID, updateOpts); err != nil {
					return nil, fmt.Errorf("failed to update listener %s of loadbalancer %s: %v", listener.ID, loadbalancer.ID, err)
				}

				klog.V(4).Infof("Listener %s updated for loadbalancer %s", listener.ID, loadbalancer.ID)
			}
		}

		// After all ports have been processed, remaining listeners are removed as obsolete.
		// Pop valid listeners.
		oldListeners = popListener(oldListeners, listener.ID)

		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
		}

		if pool == nil {
			poolProto := getPoolProtocol(backendProtocol)
			lbmethod := v2pools.LBMethod(lbaas.opts.LBMethod)
			createOpt := v2pools.CreateOpts{
				Name:        cutString(fmt.Sprintf("pool-%d-%s", portIndex, name)),
				Protocol:    poolProto,
				LBMethod:    lbmethod,
				ListenerID:  listener.ID,
				Persistence: persistence,
			}

			klog.V(4).Infof("Creating pool for listener %s using protocol %s", listener.ID, poolProto)

			pool, err = v2pools.Create(lbaas.lb, createOpt).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating pool for listener %s: %v", listener.ID, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating pool, current provisioning status %s", provisioningStatus)
			}

			klog.V(4).Infof("Pool created for listener %s: %s", listener.ID, pool.ID)

		}

		proxyProtocol, _ := getBoolFromServiceAnnotation(apiService, ServiceAnnotationProxyProtocol, false)
		poolDescription := getPoolDescription(v2pools.Protocol(pool.Protocol), pool.ID, proxyProtocol)

		if pool.Description != poolDescription {
			pool, err = v2pools.Update(lbaas.lb, pool.ID, v2pools.UpdateOpts{Description: &poolDescription}).Extract()
			if err != nil {
				return nil, fmt.Errorf("failed to update pool %s of listener %s: %v", pool.ID, listener.ID, err)
			}

			klog.V(4).Infof("Pool updated for Listener %s: %s", listener.ID, pool.ID)
		}

		members, err := getMembersByPoolID(lbaas.lb, pool.ID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		for _, node := range nodes {
			addr, err := nodeAddressForLB(node)
			if err != nil {
				if err == ErrNotFound {
					// Node failure, do not create member
					klog.Warningf("Failed to create LB pool member for node %s: %v", node.Name, err)
					continue
				} else {
					return nil, fmt.Errorf("error getting address for node %s: %v", node.Name, err)
				}
			}

			if !memberExists(members, addr, int(port.NodePort)) {
				klog.V(4).Infof("Creating member for pool %s", pool.ID)
				_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
					Name:         cutString(fmt.Sprintf("member-%d-%s-%s", portIndex, node.Name, name)),
					ProtocolPort: int(port.NodePort),
					Address:      addr,
					SubnetID:     lbaas.opts.SubnetID,
				}).Extract()
				if err != nil {
					return nil, fmt.Errorf("error creating LB pool member for node: %s, %v", node.Name, err)
				}

				provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
				if err != nil {
					return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating member, current provisioning status %s", provisioningStatus)
				}
			} else {
				// After all members have been processed, remaining members are deleted as obsolete.
				members = popMember(members, addr, int(port.NodePort))
			}

			klog.V(4).Infof("Ensured pool %s has member for %s at %s", pool.ID, node.Name, addr)
		}

		// Delete obsolete members for this pool
		for _, member := range members {
			klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
			err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
			}
		}

		monitorID := pool.MonitorID
		if monitorID == "" && lbaas.opts.CreateMonitor {
			klog.V(4).Infof("Creating monitor for pool %s", pool.ID)
			monitorProtocol := string(port.Protocol)
			if port.Protocol == corev1.ProtocolUDP {
				monitorProtocol = "UDP-CONNECT"
			}

			monitorInterval, err := getIntFromServiceAnnotation(apiService, ServiceAnnotationHealthCheckInterval, lbaas.opts.MonitorDelay)
			if err != nil {
				return nil, err
			}

			monitorTimeout, err := getIntFromServiceAnnotation(apiService, ServiceAnnotationHealthCheckTimeout, lbaas.opts.MonitorTimeout)
			if err != nil {
				return nil, err
			}

			monitorRetry, err := getIntFromServiceAnnotation(apiService, ServiceAnnotationHealthCheckRetry, int(lbaas.opts.MonitorMaxRetries))
			if err != nil {
				return nil, err
			}

			monitor, err := v2monitors.Create(lbaas.lb, v2monitors.CreateOpts{
				Name:       cutString(fmt.Sprintf("monitor-%d-%s", portIndex, name)),
				PoolID:     pool.ID,
				Type:       monitorProtocol,
				Delay:      monitorInterval,
				Timeout:    monitorTimeout,
				MaxRetries: int(monitorRetry),
			}).Extract()
			if err != nil {
				return nil, fmt.Errorf("error creating LB pool healthmonitor: %v", err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating monitor, current provisioning status %s", provisioningStatus)
			}
			monitorID = monitor.ID
		}
	}

	// All remaining listeners are obsolete, delete
	for _, listener := range oldListeners {
		klog.V(4).Infof("Deleting obsolete listener %s:", listener.ID)
		// get pool for listener
		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("error getting pool for obsolete listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			// get and delete monitor
			monitorID := pool.MonitorID
			if monitorID != "" {
				klog.V(4).Infof("Deleting obsolete monitor %s for pool %s", monitorID, pool.ID)
				err = v2monitors.Delete(lbaas.lb, monitorID).ExtractErr()
				if err != nil && !cpoerrors.IsNotFound(err) {
					return nil, fmt.Errorf("error deleting obsolete monitor %s for pool %s: %v", monitorID, pool.ID, err)
				}
				provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
				if err != nil {
					return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting monitor, current provisioning status %s", provisioningStatus)
				}
			}
			// get and delete pool members
			members, err := getMembersByPoolID(lbaas.lb, pool.ID)
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error getting members for pool %s: %v", pool.ID, err)
			}
			if members != nil {
				for _, member := range members {
					klog.V(4).Infof("Deleting obsolete member %s for pool %s address %s", member.ID, pool.ID, member.Address)
					err := v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
					if err != nil && !cpoerrors.IsNotFound(err) {
						return nil, fmt.Errorf("error deleting obsolete member %s for pool %s address %s: %v", member.ID, pool.ID, member.Address, err)
					}
					provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
					if err != nil {
						return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
					}
				}
			}
			klog.V(4).Infof("Deleting obsolete pool %s for listener %s", pool.ID, listener.ID)
			// delete pool
			err = v2pools.Delete(lbaas.lb, pool.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error deleting obsolete pool %s for listener %s: %v", pool.ID, listener.ID, err)
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting pool, current provisioning status %s", provisioningStatus)
			}
		}
		// delete listener
		err = listeners.Delete(lbaas.lb, listener.ID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return nil, fmt.Errorf("error deleteting obsolete listener: %v", err)
		}
		provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return nil, fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting listener, current provisioning status %s", provisioningStatus)
		}
		klog.V(2).Infof("Deleted obsolete listener: %s", listener.ID)
	}

	status := &corev1.LoadBalancerStatus{}
	status.Ingress = []corev1.LoadBalancerIngress{{IP: loadbalancer.VipAddress}}
	return status, nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (lbaas *LBaasV2) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(4).Infof("UpdateLoadBalancer(%v, %s, %v)", clusterName, serviceName, nodes)

	// Get SubnetID automatically.
	// The LB needs to be configured with instance addresses on the same subnet, so get SubnetID by one node.
	subnetID, err := getSubnetIDForLB(lbaas.compute, *nodes[0])
	if err != nil {
		klog.Warningf("Failed to find subnet-id for loadbalancer service %s/%s: %v", service.Namespace, service.Name, err)
		return fmt.Errorf("no subnet-id for service %s/%s : subnet-id not set in cloud provider config, "+
			"and failed to find subnet-id from OpenStack: %v", service.Namespace, service.Name, err)
	}
	lbaas.opts.SubnetID = subnetID

	ports := service.Spec.Ports
	if len(ports) == 0 {
		return fmt.Errorf("no ports provided to openstack load balancer")
	}

	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil {
		return err
	}
	if loadbalancer == nil {
		return fmt.Errorf("loadbalancer does not exist for Service %s", serviceName)
	}

	// Get all listeners for this loadbalancer, by "port key".
	type portKey struct {
		Port int
	}
	var listenerIDs []string
	lbListeners := make(map[portKey]listeners.Listener)
	allListeners, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("error getting listeners for LB %s: %v", loadbalancer.ID, err)
	}
	for _, l := range allListeners {
		key := portKey{Port: l.ProtocolPort}
		lbListeners[key] = l
		listenerIDs = append(listenerIDs, l.ID)
	}

	// Get all pools for this loadbalancer, by listener ID.
	lbPools := make(map[string]v2pools.Pool)
	for _, listenerID := range listenerIDs {
		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listenerID)
		if err != nil {
			return fmt.Errorf("error getting pool for listener %s: %v", listenerID, err)
		}
		lbPools[listenerID] = *pool
	}

	// Compose Set of member (addresses) that _should_ exist
	addrs := make(map[string]*corev1.Node)
	for _, node := range nodes {
		addr, err := nodeAddressForLB(node)
		if err != nil {
			return err
		}
		addrs[addr] = node
	}

	// Check for adding/removing members associated with each port
	for portIndex, port := range ports {
		// Get listener associated with this port
		listener, ok := lbListeners[portKey{
			Port: int(port.Port),
		}]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required listener for port %d", loadbalancer.ID, port.Port)
		}

		// Get pool associated with this listener
		pool, ok := lbPools[listener.ID]
		if !ok {
			return fmt.Errorf("loadbalancer %s does not contain required pool for listener %s", loadbalancer.ID, listener.ID)
		}

		// Find existing pool members (by address) for this port
		getMembers, err := getMembersByPoolID(lbaas.lb, pool.ID)
		if err != nil {
			return fmt.Errorf("error getting pool members %s: %v", pool.ID, err)
		}
		members := make(map[string]v2pools.Member)
		for _, member := range getMembers {
			members[member.Address] = member
		}

		// Add any new members for this port
		for addr, node := range addrs {
			if _, ok := members[addr]; ok && members[addr].ProtocolPort == int(port.NodePort) {
				// Already exists, do not create member
				continue
			}
			_, err := v2pools.CreateMember(lbaas.lb, pool.ID, v2pools.CreateMemberOpts{
				Name:         cutString(fmt.Sprintf("member-%d-%s-%s", portIndex, node.Name, loadbalancer.Name)),
				Address:      addr,
				ProtocolPort: int(port.NodePort),
				SubnetID:     lbaas.opts.SubnetID,
			}).Extract()
			if err != nil {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after creating member, current provisioning status %s", provisioningStatus)
			}
		}

		// Remove any old members for this port
		for _, member := range members {
			if _, ok := addrs[member.Address]; ok && member.ProtocolPort == int(port.NodePort) {
				// Still present, do not delete member
				continue
			}
			err = v2pools.DeleteMember(lbaas.lb, pool.ID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
			}
		}
	}

	return nil
}

func waitLoadbalancerDeleted(client *gophercloud.ServiceClient, loadbalancerID string) error {
	backoff := wait.Backoff{
		Duration: loadbalancerDeleteInitDelay,
		Factor:   loadbalancerDeleteFactor,
		Steps:    loadbalancerDeleteSteps,
	}
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		_, err := loadbalancers.Get(client, loadbalancerID).Extract()
		if err != nil {
			if cpoerrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("loadbalancer failed to delete within the allotted time")
	}

	return err
}

func isBackendProtocol(protocol string) bool {
	switch strings.ToLower(protocol) {
	case backendProtocolTerminatedHttps, backendProtocolHttp, backendProtocolTcp:
		return true
	}

	return false
}

func getListenerProtocol(backendProtocol string) listeners.Protocol {
	switch backendProtocol {
	case backendProtocolTerminatedHttps:
		return listeners.ProtocolTerminatedHTTPS
	case backendProtocolHttp:
		return listeners.ProtocolHTTP
	case backendProtocolTcp:
		return listeners.ProtocolTCP
	}
	return ""
}

func getPoolProtocol(backendProtocol string) v2pools.Protocol {
	switch backendProtocol {
	case backendProtocolTerminatedHttps:
		return v2pools.ProtocolHTTP
	case backendProtocolHttp:
		return v2pools.ProtocolHTTP
	case backendProtocolTcp:
		return v2pools.ProtocolTCP
	}
	return ""
}

func getListenerDescription(protocol listeners.Protocol, redirectHttp bool, lowTlsv bool) string {
	switch protocol {
	case listeners.ProtocolHTTP:
		if redirectHttp {
			return "forward:set-header x-forwarded-proto http,redirect_http:True"
		}

		return "forward:set-header x-forwarded-proto http"

	case listeners.ProtocolHTTPS:
		if lowTlsv {
			return "forward:set-header x-forwarded-proto https,tlsv:\"no-tlsv10 no-tlsv11\""
		}

		return "forward:set-header x-forwarded-proto https"

	default:
		return ""
	}
}

func getPoolDescription(protocol v2pools.Protocol, poolId string, proxyProtocol bool) string {
	switch protocol {
	case v2pools.ProtocolTCP:
		if proxyProtocol {
			return fmt.Sprintf("%s,proxy:true", poolId)
		}
		return ""
	default:
		return ""
	}
}

// EnsureLoadBalancerDeleted deletes the specified load balancer
func (lbaas *LBaasV2) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	serviceName := fmt.Sprintf("%s/%s", service.Namespace, service.Name)
	klog.V(4).Infof("EnsureLoadBalancerDeleted(%s, %s)", clusterName, serviceName)

	name := lbaas.GetLoadBalancerName(ctx, clusterName, service)
	legacyName := lbaas.GetLoadBalancerLegacyName(ctx, clusterName, service)
	loadbalancer, err := getLoadbalancerByName(lbaas.lb, name, legacyName)
	if err != nil && err != ErrNotFound {
		return err
	}
	if loadbalancer == nil {
		return nil
	}

	// get all listeners associated with this loadbalancer
	listenerList, err := getListenersByLoadBalancerID(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("error getting LB %s listeners: %v", loadbalancer.ID, err)
	}

	// get all pools (and health monitors) associated with this loadbalancer
	var poolIDs []string
	var monitorIDs []string
	for _, listener := range listenerList {
		pool, err := getPoolByListenerID(lbaas.lb, loadbalancer.ID, listener.ID)
		if err != nil && err != ErrNotFound {
			return fmt.Errorf("error getting pool for listener %s: %v", listener.ID, err)
		}
		if pool != nil {
			poolIDs = append(poolIDs, pool.ID)
			// If create-monitor of cloud-config is false, pool has not monitor.
			if pool.MonitorID != "" {
				monitorIDs = append(monitorIDs, pool.MonitorID)
			}
		}
	}

	// delete all monitors
	for _, monitorID := range monitorIDs {
		err := v2monitors.Delete(lbaas.lb, monitorID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return err
		}
		provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting monitor, current provisioning status %s", provisioningStatus)
		}
	}

	// delete all members and pools
	for _, poolID := range poolIDs {
		// get members for current pool
		membersList, err := getMembersByPoolID(lbaas.lb, poolID)
		if err != nil && !cpoerrors.IsNotFound(err) {
			return fmt.Errorf("error getting pool members %s: %v", poolID, err)
		}
		// delete all members for this pool
		for _, member := range membersList {
			err := v2pools.DeleteMember(lbaas.lb, poolID, member.ID).ExtractErr()
			if err != nil && !cpoerrors.IsNotFound(err) {
				return err
			}
			provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
			if err != nil {
				return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting member, current provisioning status %s", provisioningStatus)
			}
		}

		// delete pool
		err = v2pools.Delete(lbaas.lb, poolID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return err
		}
		provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting pool, current provisioning status %s", provisioningStatus)
		}
	}

	// delete all listeners
	for _, listener := range listenerList {
		err := listeners.Delete(lbaas.lb, listener.ID).ExtractErr()
		if err != nil && !cpoerrors.IsNotFound(err) {
			return err
		}
		provisioningStatus, err := waitLoadbalancerActiveProvisioningStatus(lbaas.lb, loadbalancer.ID)
		if err != nil {
			return fmt.Errorf("timeout when waiting for loadbalancer to be ACTIVE after deleting listener, current provisioning status %s", provisioningStatus)
		}
	}

	err = loadbalancers.Delete(lbaas.lb, loadbalancer.ID, loadbalancers.DeleteOpts{}).ExtractErr()
	if err != nil && !cpoerrors.IsNotFound(err) {
		return err
	}
	err = waitLoadbalancerDeleted(lbaas.lb, loadbalancer.ID)
	if err != nil {
		return fmt.Errorf("failed to delete loadbalancer: %v", err)
	}

	return nil
}

func (lbaas *LBaasV2) getSubnet(subnet string) (*subnets.Subnet, error) {
	if subnet == "" {
		return nil, nil
	}

	allPages, err := subnets.List(lbaas.network, subnets.ListOpts{Name: subnet}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("error listing subnets: %v", err)
	}
	subs, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting subnets from pages: %v", err)
	}

	if len(subs) == 0 {
		return nil, fmt.Errorf("could not find subnet %s", subnet)
	}
	if len(subs) == 1 {
		return &subs[0], nil
	}
	return nil, fmt.Errorf("find multiple subnets with name %s", subnet)
}

//getStringFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getStringFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting string) string {
	klog.V(4).Infof("getStringFromServiceAnnotation(%v, %v, %v)", service, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		//if there is an annotation for this setting, set the "setting" var to it
		// annotationValue can be empty, it is working as designed
		// it makes possible for instance provisioning loadbalancer without floatingip
		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)
		return annotationValue
	}
	//if there is no annotation, set "settings" var to the value from cloud config
	klog.V(4).Infof("Could not find a Service Annotation; falling back on cloud-config setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting
}

//getIntFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getIntFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting int) (int, error) {
	klog.V(4).Infof("getIntFromServiceAnnotation(%v, %v, %v)", service, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		//if there is an annotation for this setting, set the "setting" var to it
		// annotationValue can be empty, it is working as designed
		// it makes possible for instance provisioning loadbalancer without floatingip
		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, annotationValue)

		tmp, err := strconv.Atoi(annotationValue)
		if err != nil {
			klog.V(4).Infof("Could not parse int value from \"%s\" error \"%v\" failing back to default", annotationValue, err)
			return math.MinInt, fmt.Errorf("Could not parse int value from \"%s\" error \"%v\" failing back to default", annotationValue, err)
		}

		return tmp, nil
	}

	//if there is no annotation, set "settings" var to the value from cloud config
	klog.V(4).Infof("Could not find a Service Annotation; falling back on cloud-config setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting, nil
}

//getBoolFromServiceAnnotation searches a given v1.Service for a specific annotationKey and either returns the annotation's value or a specified defaultSetting
func getBoolFromServiceAnnotation(service *corev1.Service, annotationKey string, defaultSetting bool) (bool, error) {
	klog.V(4).Infof("getBoolFromServiceAnnotation(%v, %v, %v)", service, annotationKey, defaultSetting)
	if annotationValue, ok := service.Annotations[annotationKey]; ok {
		returnValue := false
		switch annotationValue {
		case "true":
			returnValue = true
		case "false":
			returnValue = false
		default:
			return returnValue, fmt.Errorf("unknown %s annotation: %v, specify \"true\" or \"false\" ", annotationKey, annotationValue)
		}

		klog.V(4).Infof("Found a Service Annotation: %v = %v", annotationKey, returnValue)
		return returnValue, nil
	}
	klog.V(4).Infof("Could not find a Service Annotation; falling back to default setting: %v = %v", annotationKey, defaultSetting)
	return defaultSetting, nil
}

func toListenersProtocol(protocol corev1.Protocol) listeners.Protocol {
	switch protocol {
	case corev1.ProtocolTCP:
		return listeners.ProtocolTCP
	default:
		return listeners.Protocol(string(protocol))
	}
}

// Check if a member exists for node
func memberExists(members []v2pools.Member, addr string, port int) bool {
	for _, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			return true
		}
	}

	return false
}

func popListener(existingListeners []listeners.Listener, id string) []listeners.Listener {
	for i, existingListener := range existingListeners {
		if existingListener.ID == id {
			existingListeners[i] = existingListeners[len(existingListeners)-1]
			existingListeners = existingListeners[:len(existingListeners)-1]
			break
		}
	}

	return existingListeners
}

func popMember(members []v2pools.Member, addr string, port int) []v2pools.Member {
	for i, member := range members {
		if member.Address == addr && member.ProtocolPort == port {
			members[i] = members[len(members)-1]
			members = members[:len(members)-1]
		}
	}

	return members
}

// get listener for a port or nil if does not exist
func getListenerForPort(existingListeners []listeners.Listener, port corev1.ServicePort) *listeners.Listener {
	for _, l := range existingListeners {
		if l.ProtocolPort == int(port.Port) {
			return &l
		}
	}

	return nil
}

func getListenersByLoadBalancerID(client *gophercloud.ServiceClient, id string) ([]listeners.Listener, error) {
	var existingListeners []listeners.Listener
	err := listeners.List(client, listeners.ListOpts{LoadbalancerID: id}).EachPage(func(page pagination.Page) (bool, error) {
		listenerList, err := listeners.ExtractListeners(page)
		if err != nil {
			return false, err
		}
		for _, l := range listenerList {
			for _, lb := range l.Loadbalancers {
				if lb.ID == id {
					existingListeners = append(existingListeners, l)
					break
				}
			}
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return existingListeners, nil
}

// Get pool for a listener. A listener always has exactly one pool.
func getPoolByListenerID(client *gophercloud.ServiceClient, loadbalancerID string, listenerID string) (*v2pools.Pool, error) {
	listenerPools := make([]v2pools.Pool, 0, 1)
	err := v2pools.List(client, v2pools.ListOpts{LoadbalancerID: loadbalancerID}).EachPage(func(page pagination.Page) (bool, error) {
		poolsList, err := v2pools.ExtractPools(page)
		if err != nil {
			return false, err
		}
		for _, p := range poolsList {
			for _, l := range p.Listeners {
				if l.ID == listenerID {
					listenerPools = append(listenerPools, p)
				}
			}
		}
		if len(listenerPools) > 1 {
			return false, ErrMultipleResults
		}
		return true, nil
	})
	if err != nil {
		if cpoerrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(listenerPools) == 0 {
		return nil, ErrNotFound
	} else if len(listenerPools) > 1 {
		return nil, ErrMultipleResults
	}

	return &listenerPools[0], nil
}

func getMembersByPoolID(client *gophercloud.ServiceClient, id string) ([]v2pools.Member, error) {
	var members []v2pools.Member
	err := v2pools.ListMembers(client, id, v2pools.ListMembersOpts{}).EachPage(func(page pagination.Page) (bool, error) {
		membersList, err := v2pools.ExtractMembers(page)
		if err != nil {
			return false, err
		}
		members = append(members, membersList...)

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

// The LB needs to be configured with instance addresses on the same
// subnet as the LB (aka opts.SubnetID).  Currently we're just
// guessing that the node's InternalIP is the right address.
// In case no InternalIP can be found, ExternalIP is tried.
// If neither InternalIP nor ExternalIP can be found an error is
// returned.
func nodeAddressForLB(node *corev1.Node) (string, error) {
	addrs := node.Status.Addresses
	if len(addrs) == 0 {
		return "", ErrNoAddressFound
	}

	allowedAddrTypes := []corev1.NodeAddressType{corev1.NodeInternalIP, corev1.NodeExternalIP}

	for _, allowedAddrType := range allowedAddrTypes {
		for _, addr := range addrs {
			if addr.Type == allowedAddrType {
				return addr.Address, nil
			}
		}
	}

	return "", ErrNoAddressFound
}

// getAttachedInterfacesByID returns the node interfaces of the specified instance.
func getAttachedInterfacesByID(client *gophercloud.ServiceClient, serviceID string) ([]attachinterfaces.Interface, error) {
	var interfaces []attachinterfaces.Interface

	pager := attachinterfaces.List(client, serviceID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		s, err := attachinterfaces.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		interfaces = append(interfaces, s...)
		return true, nil
	})
	if err != nil {
		return interfaces, err
	}

	return interfaces, nil
}

// getSubnetIDForLB returns subnet-id for a specific node
func getSubnetIDForLB(compute *gophercloud.ServiceClient, node corev1.Node) (string, error) {
	ipAddress, err := nodeAddressForLB(&node)
	if err != nil {
		return "", err
	}

	instanceID := node.Spec.ProviderID
	if ind := strings.LastIndex(instanceID, "/"); ind >= 0 {
		instanceID = instanceID[(ind + 1):]
	}

	interfaces, err := getAttachedInterfacesByID(compute, instanceID)
	if err != nil {
		return "", err
	}

	for _, intf := range interfaces {
		for _, fixedIP := range intf.FixedIPs {
			if fixedIP.IPAddress == ipAddress {
				return fixedIP.SubnetID, nil
			}
		}
	}

	return "", ErrNotFound
}

func getLoadBalancers(client *gophercloud.ServiceClient, opts loadbalancers.ListOpts) ([]loadbalancers.LoadBalancer, error) {
	allPages, err := loadbalancers.List(client, opts).AllPages()
	if err != nil {
		return nil, err
	}
	allLoadbalancers, err := loadbalancers.ExtractLoadBalancers(allPages)
	if err != nil {
		return nil, err
	}

	return allLoadbalancers, nil
}

// getLoadbalancerByName get the load balancer which is in valid status by the given name/legacy name.
func getLoadbalancerByName(client *gophercloud.ServiceClient, name string, legacyName string) (*loadbalancers.LoadBalancer, error) {
	var validLBs []loadbalancers.LoadBalancer

	opts := loadbalancers.ListOpts{
		Name: name,
	}
	allLoadbalancers, err := getLoadBalancers(client, opts)
	if err != nil {
		return nil, err
	}

	if len(allLoadbalancers) == 0 {
		if len(legacyName) > 0 {
			// Backoff to get load balnacer by legacy name.
			opts := loadbalancers.ListOpts{
				Name: legacyName,
			}
			allLoadbalancers, err = getLoadBalancers(client, opts)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, ErrNotFound
		}
	}

	for _, lb := range allLoadbalancers {
		// All the ProvisioningStatus could be found here https://developer.openstack.org/api-ref/load-balancer/v2/index.html#provisioning-status-codes
		if lb.ProvisioningStatus != "DELETED" && lb.ProvisioningStatus != "PENDING_DELETE" {
			validLBs = append(validLBs, lb)
		}
	}

	if len(validLBs) > 1 {
		return nil, ErrMultipleResults
	}
	if len(validLBs) == 0 {
		return nil, ErrNotFound
	}

	return &validLBs[0], nil
}

// IsAllowAll checks whether the netsets.IPNet allows traffic from 0.0.0.0/0
func IsAllowAll(ipnets netsets.IPNet) bool {
	for _, s := range ipnets.StringSlice() {
		if s == "0.0.0.0/0" {
			return true
		}
	}
	return false
}

// GetLoadBalancerSourceRanges first try to parse and verify LoadBalancerSourceRanges field from a service.
// If the field is not specified, turn to parse and verify the AnnotationLoadBalancerSourceRangesKey annotation from a service,
// extracting the source ranges to allow, and if not present returns a default (allow-all) value.
func GetLoadBalancerSourceRanges(service *corev1.Service) (netsets.IPNet, error) {
	var ipnets netsets.IPNet
	var err error
	// if SourceRange field is specified, ignore sourceRange annotation
	if len(service.Spec.LoadBalancerSourceRanges) > 0 {
		specs := service.Spec.LoadBalancerSourceRanges
		ipnets, err = netsets.ParseIPNets(specs...)

		if err != nil {
			return nil, fmt.Errorf("service.Spec.LoadBalancerSourceRanges: %v is not valid. Expecting a list of IP ranges. For example, 10.0.0.0/24. Error msg: %v", specs, err)
		}
	} else {
		val := service.Annotations[corev1.AnnotationLoadBalancerSourceRangesKey]
		val = strings.TrimSpace(val)
		if val == "" {
			val = defaultLoadBalancerSourceRanges
		}
		specs := strings.Split(val, ",")
		ipnets, err = netsets.ParseIPNets(specs...)
		if err != nil {
			return nil, fmt.Errorf("%s: %s is not valid. Expecting a comma-separated list of source IP ranges. For example, 10.0.0.0/24,192.168.2.0/24", corev1.AnnotationLoadBalancerSourceRangesKey, val)
		}
	}
	return ipnets, nil
}
