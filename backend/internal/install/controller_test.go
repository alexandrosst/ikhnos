package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheResourceSaysWhatTheCommandSays(t *testing.T) {
	r := AgentResource{
		Server: "ik.example.com:8443", CAPin: "sha256/AbC+/=", Tier: 1,
		Collectors:    Collectors{NodeProbe: true},
		Scope:         Scope{Namespaces: []string{"shop", "pay"}, Exclude: []string{"pay-test"}, Selector: "ikhnos.io/scope=yes"},
		PurgeIdentity: true,
		Image:         &ResourceImage{Repository: "reg.example/ikhnos", Tag: "1.2.3", Digest: "sha256:" + strings.Repeat("a", 64)},
	}
	got, err := r.Manifest("ikhnos-system")
	if err != nil {
		t.Fatal(err)
	}
	want := `apiVersion: ikhnos.io/v1alpha1
kind: IkhnosAgent
metadata:
  name: this-cluster
  namespace: ikhnos-system
spec:
  server:
    address: "ik.example.com:8443"
    caPin: "sha256/AbC+/="
  enrollment:
    tokenSecretRef:
      name: ikhnos-enrollment
  accessTier: 1
  collectors:
    nodeProbe: true
  scope:
    namespaces: ["shop","pay"]
    exclude: ["pay-test"]
    selector: "ikhnos.io/scope=yes"
  purgeIdentityOnDelete: true
  image:
    repository: "reg.example/ikhnos"
    tag: "1.2.3"
    digest: "sha256:` + strings.Repeat("a", 64) + `"
`
	if got != want {
		t.Fatalf("manifest:\n%s\nwant:\n%s", got, want)
	}
}

func TestAResourceThatAsksForNothingExtraSaysNothingExtra(t *testing.T) {
	got, err := AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 2}.Manifest("ikhnos-system")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"accessTier", "collectors", "scope", "purgeIdentityOnDelete", "image"} {
		if strings.Contains(got, word) {
			t.Errorf("a plain install mentions %s:\n%s", word, got)
		}
	}
}

func TestAResourceTheControllerWouldRefuseIsNotWritten(t *testing.T) {
	ok := AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 2}
	for name, r := range map[string]AgentResource{
		"no port":                    {Server: "h", CAPin: "sha256/x", Tier: 2},
		"no host":                    {Server: ":1", CAPin: "sha256/x", Tier: 2},
		"a port out of range":        {Server: "h:70000", CAPin: "sha256/x", Tier: 2},
		"no pin":                     {Server: "h:1", Tier: 2},
		"a pin with a dot":           {Server: "h:1", CAPin: "a.b", Tier: 2},
		"a tier above the highest":   {Server: "h:1", CAPin: "sha256/x", Tier: 3},
		"a tier below the lowest":    {Server: "h:1", CAPin: "sha256/x", Tier: -1},
		"a collector above its tier": {Server: "h:1", CAPin: "sha256/x", Tier: 1, Collectors: Collectors{FlowObserver: true}},
		"a scope the agent refuses":  {Server: "h:1", CAPin: "sha256/x", Tier: 2, Scope: Scope{Namespaces: []string{"A b"}}},
	} {
		if _, err := r.Manifest("ikhnos-system"); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if _, err := ok.Manifest("Not A Name"); err == nil {
		t.Error("a namespace that is not a name was written")
	}
}

// Whatever a value holds, it is one quoted scalar in the manifest, and the manifest does not end early.
func TestAValueInTheResourceIsOneScalarWhateverItHolds(t *testing.T) {
	r := AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 2, Image: &ResourceImage{Repository: "r\"\nkind: Secret\n", Tag: "x: y # z"}}
	got, err := r.Apply("ikhnos-system")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "\nkind:") != 1 || !strings.HasSuffix(got, "\nYAML") || strings.Count(got, "\nYAML") != 1 {
		t.Fatalf("a value broke out of its line:\n%s", got)
	}
	if !strings.HasPrefix(got, "kubectl apply -f - <<'YAML'\n") {
		t.Fatalf("not a quoted here-document:\n%s", got)
	}
}

func TestOneCommandInstallsTheControllerAndAsksForTheAgent(t *testing.T) {
	const token = "s3cr3t-token-value"
	res := AgentResource{
		Server: "ik.example.com:8443", CAPin: "sha256/AbC+/=", Tier: 1,
		Collectors:    Collectors{NodeProbe: true},
		Scope:         Scope{Namespaces: []string{"shop", "pay"}, Exclude: []string{"pay-test"}, Selector: "ikhnos.io/scope=yes"},
		PurgeIdentity: true,
		Image:         &ResourceImage{Repository: "reg.example/ikhnos", Tag: "1.2.3", Digest: "sha256:" + strings.Repeat("a", 64)},
	}
	c := ControllerInstall{Chart: "oci://reg.example/ikhnos-controller", Version: "1.2.3", MaxTier: 1, Registry: "reg.example/"}
	got, err := c.Bootstrap(res, token)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"helm upgrade --install ikhnos-controller oci://reg.example/ikhnos-controller", "--version 1.2.3", "--namespace ikhnos-system", "--create-namespace",
		"--reset-then-reuse-values",
		"--set maxAccessTier=1", "--set image.repository=reg.example/controller", "--set agent.image.repository=reg.example/ikhnos", "--set agent.linkImage.repository=reg.example/link",
		"--set-string bootstrap.server=ik.example.com:8443", "--set-string bootstrap.caPin=sha256/AbC+/=", "--set-string bootstrap.token=" + token,
		"--set bootstrap.collectors.nodeProbe=true", "--set bootstrap.scope.namespaces='{shop,pay}'", "--set bootstrap.scope.exclude='{pay-test}'",
		"bootstrap.scope.selector=", "--set bootstrap.purgeIdentityOnDelete=true",
		"--set-string bootstrap.image.repository=reg.example/ikhnos", "--set-string bootstrap.image.tag=1.2.3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is not in\n%s", want, got)
		}
	}
	// The resource is asked for at the tier the controller is given, which the chart reads from maxAccessTier: it is not said twice.
	if strings.Contains(got, "bootstrap.accessTier") {
		t.Errorf("the tier is said twice:\n%s", got)
	}
	// It is one command: nothing to run before it and nothing after.
	if strings.Contains(got, "kubectl") || strings.Count(got, "helm ") != 1 {
		t.Errorf("more than one command:\n%s", got)
	}
}

func TestAPlainBootstrapSaysOnlyWhatTheInstallNeeds(t *testing.T) {
	got, err := ControllerInstall{Chart: "oci://x/ikhnos-controller", MaxTier: 2}.Bootstrap(AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 2}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"image.", "--version", "collectors", "scope", "purgeIdentityOnDelete"} {
		if strings.Contains(got, word) {
			t.Errorf("a plain install mentions %s:\n%s", word, got)
		}
	}
	if !strings.Contains(got, "maxAccessTier=2") || !strings.Contains(got, "bootstrap.token=tok") {
		t.Errorf("a plain install:\n%s", got)
	}
	// The one command turns the telemetry relay on (the server may then ask the cluster for more or less telemetry, within what the
	// controller was allowed); the raise and the allow commands leave it as it is.
	if !strings.Contains(got, "--set relay.telemetry=true") {
		t.Errorf("the one-command install does not turn the relay on:\n%s", got)
	}
	for _, cmd := range []func() (string, error){ControllerInstall{Chart: "oci://x/c", MaxTier: 1}.RaiseCeiling, ControllerInstall{Chart: "oci://x/c"}.AllowTelemetry} {
		if c, err := cmd(); err != nil || strings.Contains(c, "relay") {
			t.Errorf("a command that is not the install touches the relay: %v\n%s", err, c)
		}
	}
	// A resource that asks for less than the controller may give is said.
	got, err = ControllerInstall{Chart: "oci://x/c", MaxTier: 2}.Bootstrap(AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 1}, "tok")
	if err != nil || !strings.Contains(got, "--set bootstrap.accessTier=1") {
		t.Errorf("a lower tier was not said: %v\n%s", err, got)
	}
}

func TestABootstrapTheControllerWouldRefuseIsNotWritten(t *testing.T) {
	ok := AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 1}
	c := ControllerInstall{Chart: "oci://x/c", MaxTier: 1}
	if _, err := c.Bootstrap(ok, "tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Bootstrap(ok, ""); err == nil {
		t.Error("a command with no token was written")
	}
	if _, err := (ControllerInstall{Chart: "oci://x/c", MaxTier: 3}).Bootstrap(ok, "tok"); err == nil {
		t.Error("a tier above the highest was written")
	}
	if _, err := (ControllerInstall{Chart: "-oops", MaxTier: 1}).Bootstrap(ok, "tok"); err == nil {
		t.Error("a chart that is an option was written")
	}
	bad := ok
	bad.Collectors = Collectors{FlowObserver: true}
	if _, err := c.Bootstrap(bad, "tok"); err == nil {
		t.Error("a collector above its tier was written")
	}
	if _, err := (ControllerInstall{Chart: "oci://x/ikhnos-controller", MaxTier: 3}).RaiseCeiling(); err == nil {
		t.Error("a tier above the highest was written")
	}
}

// A value that holds shell syntax is still one word, and the command still ends where it should.
func TestABootstrapValueIsOneShellWord(t *testing.T) {
	res := AgentResource{Server: "h:1", CAPin: "sha256/x", Tier: 1, Image: &ResourceImage{Repository: "r/i", Tag: "a,b=$(touch /tmp/pwned)"}}
	got, err := ControllerInstall{Chart: "oci://x/c", MaxTier: 1}.Bootstrap(res, "t'o\"k$(id)en")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> " + log + "; done\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sh := exec.Command("sh", "-c", got)
	sh.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	if out, err := sh.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(log)
	if !strings.Contains(string(raw), "bootstrap.token=t'o\"k$(id)en") || !strings.Contains(string(raw), "b=$(touch /tmp/pwned)") {
		t.Errorf("a value was not passed as it was written:\n%s", raw)
	}
}

func TestTheDefinitionOfTheResourceIsAppliedFromTheControllersChart(t *testing.T) {
	got, err := ControllerInstall{Chart: "oci://reg.example/ikhnos-controller", Version: "0.1.0"}.Definition()
	if err != nil {
		t.Fatal(err)
	}
	if want := "helm show crds oci://reg.example/ikhnos-controller --version 0.1.0 | kubectl apply --server-side --force-conflicts -f -"; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if got, _ := (ControllerInstall{Chart: "./ikhnos-controller"}).Definition(); got != "helm show crds ./ikhnos-controller | kubectl apply --server-side --force-conflicts -f -" {
		t.Fatalf("no version: %s", got)
	}
	// A hostile chart is one shell word, and one that Helm would read as an option is not made into a command.
	if cmd, err := (ControllerInstall{Chart: "a; rm -rf /"}).Definition(); err != nil || !strings.HasPrefix(cmd, "helm show crds 'a; rm -rf /' | kubectl") {
		t.Errorf("a hostile chart: %s, %v", cmd, err)
	}
	if _, err := (ControllerInstall{Chart: "-x"}).Definition(); err == nil {
		t.Error("a chart that begins with a dash is not a chart")
	}
}

func TestACommandThatInstallsTheControllerStartsWithItsDefinitionWhereItCanBeWritten(t *testing.T) {
	c := ControllerInstall{Chart: "oci://reg.example/ikhnos-controller", Version: "0.1.0"}
	raise, err := c.RaiseCeiling()
	if err != nil {
		t.Fatal(err)
	}
	got := c.WithDefinition(raise)
	if !strings.HasPrefix(got, "helm show crds oci://reg.example/ikhnos-controller --version 0.1.0 | kubectl apply --server-side --force-conflicts -f - && \\\nhelm upgrade ikhnos-controller ") || !strings.HasSuffix(got, raise) {
		t.Fatalf("got\n%s", got)
	}
	if got := (ControllerInstall{Chart: "-x"}).WithDefinition("helm upgrade y"); got != "helm upgrade y" {
		t.Fatalf("where the definition cannot be written the command stands alone: %s", got)
	}
}

func TestTheControllerInstallSetsAllowTelemetryOnlyWhenAsked(t *testing.T) {
	a := AgentResource{Server: "ik.example.com:8443", CAPin: "sha256/AbCd==", Tier: 1}
	on, err := ControllerInstall{Chart: "oci://r/ikhnos-controller", MaxTier: 1, Telemetry: true}.Bootstrap(a, "ikh_x")
	if err != nil || !strings.Contains(on, "--set allowTelemetry=true") {
		t.Fatalf("%v\n%s", err, on)
	}
	off, err := ControllerInstall{Chart: "oci://r/ikhnos-controller", MaxTier: 1}.Bootstrap(a, "ikh_x")
	if err != nil || strings.Contains(off, "allowTelemetry") {
		t.Fatalf("%v\n%s", err, off)
	}
}
