package fusionapi

import (
	"math/rand"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
)

// memberAtByMembers is the rule RunsAt replaced, member by member, kept here to check that the places give the same answer.
func memberAtByMembers(g *AppGroup, name, ns, cl string) bool {
	for _, m := range g.Members {
		if (m.Name == name || slices.Contains(m.Aliases, name)) && (m.Namespace == "" || m.Namespace == ns) &&
			(m.Cluster == "" || m.Cluster == cl) {
			return true
		}
	}
	return false
}

func TestRunsAtIsTheMemberRule(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	pick := func(vals ...string) string { return vals[r.Intn(len(vals))] }
	for range 500 {
		g := &AppGroup{}
		for range r.Intn(4) {
			m := AppMember{Name: pick("api", "web", "redis", ""), Namespace: pick("shop", "data", ""),
				Cluster: pick("c1", "c2", "")}
			if r.Intn(2) == 0 {
				m.Aliases = []string{pick("api", "db", "")}
			}
			g.Members = append(g.Members, m)
		}
		for _, name := range []string{"api", "web", "redis", "db"} { // (a name from the topology is never "")
			for _, ns := range []string{"shop", "data", ""} {
				for _, cl := range []string{"c1", "c2", ""} {
					if got, want := g.RunsAt(name, ns, cl), memberAtByMembers(g, name, ns, cl); got != want {
						t.Fatalf("RunsAt(%q, %q, %q) of %+v = %v, the members say %v", name, ns, cl, g.Members, got, want)
					}
				}
			}
		}
	}
}

func TestPlaceSetContains(t *testing.T) {
	p := placeSet{namespaces: []string{"shop", "data"}, clusters: []string{"c1"}, names: []string{"api", "redis"},
		exact: []FocusPlace{{Namespace: "shop", Cluster: "c1", Names: []string{"api"}},
			{Namespace: "data", Names: []string{"redis"}}}}
	for _, c := range []struct {
		ns, cl, name string
		want         bool
	}{
		{"shop", "c1", "api", true},
		{"data", "c1", "redis", true},
		{"data", "c1", "api", false}, // in the cut, but api is no member's name in data
		{"shop", "c2", "api", false}, // outside the cut
		{"", "c1", "api", false},     // a namespace limit sees nothing that does not say one
		{"shop", "c1", "", false},
	} {
		if got := p.contains(c.ns, c.cl, c.name); got != c.want {
			t.Errorf("contains(%q, %q, %q) = %v, want %v", c.ns, c.cl, c.name, got, c.want)
		}
	}
	if !(placeSet{}).contains("", "", "") {
		t.Error("an open set must contain everything")
	}
	s := Scope{Namespaces: []string{"shop"}}
	if !s.visibleAt("shop", "") || s.visibleAt("data", "") || s.visibleAt("", "c1") {
		t.Error("visibleAt must be the rights' namespace and cluster checks")
	}
}

func TestPlaceSetRenderers(t *testing.T) {
	s := Scope{Namespaces: []string{"shop", "data"}, FocusServices: []string{"api", "redis"}, FocusClusters: []string{"c1"},
		Focus: []FocusPlace{{Namespace: "shop", Cluster: "c1", Names: []string{"api"}},
			{Namespace: "data", Names: []string{"redis"}}}}
	p := s.places()
	if got, want := p.cutConds(labelSyntax), []string{`k8s_namespace_name=~"shop|data"`, `ikhnos_cluster_id=~"c1"`,
		`service_name=~"api|redis"`}; !slices.Equal(got, want) {
		t.Errorf("label cut = %q, want %q", got, want)
	}
	if got, want := traceSyntax.either(p.placeConds(traceSyntax)),
		`(resource.k8s.namespace.name = "shop" && resource.ikhnos.cluster.id = "c1" && resource.service.name = "api") || `+
			`(resource.k8s.namespace.name = "data" && resource.service.name = "redis")`; got != want {
		t.Errorf("trace places = %s\nwant %s", got, want)
	}
	if got := labelSyntax.either([][]string{{"a"}, {"b", "c"}, {"a"}}); got != "(a) or (b and c)" {
		t.Errorf("either = %s, want each alternative once", got)
	}
	if got, want := p.unnamed().promSelectors([]string{"x"}, []string{"service_name"}), []string{
		`{x,k8s_namespace_name="shop",ikhnos_cluster_id="c1",service_name=~"api"}`,
		`{x,k8s_namespace_name="data",service_name=~"redis"}`}; !slices.Equal(got, want) {
		t.Errorf("selectors = %q, want %q", got, want)
	}
	if got := (placeSet{}).promSelectors([]string{"x"}, []string{"service_name"}); !slices.Equal(got, []string{"{x}"}) {
		t.Errorf("an open set's selectors = %q, want base alone", got)
	}
}

// Several values are one regular expression in TraceQL, and it is a caller's rights, so it must match the listed values
// and nothing else, whatever they contain: checked with the matcher Tempo compiles it with (Prometheus', anchored) and
// with a plain unanchored search, which is how Tempo before 2.7 read it.
func TestATraceQLListOfValuesMatchesThoseValuesOnly(t *testing.T) {
	vals := []string{"a.b", "c+d", "x|y", `back\slash`, "(paren)", "dollar$", "^caret", "new\nline", "é"}
	cond := anyOf("resource.service.name", vals)
	lit, ok := strings.CutPrefix(cond, "resource.service.name =~ ")
	if !ok {
		t.Fatalf("several values are not one regular expression: %s", cond)
	}
	re, err := strconv.Unquote(lit) // (Tempo's lexer reads a string with strconv.Unquote)
	if err != nil {
		t.Fatal(err)
	}
	tempo, err := labels.NewFastRegexMatcher(re)
	if err != nil {
		t.Fatal(err)
	}
	plain := regexp.MustCompile(re)
	others := []string{"", "axb", "a.bc", "za.b", "c+dd", "cd", "ccd", "x", "y", "x|", "paren", "(paren)x", "dollar",
		"caret", "^caretx", "e", `backslash`, "new", "line"}
	for _, v := range vals {
		if !tempo.MatchString(v) || !plain.MatchString(v) {
			t.Errorf("%q is listed but not matched by %s", v, re)
		}
	}
	for _, v := range others {
		if tempo.MatchString(v) || plain.MatchString(v) {
			t.Errorf("%q is not listed but matched by %s", v, re)
		}
	}
	if got := anyOf("a", []string{"one"}); got != `a = "one"` {
		t.Errorf("one value = %s, want an equality", got)
	}
	if got := anyOf("a", []string{"", "x"}); got != `(a = "" || a = "x")` {
		t.Errorf("a list with the empty value = %s, want an or of equalities", got)
	}
}

func TestListHelpers(t *testing.T) {
	in := []string{"b", "a", "b", "c"}
	if got := distinctSorted(in); !slices.Equal(got, []string{"a", "b", "c"}) || !slices.Equal(in, []string{"b", "a", "b", "c"}) {
		t.Errorf("distinctSorted = %q (input now %q)", got, in)
	}
	if distinctSorted([]string{}) != nil {
		t.Error("distinctSorted of nothing must be nil")
	}
	if got := uniqueInOrder(in); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Errorf("uniqueInOrder = %q", got)
	}
	if got, cut := cutAt([]int{1, 2, 3}, 2); !slices.Equal(got, []int{1, 2}) || !cut {
		t.Errorf("cutAt = %v, %v", got, cut)
	}
	if got, cut := cutAt([]int{1, 2}, 2); len(got) != 2 || cut {
		t.Errorf("cutAt of a list at the limit = %v, %v", got, cut)
	}
	l := []string{"a", "b", "c", "d", "e"}
	got := firstOf(l, 3, func(n int) string { return "+" + string(rune('0'+n)) })
	if !slices.Equal(got, []string{"a", "b", "c", "+2"}) || l[3] != "d" {
		t.Errorf("firstOf = %q (input now %q)", got, l)
	}
	if OrEmpty[string](nil) == nil {
		t.Error("OrEmpty(nil) must be an empty list")
	}
}
