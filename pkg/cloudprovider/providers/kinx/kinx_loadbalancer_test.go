package kinx

import (
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/listeners"
	v2pools "github.com/gophercloud/gophercloud/openstack/loadbalancer/v2/pools"
)

// TestIsBackendProtocol verifies that isBackendProtocol accepts both upper- and
// lower-case variants (its internal strings.ToLower makes it case-insensitive).
func TestIsBackendProtocol(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"http", true},
		{"HTTP", true},
		{"Http", true},
		{"tcp", true},
		{"TCP", true},
		{"terminated_https", true},
		{"TERMINATED_HTTPS", true},
		{"Terminated_Https", true},
		{"", false},
		{"udp", false},
		{"foo", false},
		{"FOO", false},
	}

	for _, tc := range cases {
		got := isBackendProtocol(tc.input)
		if got != tc.want {
			t.Errorf("isBackendProtocol(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestGetListenerProtocol verifies that getListenerProtocol maps lowercase
// backend-protocol constants to the expected OpenStack listener Protocol.
func TestGetListenerProtocol(t *testing.T) {
	cases := []struct {
		input string
		want  listeners.Protocol
	}{
		{backendProtocolHttp, listeners.ProtocolHTTP},
		{backendProtocolTcp, listeners.ProtocolTCP},
		{backendProtocolTerminatedHttps, listeners.ProtocolTerminatedHTTPS},
		// Unknown / empty inputs must return an empty string.
		{"", ""},
		{"unknown", ""},
	}

	for _, tc := range cases {
		got := getListenerProtocol(tc.input)
		if got != tc.want {
			t.Errorf("getListenerProtocol(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestGetPoolProtocol verifies that getPoolProtocol maps lowercase
// backend-protocol constants to the expected OpenStack pool Protocol.
func TestGetPoolProtocol(t *testing.T) {
	cases := []struct {
		input string
		want  v2pools.Protocol
	}{
		{backendProtocolHttp, v2pools.ProtocolHTTP},
		{backendProtocolTcp, v2pools.ProtocolTCP},
		// terminated_https uses HTTP on the pool side.
		{backendProtocolTerminatedHttps, v2pools.ProtocolHTTP},
		// Unknown / empty inputs must return an empty string.
		{"", ""},
		{"unknown", ""},
	}

	for _, tc := range cases {
		got := getPoolProtocol(tc.input)
		if got != tc.want {
			t.Errorf("getPoolProtocol(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestBackendProtocolNormalization is the core regression test for issue #13.
// It simulates the annotation-read → strings.ToLower normalization →
// getListenerProtocol / getPoolProtocol pipeline and asserts that upper- and
// mixed-case annotation values no longer produce an empty protocol string.
func TestBackendProtocolNormalization(t *testing.T) {
	cases := []struct {
		annotationValue    string
		wantListenerProto  listeners.Protocol
		wantPoolProto      v2pools.Protocol
		// emptyExpected indicates that an empty annotation value is intentional
		// (the caller falls back to the existing listener's protocol or TCP default).
		emptyExpected bool
	}{
		// --- normal lower-case (existing behaviour must be preserved) ---
		{"http", listeners.ProtocolHTTP, v2pools.ProtocolHTTP, false},
		{"tcp", listeners.ProtocolTCP, v2pools.ProtocolTCP, false},
		{"terminated_https", listeners.ProtocolTerminatedHTTPS, v2pools.ProtocolHTTP, false},
		// --- upper-case (bug: previously returned "" – must now return valid proto) ---
		{"HTTP", listeners.ProtocolHTTP, v2pools.ProtocolHTTP, false},
		{"TCP", listeners.ProtocolTCP, v2pools.ProtocolTCP, false},
		{"TERMINATED_HTTPS", listeners.ProtocolTerminatedHTTPS, v2pools.ProtocolHTTP, false},
		// --- mixed case ---
		{"Http", listeners.ProtocolHTTP, v2pools.ProtocolHTTP, false},
		{"Tcp", listeners.ProtocolTCP, v2pools.ProtocolTCP, false},
		{"Terminated_Https", listeners.ProtocolTerminatedHTTPS, v2pools.ProtocolHTTP, false},
		// --- empty value: normalization must preserve empty string ---
		{"", "", "", true},
	}

	for _, tc := range cases {
		// Simulate the normalization added in EnsureLoadBalancer.
		normalized := strings.ToLower(tc.annotationValue)

		gotListener := getListenerProtocol(normalized)
		gotPool := getPoolProtocol(normalized)

		if tc.emptyExpected {
			if gotListener != "" || gotPool != "" {
				t.Errorf("annotation %q: expected empty protocols after normalization, got listener=%q pool=%q",
					tc.annotationValue, gotListener, gotPool)
			}
			continue
		}

		if gotListener != tc.wantListenerProto {
			t.Errorf("annotation %q (normalized %q): getListenerProtocol = %q, want %q",
				tc.annotationValue, normalized, gotListener, tc.wantListenerProto)
		}
		if gotPool != tc.wantPoolProto {
			t.Errorf("annotation %q (normalized %q): getPoolProtocol = %q, want %q",
				tc.annotationValue, normalized, gotPool, tc.wantPoolProto)
		}
	}
}
