package kinx

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

const (
	kinxSubsystem         = "kinx"
	kinxOperationKey      = "cloudprovider_kinx_api_request_duration_seconds"
	kinxOperationErrorKey = "cloudprovider_kinx_api_request_errors"
)

var (
	kinxOperationsLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: kinxSubsystem,
			Name:      kinxOperationKey,
			Help:      "Latency of kinx api call",
		},
		[]string{"request"},
	)

	kinxAPIRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: kinxSubsystem,
			Name:      kinxOperationErrorKey,
			Help:      "Cumulative number of kinx Api call errors",
		},
		[]string{"request"},
	)
)

func RegisterMetrics() {
	if err := prometheus.Register(kinxOperationsLatency); err != nil {
		klog.V(5).Infof("unable to register for latency metrics")
	}
	if err := prometheus.Register(kinxAPIRequestErrors); err != nil {
		klog.V(5).Infof("unable to register for error metrics")
	}
}
