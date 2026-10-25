package estate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/accounts"
	"ikhnos/internal/agents"
	"ikhnos/internal/kernel"
	"ikhnos/internal/store"
)

// ensureTelemetryRequest loads an agent's telemetry request from the store the first time it is needed. Do not hold h.mu.
func (h *Hub) ensureTelemetryRequest(id string, v *View) {
	h.Mu.Lock()
	loaded := v.Ext.TelemetryRequestLoad
	h.Mu.Unlock()
	if loaded {
		return
	}
	var t agents.TelemetryRequest
	var at time.Time
	if data, savedAt, err := h.C.Store.LoadSnapshot(bg(), agents.TelemetryRequestKey(id)); err == nil {
		at = savedAt
		if json.Unmarshal(data, &t) != nil {
			t = agents.TelemetryRequest{}
		} else if c, err := agents.CleanTelemetryRequest(t); err == nil {
			t = c // stored data is not trusted more than a request
		} else {
			t = agents.TelemetryRequest{Revision: t.Revision} // keep the count, so a revision is never used twice
		}
	}
	h.Mu.Lock()
	if !v.Ext.TelemetryRequestLoad {
		v.Ext.TelemetryRequest, v.Ext.TelemetryRequestAt, v.Ext.TelemetryRequestLoad = t, at, true
	}
	h.Mu.Unlock()
}

// TelemetryRequestOf returns what an agent's cluster has been asked to collect.
func (h *Hub) TelemetryRequestOf(id string) agents.TelemetryRequest {
	h.Mu.Lock()
	v := h.ViewFor(id)
	h.Mu.Unlock()
	h.ensureTelemetryRequest(id, v)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	return v.Ext.TelemetryRequest
}

// TelemetryRequestDocFor is the request for one agent by id, next to what the cluster said became of it.
func (h *Hub) TelemetryRequestDocFor(id string) *agents.TelemetryRequestDoc {
	h.Mu.Lock()
	v := h.ViewFor(id)
	h.Mu.Unlock()
	h.ensureTelemetryRequest(id, v)
	h.Mu.Lock()
	defer h.Mu.Unlock()
	return v.telemetryRequestDoc()
}

// telemetryRequestDoc is the request next to the cluster's answer. The caller holds h.mu and has loaded the request. Nil when nothing was
// ever asked.
func (v *View) telemetryRequestDoc() *agents.TelemetryRequestDoc {
	if v.Ext.TelemetryRequest.Revision == 0 {
		return nil
	}
	setAt := ""
	if !v.Ext.TelemetryRequestAt.IsZero() {
		setAt = kernel.RFC(v.Ext.TelemetryRequestAt)
	}
	return agents.TelemetryRequestDocOf(v.Ext.TelemetryRequest, v.Ext.TelemetryRelay, setAt)
}

// SetTelemetryRequest asks an approved agent's cluster to collect what in names, or takes the request back (an request that asks for
// nothing). Turning anything on takes an administrator; turning things off or down does not. The server only keeps the request and puts
// it in the agent's Config: the cluster's owner decides in the cluster whether it is taken (see agents.TelemetryRequest).
func (h *Hub) SetTelemetryRequest(ctx context.Context, actor, agentID string, in agents.TelemetryRequest) (*agents.TelemetryRequestDoc, error) {
	a, err := h.C.AgentInOrg(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if a.Status != store.StatusApproved {
		return nil, kernel.Errf(kernel.KindConflict, "agent is %s: only an approved agent's telemetry can be asked for", a.Status)
	}
	next, err := agents.CleanTelemetryRequest(in)
	if err != nil {
		return nil, err
	}
	cur := h.TelemetryRequestOf(a.ID)
	if !accounts.Decide(accounts.CallerFrom(ctx), accounts.ActChangeAccess, accounts.Resource{Widening: agents.TelemetryWidens(cur, next)}).Allowed {
		return nil, kernel.Errf(kernel.KindForbidden, "asking a cluster to collect more takes an administrator; asking it to collect less does not")
	}
	if agents.SameTelemetryRequest(cur, next) {
		return h.TelemetryRequestDocFor(a.ID), nil // nothing changed: the revision stays, and nothing is sent again
	}
	next.Revision = cur.Revision + 1
	detail := fmt.Sprintf("%q: telemetry request revision %d (%s)", a.Name, next.Revision, describeTelemetryRequest(next))
	now := h.C.Now()
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if err := h.C.AuditedWrite(h.C.Store.SaveSnapshot(ctx, agents.TelemetryRequestKey(a.ID), data, now,
		store.WithAudit(h.C.AuditRow(h.C.OrgID, actor, "agent-telemetry-requested", "agent", a.ID, detail))), "agent-telemetry-requested"); err != nil {
		return nil, err
	}
	h.Mu.Lock()
	v := h.ViewFor(a.ID)
	v.Ext.TelemetryRequest, v.Ext.TelemetryRequestAt, v.Ext.TelemetryRequestLoad = next, now, true
	h.Mu.Unlock()
	h.C.Agents.RecordAgentGraph(ctx, a, "agent-telemetry-requested", detail)
	h.PushOne(a.ID)
	return h.TelemetryRequestDocFor(a.ID), nil
}

// describeTelemetryRequest says in a few words what a request asks for, for the audit trail.
func describeTelemetryRequest(t agents.TelemetryRequest) string {
	if !t.Requests() {
		return "taken back: the cluster's own settings stand"
	}
	out := ""
	if s := t.Signals; s != nil {
		on := 0
		for _, b := range []bool{s.ResourceUsage, s.Energy, s.KubernetesState, s.NodeRuntime, s.NetworkLatency, s.ClusterLinks, s.ApplicationMetrics, s.SystemLogs, s.KubernetesEvents, s.ApplicationLogs, s.Traces, s.Accelerators} {
			if b {
				on++
			}
		}
		out = fmt.Sprintf("%d of 12 signals on", on)
	}
	if p := t.TracesSamplingPercent; p != nil {
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%d%% of traces", *p)
	}
	return out
}

// noteTelemetryRelay records what an agent's heartbeat says became of the request. The caller holds h.mu.
func (v *View) noteTelemetryRelay(r *ikhnosv1.TelemetryRelayStatus) {
	if r == nil {
		return
	}
	v.Ext.TelemetryRelay = agents.CleanRelayState(r)
}
