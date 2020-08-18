package kinx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/trusts"
	tokens3 "github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/gophercloud/utils/client"
	"github.com/gophercloud/utils/openstack/clientconfig"
	gcfg "gopkg.in/gcfg.v1"

	"github.com/kinxnet/cloud-provider-kinx/pkg/util/metadata"
	"github.com/kinxnet/cloud-provider-kinx/pkg/version"
	netutil "k8s.io/apimachinery/pkg/util/net"
	certutil "k8s.io/client-go/util/cert"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the name of the kinx provider
	ProviderName = "kinx"

	// TypeHostName is the name type of openstack instance
	TypeHostName     = "hostname"
	availabilityZone = "availability_zone"
	defaultTimeOut   = 60 * time.Second
)

// ErrNotFound is used to inform that the object is missing
var ErrNotFound = errors.New("failed to find object")

// ErrMultipleResults is used when we unexpectedly get back multiple results
var ErrMultipleResults = errors.New("multiple results where only one expected")

// ErrNoAddressFound is used when we cannot find an ip address for the host
var ErrNoAddressFound = errors.New("no address found for host")

// ErrIPv6SupportDisabled is used when one tries to use IPv6 Addresses when
// IPv6 support is disabled by config
var ErrIPv6SupportDisabled = errors.New("IPv6 support is disabled")

// userAgentData is used to add extra information to the gophercloud user-agent
var userAgentData []string

// MyDuration is the encoding.TextUnmarshaler interface for time.Duration
type MyDuration struct {
	time.Duration
}

// UnmarshalText is used to convert from text to Duration
func (d *MyDuration) UnmarshalText(text []byte) error {
	res, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = res
	return nil
}

// LBClass defines the corresponding floating network, floating subnet or internal subnet ID
type LBClass struct {
	FloatingNetworkID string `gcfg:"floating-network-id,omitempty"`
	FloatingSubnetID  string `gcfg:"floating-subnet-id,omitempty"`
	SubnetID          string `gcfg:"subnet-id,omitempty"`
	NetworkID         string `gcfg:"network-id,omitempty"`
}

// NetworkingOpts is used for networking settings
type NetworkingOpts struct {
	IPv6SupportDisabled bool     `gcfg:"ipv6-support-disabled"`
	PublicNetworkName   []string `gcfg:"public-network-name"`
	InternalNetworkName []string `gcfg:"internal-network-name"`
}

// LoadBalancer is used for creating and maintaining load balancers
type LoadBalancer struct {
	network *gophercloud.ServiceClient
	compute *gophercloud.ServiceClient
	lb      *gophercloud.ServiceClient
	opts    LoadBalancerOpts
}

// LoadBalancerOpts have the options to talk to Neutron LBaaSV2 or Octavia
type LoadBalancerOpts struct {
	LBVersion            string              `gcfg:"lb-version"`          // overrides autodetection. Only support v2.
	SubnetID             string              `gcfg:"subnet-id"`           // overrides autodetection.
	NetworkID            string              `gcfg:"network-id"`          // If specified, will create virtual ip from a subnet in network which has available IP addresses
	FloatingNetworkID    string              `gcfg:"floating-network-id"` // If specified, will create floating ip for loadbalancer, or do not create floating ip.
	FloatingSubnetID     string              `gcfg:"floating-subnet-id"`  // If specified, will create floating ip for loadbalancer in this particular floating pool subnetwork.
	LBClasses            map[string]*LBClass // Predefined named Floating networks and subnets
	LBMethod             string              `gcfg:"lb-method"` // default to ROUND_ROBIN.
	LBProvider           string              `gcfg:"lb-provider"`
	CreateMonitor        bool                `gcfg:"create-monitor"`
	MonitorDelay         MyDuration          `gcfg:"monitor-delay"`
	MonitorTimeout       MyDuration          `gcfg:"monitor-timeout"`
	MonitorMaxRetries    uint                `gcfg:"monitor-max-retries"`
	ManageSecurityGroups bool                `gcfg:"manage-security-groups"`
	NodeSecurityGroupIDs []string            // Do not specify, get it automatically when enable manage-security-groups. TODO(FengyunPan): move it into cache
	InternalLB           bool                `gcfg:"internal-lb"`    // default false
	CascadeDelete        bool                `gcfg:"cascade-delete"` // applicable only if use-octavia is set to True
}

// Kinx is an implementation of cloud provider Interface for Kinx.
type Kinx struct {
	openstackProvider *gophercloud.ProviderClient
	region            string
	lbOpts            LoadBalancerOpts
	routeOpts         RouterOpts
	metadataOpts      MetadataOpts
	networkingOpts    NetworkingOpts
	// InstanceID of the server where this OpenStack object is instantiated.
	localInstanceID string
}

// MetadataOpts is used for configuring how to talk to metadata service or config drive
type MetadataOpts struct {
	SearchOrder    string     `gcfg:"search-order"`
	RequestTimeout MyDuration `gcfg:"request-timeout"`
}

type AuthOpts struct {
	AuthURL          string `gcfg:"auth-url" mapstructure:"auth-url" name:"os-authURL" dependsOn:"os-password|os-trustID"`
	UserID           string `gcfg:"user-id" mapstructure:"user-id" name:"os-userID" value:"optional" dependsOn:"os-password"`
	Username         string `name:"os-userName" value:"optional" dependsOn:"os-password"`
	Password         string `name:"os-password" value:"optional" dependsOn:"os-domainID|os-domainName,os-projectID|os-projectName,os-userID|os-userName"`
	TenantID         string `gcfg:"tenant-id" mapstructure:"project-id" name:"os-projectID" value:"optional" dependsOn:"os-password"`
	TenantName       string `gcfg:"tenant-name" mapstructure:"project-name" name:"os-projectName" value:"optional" dependsOn:"os-password"`
	TrustID          string `gcfg:"trust-id" mapstructure:"trust-id" name:"os-trustID" value:"optional"`
	DomainID         string `gcfg:"domain-id" mapstructure:"domain-id" name:"os-domainID" value:"optional" dependsOn:"os-password"`
	DomainName       string `gcfg:"domain-name" mapstructure:"domain-name" name:"os-domainName" value:"optional" dependsOn:"os-password"`
	TenantDomainID   string `gcfg:"tenant-domain-id" mapstructure:"project-domain-id" name:"os-projectDomainID" value:"optional"`
	TenantDomainName string `gcfg:"tenant-domain-name" mapstructure:"project-domain-name" name:"os-projectDomainName" value:"optional"`
	UserDomainID     string `gcfg:"user-domain-id" mapstructure:"user-domain-id" name:"os-userDomainID" value:"optional"`
	UserDomainName   string `gcfg:"user-domain-name" mapstructure:"user-domain-name" name:"os-userDomainName" value:"optional"`
	Region           string `name:"os-region"`
	CAFile           string `gcfg:"ca-file" mapstructure:"ca-file" name:"os-certAuthorityPath" value:"optional"`

	// Manila only options
	TLSInsecure string `name:"os-TLSInsecure" value:"optional" matches:"^true|false$"`
	// backward compatibility with the manila-csi-plugin
	CAFileContents  string `name:"os-certAuthority" value:"optional"`
	TrusteeID       string `name:"os-trusteeID" value:"optional" dependsOn:"os-trustID"`
	TrusteePassword string `name:"os-trusteePassword" value:"optional" dependsOn:"os-trustID"`

	UseClouds  bool   `gcfg:"use-clouds" mapstructure:"use-clouds" name:"os-useClouds" value:"optional"`
	CloudsFile string `gcfg:"clouds-file,omitempty" mapstructure:"clouds-file,omitempty" name:"os-cloudsFile" value:"optional"`
	Cloud      string `gcfg:"cloud,omitempty" mapstructure:"cloud,omitempty" name:"os-cloud" value:"optional"`

	ApplicationCredentialID     string `gcfg:"application-credential-id" mapstructure:"application-credential-id" name:"os-applicationCredentialID" value:"optional"`
	ApplicationCredentialName   string `gcfg:"application-credential-name" mapstructure:"application-credential-name" name:"os-applicationCredentialName" value:"optional"`
	ApplicationCredentialSecret string `gcfg:"application-credential-secret" mapstructure:"application-credential-secret" name:"os-applicationCredentialSecret" value:"optional"`
}

// Config is used to read and store information from the cloud configuration file
type Config struct {
	Global            AuthOpts
	LoadBalancer      LoadBalancerOpts
	LoadBalancerClass map[string]*LBClass
	Metadata          MetadataOpts
}

func LogCfg(cfg Config) {
	klog.V(5).Infof("AuthURL: %s", cfg.Global.AuthURL)
	klog.V(5).Infof("UserID: %s", cfg.Global.UserID)
	klog.V(5).Infof("Username: %s", cfg.Global.Username)
	klog.V(5).Infof("TenantID: %s", cfg.Global.TenantID)
	klog.V(5).Infof("TenantName: %s", cfg.Global.TenantName)
	klog.V(5).Infof("TrustID: %s", cfg.Global.TrustID)
	klog.V(5).Infof("DomainID: %s", cfg.Global.DomainID)
	klog.V(5).Infof("DomainName: %s", cfg.Global.DomainName)
	klog.V(5).Infof("TenantDomainID: %s", cfg.Global.TenantDomainID)
	klog.V(5).Infof("TenantDomainName: %s", cfg.Global.TenantDomainName)
	klog.V(5).Infof("UserDomainID: %s", cfg.Global.UserDomainID)
	klog.V(5).Infof("UserDomainName: %s", cfg.Global.UserDomainName)
	klog.V(5).Infof("Region: %s", cfg.Global.Region)
	klog.V(5).Infof("CAFile: %s", cfg.Global.CAFile)
	klog.V(5).Infof("UseClouds: %t", cfg.Global.UseClouds)
	klog.V(5).Infof("CloudsFile: %s", cfg.Global.CloudsFile)
	klog.V(5).Infof("Cloud: %s", cfg.Global.Cloud)
	klog.V(5).Infof("ApplicationCredentialID: %s", cfg.Global.ApplicationCredentialID)
	klog.V(5).Infof("ApplicationCredentialName: %s", cfg.Global.ApplicationCredentialName)
}

type Logger struct{}

func (l Logger) Printf(format string, args ...interface{}) {
	debugger := klog.V(6).Enabled()

	// extra check in case, when verbosity has been changed dynamically
	if debugger {
		var skip int
		var found bool
		var gc = "/github.com/gophercloud/gophercloud"

		// detect the depth of the actual function, which calls gophercloud code
		// 10 is the common depth from the logger to "github.com/gophercloud/gophercloud"
		for i := 10; i <= 20; i++ {
			if _, file, _, ok := runtime.Caller(i); ok && !found && strings.Contains(file, gc) {
				found = true
				continue
			} else if ok && found && !strings.Contains(file, gc) {
				skip = i
				break
			} else if !ok {
				break
			}
		}

		for _, v := range strings.Split(fmt.Sprintf(format, args...), "\n") {
			klog.InfoDepth(skip, v)
		}
	}
}

func init() {
	RegisterMetrics()
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		cfg, err := ReadConfig(config)
		if err != nil {
			klog.Warningf("failed to read config: %v", err)
			return nil, err
		}
		cloud, err := NewKinx(cfg)
		if err != nil {
			klog.Warningf("New Kinx client created failed with config: %v", err)
		}
		return cloud, err
	})
}

func (cfg AuthOpts) ToAuthOptions() gophercloud.AuthOptions {
	opts := clientconfig.ClientOpts{
		// this is needed to disable the clientconfig.AuthOptions func env detection
		EnvPrefix: "_",
		Cloud:     cfg.Cloud,
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:                     cfg.AuthURL,
			UserID:                      cfg.UserID,
			Username:                    cfg.Username,
			Password:                    cfg.Password,
			ProjectID:                   cfg.TenantID,
			ProjectName:                 cfg.TenantName,
			DomainID:                    cfg.DomainID,
			DomainName:                  cfg.DomainName,
			ProjectDomainID:             cfg.TenantDomainID,
			ProjectDomainName:           cfg.TenantDomainName,
			UserDomainID:                cfg.UserDomainID,
			UserDomainName:              cfg.UserDomainName,
			ApplicationCredentialID:     cfg.ApplicationCredentialID,
			ApplicationCredentialName:   cfg.ApplicationCredentialName,
			ApplicationCredentialSecret: cfg.ApplicationCredentialSecret,
		},
	}

	ao, err := clientconfig.AuthOptions(&opts)
	if err != nil {
		klog.V(1).Infof("Error parsing auth: %s", err)
		return gophercloud.AuthOptions{}
	}

	// Persistent service, so we need to be able to renew tokens.
	ao.AllowReauth = true

	return *ao
}

func (cfg AuthOpts) ToAuth3Options() tokens3.AuthOptions {
	ao := cfg.ToAuthOptions()

	var scope tokens3.Scope
	if ao.Scope != nil {
		scope.ProjectID = ao.Scope.ProjectID
		scope.ProjectName = ao.Scope.ProjectName
		scope.DomainID = ao.Scope.DomainID
		scope.DomainName = ao.Scope.DomainName
	}

	return tokens3.AuthOptions{
		IdentityEndpoint:            ao.IdentityEndpoint,
		UserID:                      ao.UserID,
		Username:                    ao.Username,
		Password:                    ao.Password,
		DomainID:                    ao.DomainID,
		DomainName:                  ao.DomainName,
		ApplicationCredentialID:     ao.ApplicationCredentialID,
		ApplicationCredentialName:   ao.ApplicationCredentialName,
		ApplicationCredentialSecret: ao.ApplicationCredentialSecret,
		Scope:                       scope,
		AllowReauth:                 ao.AllowReauth,
	}
}

// ReadConfig reads values from the cloud.conf
func ReadConfig(config io.Reader) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("no OpenStack cloud provider config file given")
	}
	var cfg Config

	// Set default values explicitly
	cfg.LoadBalancer.InternalLB = false
	cfg.LoadBalancer.LBProvider = "kinx"
	cfg.LoadBalancer.LBMethod = "ROUND_ROBIN"
	cfg.LoadBalancer.CreateMonitor = false
	cfg.LoadBalancer.ManageSecurityGroups = false
	cfg.LoadBalancer.MonitorDelay = MyDuration{5 * time.Second}
	cfg.LoadBalancer.MonitorTimeout = MyDuration{3 * time.Second}
	cfg.LoadBalancer.MonitorMaxRetries = 1
	cfg.LoadBalancer.CascadeDelete = true

	err := gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
	if err != nil {
		return Config{}, err
	}

	klog.V(5).Infof("Config, loaded from the config file:")
	LogCfg(cfg)

	if cfg.Global.UseClouds {
		if cfg.Global.CloudsFile != "" {
			os.Setenv("OS_CLIENT_CONFIG_FILE", cfg.Global.CloudsFile)
		}
		err = ReadClouds(&cfg)
		if err != nil {
			return Config{}, err
		}
		klog.V(5).Infof("Config, loaded from the %s:", cfg.Global.CloudsFile)
		LogCfg(cfg)
	}
	// Set the default values for search order if not set
	if cfg.Metadata.SearchOrder == "" {
		cfg.Metadata.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	}

	return cfg, err
}

// caller is a tiny helper for conditional unwind logic
type caller bool

func newCaller() caller   { return caller(true) }
func (c *caller) disarm() { *c = false }

func (c *caller) call(f func()) {
	if *c {
		f()
	}
}

// replaceEmpty is a helper function to replace empty fields with another field
func replaceEmpty(a string, b string) string {
	if a == "" {
		return b
	}
	return a
}

// ReadClouds reads Reads clouds.yaml to generate a Config
// Allows the cloud-config to have priority
func ReadClouds(cfg *Config) error {
	co := new(clientconfig.ClientOpts)
	if cfg.Global.Cloud != "" {
		co.Cloud = cfg.Global.Cloud
	}
	cloud, err := clientconfig.GetCloudFromYAML(co)
	if err != nil && err.Error() != "unable to load clouds.yaml: no clouds.yaml file found" {
		return err
	}

	cfg.Global.AuthURL = replaceEmpty(cfg.Global.AuthURL, cloud.AuthInfo.AuthURL)
	cfg.Global.UserID = replaceEmpty(cfg.Global.UserID, cloud.AuthInfo.UserID)
	cfg.Global.Username = replaceEmpty(cfg.Global.Username, cloud.AuthInfo.Username)
	cfg.Global.Password = replaceEmpty(cfg.Global.Password, cloud.AuthInfo.Password)
	cfg.Global.TenantID = replaceEmpty(cfg.Global.TenantID, cloud.AuthInfo.ProjectID)
	cfg.Global.TenantName = replaceEmpty(cfg.Global.TenantName, cloud.AuthInfo.ProjectName)
	cfg.Global.DomainID = replaceEmpty(cfg.Global.DomainID, cloud.AuthInfo.DomainID)
	cfg.Global.DomainName = replaceEmpty(cfg.Global.DomainName, cloud.AuthInfo.DomainName)
	cfg.Global.TenantDomainID = replaceEmpty(cfg.Global.TenantDomainID, cloud.AuthInfo.ProjectDomainID)
	cfg.Global.TenantDomainName = replaceEmpty(cfg.Global.TenantDomainName, cloud.AuthInfo.ProjectDomainName)
	cfg.Global.UserDomainID = replaceEmpty(cfg.Global.UserDomainID, cloud.AuthInfo.UserDomainID)
	cfg.Global.UserDomainName = replaceEmpty(cfg.Global.UserDomainName, cloud.AuthInfo.UserDomainName)
	cfg.Global.Region = replaceEmpty(cfg.Global.Region, cloud.RegionName)
	cfg.Global.CAFile = replaceEmpty(cfg.Global.CAFile, cloud.CACertFile)
	cfg.Global.ApplicationCredentialID = replaceEmpty(cfg.Global.ApplicationCredentialID, cloud.AuthInfo.ApplicationCredentialID)
	cfg.Global.ApplicationCredentialName = replaceEmpty(cfg.Global.ApplicationCredentialName, cloud.AuthInfo.ApplicationCredentialName)
	cfg.Global.ApplicationCredentialSecret = replaceEmpty(cfg.Global.ApplicationCredentialSecret, cloud.AuthInfo.ApplicationCredentialSecret)

	return nil
}

// check opts for OpenStackㄹ
func checkOpenStackOpts(kinxOpts *Kinx) error {
	return checkMetadataSearchOrder(kinxOpts.metadataOpts.SearchOrder)
}

// NewOpenStackClient creates a new instance of the openstack client
func NewOpenStackClient(cfg *AuthOpts, userAgent string, extraUserAgent ...string) (*gophercloud.ProviderClient, error) {
	provider, err := openstack.NewClient(cfg.AuthURL)
	if err != nil {
		return nil, err
	}

	ua := gophercloud.UserAgent{}
	ua.Prepend(fmt.Sprintf("%s/%s", userAgent, version.Version))
	for _, data := range extraUserAgent {
		ua.Prepend(data)
	}
	provider.UserAgent = ua
	klog.V(4).Infof("Using user-agent %s", ua.Join())

	var caPool *x509.CertPool
	if cfg.CAFile != "" {
		// read and parse CA certificate from file
		caPool, err = certutil.NewPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read and parse %s certificate: %s", cfg.CAFile, err)
		}
	} else if cfg.CAFileContents != "" {
		// parse CA certificate from the contents
		caPool = x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM([]byte(cfg.CAFileContents)); !ok {
			return nil, fmt.Errorf("failed to parse os-certAuthority certificate")
		}
	}

	config := &tls.Config{}
	config.InsecureSkipVerify = cfg.TLSInsecure == "true"

	if caPool != nil {
		config.RootCAs = caPool
	}

	provider.HTTPClient.Transport = netutil.SetOldTransportDefaults(&http.Transport{TLSClientConfig: config})

	if klog.V(6).Enabled() {
		provider.HTTPClient.Transport = &client.RoundTripper{
			Rt:     provider.HTTPClient.Transport,
			Logger: &Logger{},
		}
	}

	if cfg.TrustID != "" {
		opts := cfg.ToAuth3Options()

		// support for the legacy manila auth
		// if TrusteeID and TrusteePassword were defined, then use them
		opts.UserID = replaceEmpty(cfg.TrusteeID, opts.UserID)
		opts.Password = replaceEmpty(cfg.TrusteePassword, opts.Password)

		authOptsExt := trusts.AuthOptsExt{
			TrustID:            cfg.TrustID,
			AuthOptionsBuilder: &opts,
		}
		err = openstack.AuthenticateV3(provider, authOptsExt, gophercloud.EndpointOpts{})

		return provider, err
	}

	opts := cfg.ToAuthOptions()
	err = openstack.Authenticate(provider, opts)

	return provider, err
}

func NewKinx(cfg Config) (*Kinx, error) {
	provider, err := NewOpenStackClient(&cfg.Global, "kinx-cloud-controller-manager", userAgentData...)
	if err != nil {
		return nil, err
	}

	if cfg.Metadata.RequestTimeout == (MyDuration{}) {
		cfg.Metadata.RequestTimeout.Duration = time.Duration(defaultTimeOut)
	}
	provider.HTTPClient.Timeout = cfg.Metadata.RequestTimeout.Duration
	kinx := Kinx{
		openstackProvider: provider,
		region:            cfg.Global.Region,
		lbOpts:            cfg.LoadBalancer,
		metadataOpts:      cfg.Metadata,
	}

	// ini file doesn't support maps so we are reusing top level sub sections
	// and copy the resulting map to corresponding loadbalancer section
	kinx.lbOpts.LBClasses = cfg.LoadBalancerClass

	err = checkOpenStackOpts(&kinx)
	if err != nil {
		return nil, err
	}

	return &kinx, nil
}

// Initialize passes a Kubernetes clientBuilder interface to the cloud provider
func (k *Kinx) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// Clusters is a no-op
func (k *Kinx) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// ProviderName returns the cloud provider ID.
func (k *Kinx) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if the cluster has a clusterID
func (k *Kinx) HasClusterID() bool {
	return true
}

func checkMetadataSearchOrder(order string) error {
	if order == "" {
		return errors.New("invalid value in section [Metadata] with key `search-order`. Value cannot be empty")
	}

	elements := strings.Split(order, ",")
	if len(elements) > 2 {
		return errors.New("invalid value in section [Metadata] with key `search-order`. Value cannot contain more than 2 elements")
	}

	for _, id := range elements {
		id = strings.TrimSpace(id)
		switch id {
		case metadata.ConfigDriveID:
		case metadata.MetadataID:
		default:
			return fmt.Errorf("invalid element %q found in section [Metadata] with key `search-order`."+
				"Supported elements include %q and %q", id, metadata.ConfigDriveID, metadata.MetadataID)
		}
	}

	return nil
}

// InstanceID returns the kubelet's cloud provider ID.
func (k *Kinx) InstanceID() (string, error) {
	if len(k.localInstanceID) == 0 {
		id, err := readInstanceID(k.metadataOpts.SearchOrder)
		if err != nil {
			return "", err
		}
		k.localInstanceID = id
	}
	return k.localInstanceID, nil
}

func readInstanceID(searchOrder string) (string, error) {
	// First, try to get data from metadata service because local
	// data might be changed by accident
	md, err := metadata.Get(searchOrder)
	if err == nil {
		return md.UUID, nil
	}

	// Try to find instance ID on the local filesystem (created by cloud-init)
	const instanceIDFile = "/var/lib/cloud/data/instance-id"
	idBytes, err := ioutil.ReadFile(instanceIDFile)
	if err == nil {
		instanceID := string(idBytes)
		instanceID = strings.TrimSpace(instanceID)
		klog.V(3).Infof("Got instance id from %s: %s", instanceIDFile, instanceID)
		if instanceID != "" && instanceID != "iid-datasource-none" {
			return instanceID, nil
		}
	}

	return "", err
}

// Routes initializes routes support
func (k *Kinx) Routes() (cloudprovider.Routes, bool) {
	klog.V(4).Info("openstack.Routes() called")

	network, err := k.NewNetworkV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Network client: %v", err)
		return nil, false
	}

	netExts, err := networkExtensions(network)
	if err != nil {
		klog.Warningf("Failed to list neutron extensions: %v", err)
		return nil, false
	}

	if !netExts["extraroute"] {
		klog.V(3).Info("Neutron extraroute extension not found, required for Routes support")
		return nil, false
	}

	compute, err := k.NewComputeV2()
	if err != nil {
		klog.Errorf("Failed to create an OpenStack Compute client: %v", err)
		return nil, false
	}

	r, err := NewRoutes(compute, network, k.routeOpts, k.networkingOpts)
	if err != nil {
		klog.Warningf("Error initialising Routes support: %v", err)
		return nil, false
	}

	klog.V(1).Info("Claiming to support Routes")
	return r, true
}
