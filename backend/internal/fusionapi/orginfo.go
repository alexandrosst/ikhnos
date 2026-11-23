package fusionapi

import (
	"context"
	"math"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// The organisation info series: what lets a dashboard, or a query, show the organisation by the name people gave it, where the
// telemetry carries only its id (ikhnos_org_id, stamped by every agent). The server writes, with the application and cluster series,
//
//	ikhnos_org_info{ikhnos_org_id, org_name} 1
//
// so a query joins on the id:
//
//	sum by (org_name) (rate(http_server_request_duration_seconds_count[5m]) * on (ikhnos_org_id) group_left (org_name) topk by (ikhnos_org_id) (1, ikhnos_org_info))
//
// An organisation renamed in ikhnos is written under its new name within a minute; the old name's series stays for Prometheus'
// five-minute lookback, which is why the join takes one series per id (topk), as the dashboards do for the cluster names.

// OrgInfoMetric is the name of the organisation info series.
const OrgInfoMetric = "ikhnos_org_info"

// OrgName is an organisation's id and the name people know it by.
type OrgName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrgNamer is what an Extras that knows FUSION's organisation adds to it.
type OrgNamer interface {
	OrgName(ctx context.Context) (OrgName, error)
}

// EncodeOrgInfo writes the series as an OTLP/HTTP metrics request body, a gauge of value 1 stamped at (see EncodeAppInfo).
func EncodeOrgInfo(o OrgName, at time.Time) []byte {
	var dp []byte
	for _, kv := range [][2]string{{vocOrg.Name(), o.ID}, {"org_name", o.Name}} {
		dp = protowire.AppendTag(dp, 7, protowire.BytesType) // attributes
		dp = protowire.AppendBytes(dp, keyValue(kv[0], kv[1]))
	}
	dp = protowire.AppendTag(dp, 3, protowire.Fixed64Type) // time_unix_nano
	dp = protowire.AppendFixed64(dp, uint64(at.UnixNano()))
	dp = protowire.AppendTag(dp, 4, protowire.Fixed64Type) // as_double
	dp = protowire.AppendFixed64(dp, math.Float64bits(1))
	var points []byte
	points = protowire.AppendTag(points, 1, protowire.BytesType) // Gauge.data_points
	points = protowire.AppendBytes(points, dp)
	return gaugeRequest(OrgInfoMetric, "The name of the organisation FUSION belongs to, by the id the telemetry carries (ikhnos_org_id).", points)
}

// PushOrgInfo writes the organisation info series. An organisation with no id or no name is not written.
func (c *Client) PushOrgInfo(ctx context.Context, o OrgName) error {
	if o.ID == "" || o.Name == "" {
		return nil
	}
	return c.pushOTLP(ctx, EncodeOrgInfo(o, c.now()), "the organisation series")
}
