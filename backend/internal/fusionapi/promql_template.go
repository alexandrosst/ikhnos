package fusionapi

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ParsePromQueries reads the values of the promql parameter.
func ParsePromQueries(vals []string) ([]PromQuery, error) {
	var out []PromQuery
	seen := map[string]bool{}
	for _, v := range vals {
		if strings.TrimSpace(v) == "" {
			continue
		}
		name, expr, ok := strings.Cut(v, "=")
		name, expr = strings.TrimSpace(name), strings.TrimSpace(expr)
		if !ok || !promNameRe.MatchString(name) || len(name) > promQueryNameMax || expr == "" || strings.HasPrefix(expr, "=") {
			return nil, badRequest("promql takes <name>=<expression>, with a name of letters, digits and underscores (for example promql=errors=sum(rate(http_errors_total{service_name=\"${service}\"}[1m])))")
		}
		if seen[name] {
			return nil, badRequest("promql: the name %q is used twice", name)
		}
		if len(expr) > maxPromQueryLen {
			return nil, badRequest("promql %s: at most %d characters", name, maxPromQueryLen)
		}
		tpl, err := parseTemplate(expr)
		if err != nil {
			return nil, badRequest("promql %s: %v", name, err)
		}
		q := PromQuery{Name: name, Expr: expr, tpl: tpl}
		for _, p := range tpl {
			for _, rv := range promResourceVars {
				if p.v == rv {
					q.perResource = true
				}
			}
		}
		seen[name] = true
		out = append(out, q)
		if len(out) > maxPromQueries {
			return nil, badRequest("promql: at most %d queries per read", maxPromQueries)
		}
	}
	return out, nil
}

func parseTemplate(expr string) ([]tplPart, error) {
	var parts []tplPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, tplPart{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(expr); {
		switch {
		case strings.HasPrefix(expr[i:], "$${"): // an escaped ${
			lit.WriteString("${")
			i += 3
		case strings.HasPrefix(expr[i:], "${"):
			end := strings.IndexByte(expr[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("a ${ is not closed (write $${ for a literal ${)")
			}
			name, mod, _ := strings.Cut(expr[i+2:i+end], ":")
			p := tplPart{v: strings.TrimSpace(name)}
			switch strings.TrimSpace(mod) {
			case "":
			case "re":
				p.re = true
			default:
				return nil, fmt.Errorf("${%s}: the only modifier is :re", name)
			}
			if !isPromVar(p.v) {
				return nil, fmt.Errorf("${%s} is not a variable; use %s", name, strings.Join(append(append([]string{}, promResourceVars...), promOtherVars...), ", "))
			}
			flush()
			parts = append(parts, p)
			i += end + 1
		default:
			lit.WriteByte(expr[i])
			i++
		}
	}
	flush()
	return parts, nil
}

func isPromVar(n string) bool {
	for _, v := range promResourceVars {
		if v == n {
			return true
		}
	}
	for _, v := range promOtherVars {
		if v == n {
			return true
		}
	}
	return false
}

// safePromValue says whether a value can stand inside a PromQL string without changing the query's shape.
func safePromValue(v string) bool {
	if v == "" || len(v) > maxPromValueLen {
		return false
	}
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if strings.ContainsRune("_.:/@+-=, ", r) {
			continue
		}
		return false
	}
	return true
}

// expand fills the variables in. skip says why this expansion cannot be made (a variable has no value, or an unsafe one).
func (q *PromQuery) expand(vars map[string]string) (expr, skip string) {
	var b strings.Builder
	for _, p := range q.tpl {
		if p.v == "" {
			b.WriteString(p.lit)
			continue
		}
		val := vars[p.v]
		if val == "" {
			return "", "no " + strings.ReplaceAll(p.v, "_", " ")
		}
		if !safePromValue(val) {
			return "", "its " + strings.ReplaceAll(p.v, "_", " ") + " cannot be put into a query safely"
		}
		if p.re { // a regular expression inside a double-quoted PromQL string: each backslash is written twice
			val = strings.ReplaceAll(regexp.QuoteMeta(val), `\`, `\\`)
		}
		b.WriteString(val)
	}
	return b.String(), ""
}

func resourceVars(r *Resource) map[string]string {
	return map[string]string{"service": r.Service, "namespace": r.Namespace, "pod": r.Pod, "node": r.Node, "cluster": r.Cluster, "workload": r.Workload()}
}

// narrowQuery narrows a query to what the read is about: the application's own series (narrowToApplication), and the services
// promql_services names.
func narrowQuery(expr string, opts FuseOptions) (string, error) {
	var err error
	if opts.Application != nil {
		if expr, err = narrowToApplication(expr, opts.Application); err != nil {
			return "", err
		}
	}
	if len(opts.PromQLServices) > 0 {
		return withMatchers(expr, map[string][]string{lblService: opts.PromQLServices}, narrowedByRead)
	}
	return expr, nil
}

// uses says whether the expression names one of the variables.
func (q *PromQuery) uses(vars ...string) bool {
	for _, p := range q.tpl {
		if p.v != "" && slices.Contains(vars, p.v) {
			return true
		}
	}
	return false
}
