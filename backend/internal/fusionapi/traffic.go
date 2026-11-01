package fusionapi

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// TrafficWindow is how far back the gateway's counters are read: long enough that a sender that reports every few seconds, or a
// gateway that pushes its counters every 30, always has samples inside it, short enough that "quiet" means quiet now.
const TrafficWindow = "5m"

// TrafficWindowSeconds is TrafficWindow in seconds, which the rates are figured over.
const TrafficWindowSeconds = 300

// SignalTraffic is what the central gateway did with one kind of telemetry in the window, per second.
type SignalTraffic struct {
	// Accepted is what its receiver took in from the senders; Refused what it turned away (a memory limit, a full queue).
	Accepted float64 `json:"accepted"`
	Refused  float64 `json:"refused,omitempty"`
	// Sent is what it delivered to the store behind it; Failed what it could not (the store refused, was down, or the queue was full).
	Sent   float64 `json:"sent"`
	Failed float64 `json:"failed,omitempty"`
	// Queue is how full the sending queue to the store is, 0 to 1. Absent when the gateway does not say.
	Queue *float64 `json:"queue,omitempty"`
}

// GatewayTraffic is what the gateway reports of its own work, as the stores see it. The gateway pushes these counters to the metrics
// store itself (nothing scrapes it), so they are as fresh as its last push, and they are what the gateway counted, not what a store
// holds.
type GatewayTraffic struct {
	// WindowSeconds is how long the figures are counted over.
	WindowSeconds int           `json:"windowSeconds"`
	Metrics       SignalTraffic `json:"metrics"`
	Logs          SignalTraffic `json:"logs"`
	Traces        SignalTraffic `json:"traces"`
}

// trafficCounters: the names of the gateway's own counters, by kind of telemetry (the collector says metric points, log records and spans).
const trafficCounters = `{__name__=~"otelcol_(receiver_(accepted|refused)|exporter_(sent|send_failed|enqueue_failed))_(metric_points|log_records|spans)_total"}`

// GatewayTraffic asks the metrics store for the gateway's counters over the last TrafficWindow. It returns nil, with no error, when the
// store holds none (the gateway has not pushed any lately: it is off, or too new): then nothing is known, which is not the same as nothing
// moving. Only a Scope with no limit may ask, since it says something about every sender.
func (c *Client) GatewayTraffic(ctx context.Context, s Scope) (*GatewayTraffic, error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return nil, err
	}
	if err := s.needUnrestricted("the gateway's traffic"); err != nil {
		return nil, err
	}
	counted, err := c.promVector(ctx, s, `sum by (__name__) (increase(`+trafficCounters+`[`+TrafficWindow+`]))`)
	if err != nil {
		return nil, err
	}
	if len(counted) == 0 {
		return nil, nil
	}
	out := &GatewayTraffic{WindowSeconds: TrafficWindowSeconds}
	for _, r := range counted {
		sig := out.signal(r.Metric["__name__"])
		if sig == nil {
			continue
		}
		perSecond := r.Value / TrafficWindowSeconds
		switch {
		case strings.Contains(r.Metric["__name__"], "_receiver_accepted_"):
			sig.Accepted += perSecond
		case strings.Contains(r.Metric["__name__"], "_receiver_refused_"):
			sig.Refused += perSecond
		case strings.Contains(r.Metric["__name__"], "_exporter_sent_"):
			sig.Sent += perSecond
		default:
			sig.Failed += perSecond
		}
	}
	// The queue is read apart, and its absence only leaves the figure out.
	if q, err := c.promVector(ctx, s, `max by (exporter) (otelcol_exporter_queue_size / otelcol_exporter_queue_capacity)`); err == nil {
		for _, r := range q {
			fill := math.Min(math.Max(r.Value, 0), 1)
			switch {
			case strings.HasSuffix(r.Metric["exporter"], "/metrics"):
				out.Metrics.Queue = &fill
			case strings.HasSuffix(r.Metric["exporter"], "/logs"):
				out.Logs.Queue = &fill
			case strings.HasSuffix(r.Metric["exporter"], "/traces"):
				out.Traces.Queue = &fill
			}
		}
	}
	return out, nil
}

// signal is the figures of the kind of telemetry a counter's name is about.
func (g *GatewayTraffic) signal(name string) *SignalTraffic {
	switch {
	case strings.HasSuffix(name, "_metric_points_total"):
		return &g.Metrics
	case strings.HasSuffix(name, "_log_records_total"):
		return &g.Logs
	case strings.HasSuffix(name, "_spans_total"):
		return &g.Traces
	}
	return nil
}

type promSample struct {
	Metric map[string]string
	Value  float64
}

// promVector runs an instant query and returns its samples, leaving out any that is not a number (the rate of a series with one sample).
func (c *Client) promVector(ctx context.Context, s Scope, q string) ([]promSample, error) {
	data, err := c.RawMetricQuery(ctx, s, "query", url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	var d struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	var out []promSample
	for _, r := range d.Result {
		str, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(str, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, promSample{Metric: r.Metric, Value: v})
	}
	return out, nil
}
