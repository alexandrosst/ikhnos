package chart

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// The server's own self-signed certificate for the admin port names the addresses the server is reached by, not only localhost: the
// host of admin.publicURL and of agent.publicAddress go in by themselves, next to admin.tls.selfSignedHosts. The names it was made for
// are written on the Secret, so that the next upgrade makes a new certificate when they change (the cluster lookup that decides this
// returns nothing under helm template, so that part is tried against a cluster by hand; here, the names and what is recorded).

type adminTLSRender struct {
	secret corev1.Secret
	pod    appsv1.Deployment
	ips    []string
	dns    []string
}

func renderAdminTLS(t *testing.T, extra ...string) adminTLSRender {
	t.Helper()
	h, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	dir := filepath.Join(t.TempDir(), "ikhnos-server")
	copyTree(t, filepath.Join("..", "..", "..", "deploy", "helm", "ikhnos-server"), dir)
	copyTree(t, "ikhnos-fusion", filepath.Join(dir, "charts", "ikhnos-fusion"))
	// No admin.behindTlsProxy: nothing else protects the port, so the chart makes the certificate.
	args := append([]string{"template", "ik", dir, "-n", "ikhnos", "--set", "agent.publicAddress=192.0.2.10:30443", "--set", "agent.service.type=NodePort", "--set", "sealing.disabled=true"}, extra...)
	out, err := exec.Command(h, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template %v: %v\n%s", extra, err, out)
	}
	var r adminTLSRender
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(out)), 4096)
	found := false
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &meta) != nil {
			continue
		}
		switch {
		case meta.Kind == "Secret" && strings.HasSuffix(meta.Metadata.Name, "-admin-tls-selfsigned"):
			json.Unmarshal(raw, &r.secret)
			found = true
		case meta.Kind == "Deployment" && meta.Metadata.Name == "ik-ikhnos-server":
			json.Unmarshal(raw, &r.pod)
		}
	}
	if !found {
		t.Fatalf("no self-signed admin certificate Secret was rendered:\n%s", out)
	}
	crt := r.secret.Data["tls.crt"]
	if len(crt) == 0 {
		t.Fatal("the Secret has no certificate")
	}
	block, _ := pem.Decode(crt)
	if block == nil {
		t.Fatal("the certificate is not PEM")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range c.IPAddresses {
		r.ips = append(r.ips, ip.String())
	}
	r.dns = c.DNSNames
	sort.Strings(r.ips)
	sort.Strings(r.dns)
	return r
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestTheAdminCertificateNamesTheAddressTheServerIsReachedBy(t *testing.T) {
	r := renderAdminTLS(t)
	for _, want := range []string{"127.0.0.1", "::1", "192.0.2.10"} {
		if !contains(r.ips, want) {
			t.Errorf("the certificate should name %s: %v", want, r.ips)
		}
	}
	if !contains(r.dns, "localhost") {
		t.Errorf("localhost: %v", r.dns)
	}
	if got := r.secret.Annotations["ikhnos.io/admin-tls-hosts"]; got != "192.0.2.10" {
		t.Errorf("the Secret should say which names it was made for: %q", got)
	}
}

func TestTheAdminCertificateAlsoNamesThePublicURLAndTheListedHosts(t *testing.T) {
	r := renderAdminTLS(t, "--set", "admin.publicURL=https://ikhnos.example.org:8443", "--set", "admin.tls.selfSignedHosts={lan.local,10.1.2.3,192.0.2.10}")
	if !contains(r.dns, "ikhnos.example.org") || !contains(r.dns, "lan.local") {
		t.Errorf("dns names: %v", r.dns)
	}
	if !contains(r.ips, "10.1.2.3") || !contains(r.ips, "192.0.2.10") {
		t.Errorf("ips: %v", r.ips)
	}
	n := 0
	for _, ip := range r.ips {
		if ip == "192.0.2.10" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("an address given twice is named once: %v", r.ips)
	}
	if got := r.secret.Annotations["ikhnos.io/admin-tls-hosts"]; got != "10.1.2.3,192.0.2.10,ikhnos.example.org,lan.local" {
		t.Errorf("names recorded, sorted: %q", got)
	}
}

func TestTheServerPodRestartsWhenTheNamesOfItsCertificateChange(t *testing.T) {
	a := renderAdminTLS(t).pod.Spec.Template.Annotations["checksum/admin-tls-hosts"]
	b := renderAdminTLS(t).pod.Spec.Template.Annotations["checksum/admin-tls-hosts"]
	c := renderAdminTLS(t, "--set", "admin.tls.selfSignedHosts={10.1.2.3}").pod.Spec.Template.Annotations["checksum/admin-tls-hosts"]
	if a == "" || a != b {
		t.Errorf("the same settings must give the same text (or every sync would restart the server): %q %q", a, b)
	}
	if a == c {
		t.Error("new names must restart the server, which reads its certificate at start")
	}
}
