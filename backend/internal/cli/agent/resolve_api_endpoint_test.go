package agent

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// The "kubernetes" EndpointSlice in "default" (whatever backs the control plane) is what resolveApiEndpoint
// reads instead of the virtual ClusterIP every in-cluster client is handed by default; the Endpoints object of the
// same name, deprecated from Kubernetes 1.33, is read only when the slice cannot be.
func slice(addresses []discoveryv1.Endpoint, ports ...discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
		AddressType: discoveryv1.AddressTypeIPv4, Endpoints: addresses, Ports: ports,
	}
}

func str(s string) *string { return &s }
func i32(i int32) *int32   { return &i }
func yes() *bool           { b := true; return &b }
func no() *bool            { b := false; return &b }

func TestResolveApiEndpointReadsTheEndpointSlice(t *testing.T) {
	client := fake.NewSimpleClientset(slice(
		[]discoveryv1.Endpoint{{Addresses: []string{"10.0.5.12"}, Conditions: discoveryv1.EndpointConditions{Ready: yes()}}},
		discoveryv1.EndpointPort{Name: str("https"), Port: i32(6443)}))
	if got, want := resolveApiEndpoint(client), "10.0.5.12:6443"; got != want {
		t.Errorf("resolveApiEndpoint() = %q, want %q", got, want)
	}
}

func TestResolveApiEndpointSkipsAnAddressThatIsNotReadyAndPicksThePortByName(t *testing.T) {
	client := fake.NewSimpleClientset(slice(
		[]discoveryv1.Endpoint{
			{Addresses: []string{"10.0.5.1"}, Conditions: discoveryv1.EndpointConditions{Ready: no()}},
			{Addresses: []string{"10.0.5.2"}}, // ready is unset: nothing says it is not
		},
		discoveryv1.EndpointPort{Name: str("metrics"), Port: i32(9090)},
		discoveryv1.EndpointPort{Name: str("https"), Port: i32(6443)}))
	if got, want := resolveApiEndpoint(client), "10.0.5.2:6443"; got != want {
		t.Errorf("resolveApiEndpoint() = %q, want %q", got, want)
	}
}

func TestResolveApiEndpointTakesTheFirstPortOfASliceWhenNoneIsHttpsOr443(t *testing.T) {
	client := fake.NewSimpleClientset(slice([]discoveryv1.Endpoint{{Addresses: []string{"10.0.5.12"}}}, discoveryv1.EndpointPort{Port: i32(8443)}))
	if got, want := resolveApiEndpoint(client), "10.0.5.12:8443"; got != want {
		t.Errorf("resolveApiEndpoint() = %q, want %q", got, want)
	}
}

// The slice wins over the Endpoints object when both are there, and a slice with nothing usable in it is not read as an answer.
func TestResolveApiEndpointPrefersTheSliceAndIgnoresOneWithNothingUsable(t *testing.T) {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
		Subsets:    []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.9.9.9"}}, Ports: []corev1.EndpointPort{{Port: 443}}}},
	}
	both := fake.NewSimpleClientset(ep, slice([]discoveryv1.Endpoint{{Addresses: []string{"10.0.5.12"}}}, discoveryv1.EndpointPort{Port: i32(6443)}))
	if got, want := resolveApiEndpoint(both), "10.0.5.12:6443"; got != want {
		t.Errorf("with both, resolveApiEndpoint() = %q, want %q", got, want)
	}
	empty := fake.NewSimpleClientset(slice(nil, discoveryv1.EndpointPort{Port: i32(6443)}))
	if got := resolveApiEndpoint(empty); got != "" {
		t.Errorf("a slice with no endpoint gave %q", got)
	}
	noPort := fake.NewSimpleClientset(slice([]discoveryv1.Endpoint{{Addresses: []string{"10.0.5.12"}}}))
	if got := resolveApiEndpoint(noPort); got != "" {
		t.Errorf("a slice with no port gave %q", got)
	}
}

// An agent whose Role an older chart granted can read the Endpoints object and not the slice: it keeps resolving.
func TestResolveApiEndpointStillReadsEndpointsWhenThereIsNoSliceToRead(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.5.12"}},
			Ports:     []corev1.EndpointPort{{Name: "https", Port: 6443}},
		}},
	})
	if got, want := resolveApiEndpoint(client), "10.0.5.12:6443"; got != want {
		t.Errorf("resolveApiEndpoint() = %q, want %q", got, want)
	}
}

// A port with no name at all is still usable - the first (and here, only) one listed.
func TestResolveApiEndpointFallsBackToTheFirstPortWhenNoneIsNamedHttpsOr443(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.0.5.12"}},
			Ports:     []corev1.EndpointPort{{Name: "", Port: 8443}},
		}},
	})
	if got, want := resolveApiEndpoint(client), "10.0.5.12:8443"; got != want {
		t.Errorf("resolveApiEndpoint() = %q, want %q", got, want)
	}
}

// No permission, no such object, or no subset ready yet: the caller must be able to tell "nothing better
// found" apart from a real address, so it can keep whatever it already had.
func TestResolveApiEndpointIsEmptyWhenThereIsNothingUsable(t *testing.T) {
	cases := []struct {
		name   string
		client func() *fake.Clientset
	}{
		{"no such object (RBAC not granted, or a non-standard cluster)", func() *fake.Clientset { return fake.NewSimpleClientset() }},
		{"object exists but no subset has come up yet", func() *fake.Clientset {
			return fake.NewSimpleClientset(&corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"}})
		}},
		{"a subset with no addresses", func() *fake.Clientset {
			return fake.NewSimpleClientset(&corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
				Subsets:    []corev1.EndpointSubset{{Ports: []corev1.EndpointPort{{Port: 443}}}},
			})
		}},
	}
	for _, c := range cases {
		if got := resolveApiEndpoint(c.client()); got != "" {
			t.Errorf("%s: resolveApiEndpoint() = %q, want \"\"", c.name, got)
		}
	}
}
