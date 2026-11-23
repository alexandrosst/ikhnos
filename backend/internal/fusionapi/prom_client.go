package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// promEnvelope is Prometheus' answer shape.
type promEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`
	// Warnings and Infos are what Prometheus says about an answer it gave (a query that hit a limit, a rate over a gauge).
	Warnings []string `json:"warnings,omitempty"`
	Infos    []string `json:"infos,omitempty"`
}

func (c *Client) prom(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	env, err := c.promEnv(ctx, path, q)
	return env.Data, err
}

// promEnv is Prometheus' whole answer, with what it said about it.
func (c *Client) promEnv(ctx context.Context, path string, q url.Values) (promEnvelope, error) {
	var env promEnvelope
	if err := c.get(ctx, storeProm, c.Prometheus, path, q, nil, &env); err != nil {
		return promEnvelope{}, err
	}
	if env.Status != "success" {
		return promEnvelope{}, errf(http.StatusBadGateway, "%s: %s", storeProm, env.Error)
	}
	return env, nil
}

// maxGetQuery is the longest encoded question sent to Prometheus or Loki in a URL; a longer one is sent as a form (POST),
// which their query endpoints take alike. 4 KiB stays well inside the 8 KiB request line common proxies allow.
const maxGetQuery = 4 << 10

// promForm is promEnv for a question that may be long: sent in the URL when it is short, as a form otherwise.
func (c *Client) promForm(ctx context.Context, path string, q url.Values) (promEnvelope, error) {
	if len(q.Encode()) <= maxGetQuery {
		return c.promEnv(ctx, path, q)
	}
	var env promEnvelope
	if err := c.call(ctx, http.MethodPost, storeProm, c.Prometheus, path, q, nil, &env); err != nil {
		return promEnvelope{}, err
	}
	if env.Status != "success" {
		return promEnvelope{}, errf(http.StatusBadGateway, "%s: %s", storeProm, env.Error)
	}
	return env, nil
}

// promRange evaluates one expression as a range query. A Scope with a namespace or cluster limit has it added to every selector
// of the expression (restrictPromQL); its results are then made of what it may see only, and are not checked again by their
// labels (a sum carries no namespace, and is the caller's to have).
func (c *Client) promRange(ctx context.Context, s Scope, expr string, w TimeRange, step time.Duration, maxSeries int) ([]MetricSeries, bool, error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return nil, false, err
	}
	restricted, err := restrictPromQL(expr, s)
	if err != nil {
		return nil, false, err
	}
	data, err := c.prom(ctx, "/api/v1/query_range", url.Values{
		"query": {restricted}, "start": {unixFloat(w.From)}, "end": {unixFloat(w.To)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}, "limit": {strconv.Itoa(maxSeries + 1)},
	})
	if err != nil {
		return nil, false, err
	}
	return decodeMatrix(data, Scope{Signals: s.Signals}, maxSeries)
}
