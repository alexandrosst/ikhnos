package fusionapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTheOrganisationSeriesCarriesTheIdTheTelemetryCarriesAndTheName(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/otlp/v1/metrics" {
			http.NotFound(w, r)
			return
		}
		got, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	c := &Client{Prometheus: srv.URL, Now: func() time.Time { return time.Unix(1791200000, 0) }}
	if err := c.PushOrgInfo(context.Background(), OrgName{ID: "default", Name: "Edge lab"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{OrgInfoMetric, "ikhnos_org_id", "default", "org_name", "Edge lab"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Fatalf("the series does not carry %q", want)
		}
	}
	got = nil
	if err := c.PushOrgInfo(context.Background(), OrgName{ID: "default"}); err != nil || got != nil {
		t.Fatalf("an organisation with no name is not written: %v %v", err, got)
	}
	sel, _ := MetricFilter{}.selector(AllSignals())
	if !bytes.Contains([]byte(sel), []byte(`__name__!="ikhnos_org_info"`)) {
		t.Fatalf("a telemetry read must leave the organisation series out: %s", sel)
	}
}
