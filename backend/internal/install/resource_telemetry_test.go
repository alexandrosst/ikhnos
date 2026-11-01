package install

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The telemetry of a resource is the telemetry of the panel's draft, in the words of a resource. These tests hold the two to each other:
// the Helm values a resource's telemetry makes are the values the panel's command states, for drafts that use every part of it.

func drafts() map[string]Telemetry {
	all := Signals{ResourceUsage: true, Energy: true, KubernetesState: true, NodeRuntime: true, NetworkLatency: true, ClusterLinks: true,
		ApplicationMetrics: true, SystemLogs: true, KubernetesEvents: true, ApplicationLogs: true, Traces: true, Accelerators: true}
	scoped := valid()
	scoped.Signals = Signals{ApplicationMetrics: true, ApplicationLogs: true, Traces: true, KubernetesEvents: true}
	scoped.Scopes = Scopes{
		ApplicationMetrics: ScopeOverride{Namespaces: []string{"shop", "pay"}, Workloads: []Workload{{Namespace: "shop", Names: []string{"cart"}}, {Namespace: "pay"}}},
		ApplicationLogs:    ScopeOverride{Exclude: []string{"kube-public"}},
		Traces:             ScopeOverride{Namespaces: []string{"shop"}},
		Infrastructure:     true,
	}
	everything := valid()
	everything.Signals = all
	everything.Energy = Source{Kind: "existing", Endpoint: "kepler.monitoring:9102"}
	everything.Accelerators = Accelerators{Source: Source{Kind: "existing", Endpoint: "dcgm.gpu:9400"}, ApplyScope: true}
	everything.ClusterLinks = ClusterLinks{Probe: true, Throughput: true, AuthSecret: "iperf-auth"}
	everything.Destination = Destination{Endpoint: "otel.example.com:4318", Protocol: "http", Insecure: true, AuthHeader: "X-Api-Key", AuthSecret: "otel-token", AuthKey: "key"}
	everything.Processing = Processing{ResourceDetection: true, Redaction: true, TracesSamplingPercent: pct(25.5), Debug: "basic", Tags: []Tag{{Key: "env", Value: "prod"}, {Key: "team", Value: "edge"}}}
	everything.Owner = Owner{OrgID: "org-1", ClusterID: "c-1"}
	split := valid()
	split.Signals = Signals{ResourceUsage: true, SystemLogs: true, Traces: true}
	split.Destination = Destination{}
	split.Routes = Routes{Split: true,
		Metrics: Destination{Endpoint: "m.example.com:4317", Protocol: "grpc"},
		Logs:    Destination{Endpoint: "https://l.example.com/v1/logs", Protocol: "http", AuthSecret: "logs-token"},
		Traces:  Destination{Endpoint: "z.example.com:9411", Protocol: "zipkin"}}
	plainAuth := valid()
	plainAuth.Destination = Destination{Endpoint: "otel.example.com:4317", Protocol: "grpc", AuthSecret: "otel-token", AuthKey: "token", AuthHeader: "Authorization"}
	return map[string]Telemetry{"the smallest": valid(), "every signal and every option": everything, "scoped application signals": scoped,
		"a route for each kind": split, "a credential at its defaults": plainAuth}
}

func TestTheValuesOfAResourcesTelemetryAreTheValuesOfThePanelsCommand(t *testing.T) {
	for name, d := range drafts() {
		t.Run(name, func(t *testing.T) {
			want, err := d.Values()
			if err != nil {
				t.Fatal(err)
			}
			r, err := ResourceOf(d)
			if err != nil || r == nil {
				t.Fatalf("%v %v", r, err)
			}
			// Through the wire, as the controller reads it from the cluster.
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var back ResourceTelemetry
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if p := back.Problems(2, true); len(p) > 0 {
				t.Fatalf("%v\n%s", p, b)
			}
			got, err := back.Values(2, true)
			if err != nil {
				t.Fatal(err)
			}
			wantMap, err := HelmValues(want)
			if err != nil {
				t.Fatal(err)
			}
			gotMap, err := HelmValues(got)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(wantMap, gotMap) {
				t.Fatalf("the resource and the command install different telemetry\ncommand:  %v\nresource: %v\n%s", wantMap, gotMap, b)
			}
		})
	}
}

func TestTurningTelemetryOffIsAResourceWithNone(t *testing.T) {
	off := valid()
	off.Signals = Signals{}
	if r, err := ResourceOf(off); r != nil || err != nil {
		t.Fatalf("%v %v", r, err)
	}
	// And a resource whose signals are all off installs nothing of telemetry.
	vs, err := ResourceTelemetry{}.Values(2, false)
	if err != nil || len(vs) != 0 {
		t.Fatalf("%v %v", vs, err)
	}
}

func TestWhatAResourceCannotStateIsRefusedInWordsThatSendThePersonToHelm(t *testing.T) {
	mut := func(f func(*Telemetry)) Telemetry { v := valid(); f(&v); return v }
	for name, c := range map[string]struct {
		t    Telemetry
		want string
	}{
		"a group left as installed":  {mut(func(t *Telemetry) { t.Kept = []string{KeptTags} }), "left as installed"},
		"a route left as installed":  {mut(func(t *Telemetry) { t.Routes.Kept = []Modality{Metrics} }), "left as installed"},
		"extra processors":           {mut(func(t *Telemetry) { t.Processing.Extra = []Processor{{Kind: Filter, Name: "x"}} }), "extra processors"},
		"an operator as destination": {mut(func(t *Telemetry) { t.Destination.Operator = "op-1" }), "regional operator"},
		"an operator in a route": {mut(func(t *Telemetry) {
			t.Routes.Split = true
			t.Routes.Metrics = Destination{Endpoint: "x:1", Protocol: "grpc", Operator: "op-1"}
		}), "regional operator"},
		"a draft with a plain problem": {mut(func(t *Telemetry) { t.Destination.Endpoint = "" }), "export endpoint is required"},
	} {
		r, err := ResourceOf(c.t)
		if r != nil || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, %v (wanted %q)", name, r, err, c.want)
		}
		if strings.Contains(c.want, "regional") || strings.Contains(c.want, "extra") {
			if !strings.Contains(err.Error(), "helm command") {
				t.Errorf("%s: does not send the person to the helm command: %v", name, err)
			}
		}
	}
}

func TestAResourceThatAsksForWhatTheAgentCannotDoIsRefused(t *testing.T) {
	ok := ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: &ResourceDestination{Endpoint: "otel.example.com:4317"}}
	if p := ok.Problems(0, false); len(p) != 0 {
		t.Fatalf("%v", p)
	}
	for name, c := range map[string]struct {
		r           ResourceTelemetry
		tier        int
		measurement bool
		want        string
	}{
		"no destination":                   {ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}}, 2, false, "export endpoint is required"},
		"cluster links below tier 2":       {ResourceTelemetry{Signals: ResourceSignals{ClusterLinks: true}, Destination: ok.Destination}, 1, false, "tier 2"},
		"network latency without measures": {ResourceTelemetry{Signals: ResourceSignals{NetworkLatency: true}, Destination: ok.Destination}, 2, false, "path measurements"},
		"a source nobody knows":            {ResourceTelemetry{Signals: ResourceSignals{Energy: true}, Energy: &ResourceSource{Kind: "mystery"}, Destination: ok.Destination}, 2, false, "energy.kind"},
		"a protocol nobody knows":          {ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: &ResourceDestination{Endpoint: "x:1", Protocol: "carrier-pigeon"}}, 2, false, "protocol"},
		"a debug level nobody knows":       {ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: ok.Destination, Processing: &ResourceProcessing{Debug: "loud"}}, 2, false, "debug"},
		"a hostile endpoint":               {ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: &ResourceDestination{Endpoint: "a b"}}, 2, false, "endpoint"},
		"a tag under the reserved prefix":  {ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: ok.Destination, Processing: &ResourceProcessing{Tags: []Tag{{Key: "ikhnos.x", Value: "1"}}}}, 2, false, "reserved"},
		"a sampling percentage over 100":   {ResourceTelemetry{Signals: ResourceSignals{Traces: true}, Destination: ok.Destination, Processing: &ResourceProcessing{TracesSamplingPercent: pct(120)}}, 2, false, "between 0 and 100"},
	} {
		p := c.r.Problems(c.tier, c.measurement)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "; "), c.want) {
			t.Errorf("%s: %v (wanted %q)", name, p, c.want)
		}
	}
}

// ---- nothing of the panel's telemetry is dropped without being decided ----

func jsonNames(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch {
		case name == "-":
		case name == "" && f.Anonymous: // embedded: its fields are this one's
			out = append(out, jsonNames(f.Type)...)
		case name != "":
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryFieldOfThePanelsTelemetryIsOnTheResourceOrIsSaidNotToBe(t *testing.T) {
	// The resource's own fields: it says in one object (mtls) what the draft says in two (mtlsSecret and serverName).
	only := map[string][]string{"Destination": {"mtls"}}
	// For each part of the draft: its resource counterpart, and the fields of the draft that a resource deliberately does not have.
	for _, c := range []struct {
		name     string
		draft    reflect.Type
		resource reflect.Type
		not      []string
	}{
		{"Telemetry", reflect.TypeOf(Telemetry{}), reflect.TypeOf(ResourceTelemetry{}), []string{"kept", "hadTelemetry", "measurementsOn"}},
		{"Signals", reflect.TypeOf(Signals{}), reflect.TypeOf(ResourceSignals{}), nil},
		{"Source", reflect.TypeOf(Source{}), reflect.TypeOf(ResourceSource{}), nil},
		{"Accelerators", reflect.TypeOf(Accelerators{}), reflect.TypeOf(ResourceAccelerators{}), nil},
		{"ClusterLinks", reflect.TypeOf(ClusterLinks{}), reflect.TypeOf(ResourceClusterLinks{}), nil},
		{"Workload", reflect.TypeOf(Workload{}), reflect.TypeOf(ResourceWorkload{}), nil},
		{"ScopeOverride", reflect.TypeOf(ScopeOverride{}), reflect.TypeOf(ResourceScope{}), nil},
		{"Scopes", reflect.TypeOf(Scopes{}), reflect.TypeOf(ResourceScopes{}), nil},
		{"Destination", reflect.TypeOf(Destination{}), reflect.TypeOf(ResourceDestination{}), []string{"operator", "mtlsSecret", "serverName"}},
		{"Routes", reflect.TypeOf(Routes{}), reflect.TypeOf(ResourceRoutes{}), []string{"installed", "kept"}},
		{"Processing", reflect.TypeOf(Processing{}), reflect.TypeOf(ResourceProcessing{}), []string{"extra"}},
	} {
		want := map[string]bool{}
		for _, n := range jsonNames(c.draft) {
			want[n] = true
		}
		for _, n := range c.not {
			if !want[n] {
				t.Errorf("%s: %q is said not to be on a resource and the draft has no such field", c.name, n)
			}
			delete(want, n)
		}
		got := map[string]bool{}
		for _, n := range jsonNames(c.resource) {
			got[n] = true
		}
		for n := range want {
			if !got[n] {
				t.Errorf("%s.%s is in the panel's draft and not on a resource: carry it, or say in this test that it is not", c.name, n)
			}
		}
		for n := range got {
			if !want[n] && !slices.Contains(only[c.name], n) {
				t.Errorf("%s.%s is on a resource and not in the panel's draft", c.name, n)
			}
		}
	}
}

func TestAnOperatorDestinationIsOnAResourceAsMutualTLSWhenTheServerSaysHowToReachIt(t *testing.T) {
	op := OperatorTLS{Endpoint: "central.fusion.example.com:4317", SecretName: "ikhnos-operator-client-op-1", ServerName: "central.fusion.example.com"}
	draft := valid()
	draft.Destination = Destination{Endpoint: "placeholder:4317", Protocol: "http", Insecure: true, AuthSecret: "left-over", Operator: "op-1"}

	if _, err := ResourceOfOperators(draft, nil); err == nil {
		t.Fatal("an operator the server said nothing of was accepted")
	}
	r, err := ResourceOfOperators(draft, map[string]OperatorTLS{"op-1": op})
	if err != nil {
		t.Fatal(err)
	}
	d := r.Destination
	if d == nil || d.Endpoint != op.Endpoint || d.MTLS == nil || d.MTLS.SecretName != op.SecretName || d.MTLS.ServerName != op.ServerName {
		t.Fatalf("the destination is %+v", d)
	}
	if d.Protocol != "" || d.Insecure || d.AuthSecret != "" {
		t.Errorf("what an earlier destination left in the draft reached the resource: %+v", d)
	}
	if p := r.Problems(2, true); len(p) != 0 {
		t.Fatalf("the resource is refused: %v", p)
	}

	// The release the controller installs states the same as the server's own command does for an operator.
	vals, err := r.Values(2, true)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, v := range vals {
		got[v.Key] = v.Value
	}
	for k, want := range map[string]string{
		"telemetry.export.otlp.endpoint":            op.Endpoint,
		"telemetry.export.otlp.protocol":            "grpc",
		"telemetry.export.otlp.tls.insecure":        "false",
		"telemetry.export.otlp.tls.mtls.enabled":    "true",
		"telemetry.export.otlp.tls.mtls.secretName": op.SecretName,
		"telemetry.export.otlp.tls.serverName":      op.ServerName,
		"telemetry.export.otlp.auth.secretName":     "",
	} {
		if g, ok := got[k]; !ok || g != want {
			t.Errorf("%s = %q (present %v), want %q", k, g, ok, want)
		}
	}
}

func TestAnMTLSDestinationOfAResourceNeedsItsSecretAndEndpoint(t *testing.T) {
	r := ResourceTelemetry{Signals: ResourceSignals{ResourceUsage: true}, Destination: &ResourceDestination{Endpoint: "x:4317", MTLS: &ResourceMTLS{}}}
	if p := r.Problems(0, false); len(p) == 0 || !strings.Contains(strings.Join(p, " "), "mtls.secretName") {
		t.Errorf("an mTLS destination without a Secret is accepted: %v", p)
	}
	r.Destination = &ResourceDestination{MTLS: &ResourceMTLS{SecretName: "s"}}
	if p := r.Problems(0, false); len(p) == 0 {
		t.Errorf("an mTLS destination without an endpoint is accepted")
	}
}
