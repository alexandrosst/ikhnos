package fusionapi

import (
	"context"
	"encoding/json"
	"net/url"
)

// rawPromParams are the only parameters a raw Prometheus query forwards.
var rawPromParams = map[string][]string{
	"query":       {"query", "time"},
	"query_range": {"query", "start", "end", "step"},
}

// RawMetricQuery runs a PromQL query exactly as written and returns Prometheus' own "data" object. endpoint is
// "query" or "query_range". Only a Scope with no namespace or cluster limit may do this.
//
// It is for this package's own queries, which are about the whole of FUSION: only an unlimited Scope may ask.
func (c *Client) RawMetricQuery(ctx context.Context, s Scope, endpoint string, params url.Values) (json.RawMessage, error) {
	if err := s.needUnrestricted("a PromQL query"); err != nil {
		return nil, err
	}
	a, err := c.RawMetricAnswer(ctx, s, endpoint, params)
	return a.Data, err
}

// RawPromAnswer is Prometheus' answer to a query as written: its data, and the warnings and infos it gave with it.
type RawPromAnswer struct {
	Data     json.RawMessage
	Warnings []string
	Infos    []string
}

// RawMetricAnswer is a caller's own PromQL, with what Prometheus said about its answer, for a caller that passes the answer on as
// Prometheus gave it. A Scope limited to certain namespaces or clusters has its limits added to every selector of the query
// (restrictPromQL), so it reads only what it may see.
func (c *Client) RawMetricAnswer(ctx context.Context, s Scope, endpoint string, params url.Values) (RawPromAnswer, error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return RawPromAnswer{}, err
	}
	allowed, ok := rawPromParams[endpoint]
	if !ok {
		return RawPromAnswer{}, badRequest("unknown metrics endpoint")
	}
	q := url.Values{}
	for _, k := range allowed {
		if v := params.Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if q.Get("query") == "" || len(q.Get("query")) > 4096 {
		return RawPromAnswer{}, badRequest("query is required and at most 4096 characters")
	}
	if endpoint == "query_range" && (q.Get("start") == "" || q.Get("end") == "" || q.Get("step") == "") {
		return RawPromAnswer{}, badRequest("query_range needs start, end and step")
	}
	restricted, err := restrictPromQL(q.Get("query"), s)
	if err != nil {
		return RawPromAnswer{}, err
	}
	q.Set("query", restricted)
	env, err := c.promEnv(ctx, "/api/v1/"+endpoint, q)
	if err != nil {
		return RawPromAnswer{}, err
	}
	return RawPromAnswer{Data: env.Data, Warnings: env.Warnings, Infos: env.Infos}, nil
}
