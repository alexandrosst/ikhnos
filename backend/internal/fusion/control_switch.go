package fusion

import (
	"context"
	"errors"
	"strings"

	"ikhnos/internal/accounts"
	"ikhnos/internal/kernel"
	"ikhnos/internal/operators"
)

// OrgCore is the core FUSION's own work runs on: the main organisation's view of the platform-wide core. The central
// operator, its CA and its certificate live in that one organisation (see Org), and the checks in Enable, Disable and
// Renew compare the core's OrgID with it. Work that has no signed-in user to take the organisation from - the daily
// renewal started at boot - is handed the platform core (OrgID ""), which would fail that comparison and silently do
// nothing, so it goes through here first. A core that already is some organisation's is returned as it is, and the
// comparison then decides; with no Org set there is nothing to scope to.
func (f *FusionControl) OrgCore(c *Service) *Service {
	if f == nil || f.Org == "" || c == nil || c.OrgID != "" {
		return c
	}
	return c.ForOrg(f.Org)
}

// writeOrgSetting puts the organisation FUSION belongs to in its settings (central.orgId), which the gateway reads when it starts
// and stamps on everything it takes in as ikhnos.org.id. Best effort: without it the senders' own stamp stands, which is the same
// organisation's (only its senders can reach the gateway), so nothing is refused for want of it.
func (f *FusionControl) writeOrgSetting(ctx context.Context) {
	if f.Org == "" {
		return
	}
	rk, why := f.retentionKube()
	if why != "" {
		return
	}
	// Only into settings that exist (a FUSION the server manages); one that is not there is not created here.
	if cur, err := rk.Settings(ctx, f.settingsName()); err != nil || cur[OrgSettingKey] == f.Org {
		return
	}
	_ = rk.PatchSettings(ctx, f.settingsName(), map[string]string{OrgSettingKey: f.Org})
}

// OrgSettingKey is the key of FUSION's settings that holds the organisation the gateway stamps.
const OrgSettingKey = "central.orgId"

// Enable gets the central operator and its certificates ready, then starts the four workloads. Safe to repeat.
func (f *FusionControl) Enable(ctx context.Context, c *Service, actor string) (FusionStatus, error) {
	if f != nil { // a nil switch answers "not available" below, without touching anything
		f.switchMu.Lock()
		defer f.switchMu.Unlock()
	}
	prev := f.Status(ctx)
	if !prev.Available {
		return prev, kernel.Errf(kernel.KindConflict, "%s", prev.Message)
	}
	if f.Org != "" && c.OrgID != f.Org {
		return FusionStatus{}, kernel.Errf(kernel.KindForbidden, "FUSION belongs to this server's main organisation, which is the only one that can turn it on")
	}
	_, bundle, err := c.EnsureCentralOperator(ctx, actor, f.destination(), f.certHosts())
	if err != nil {
		return FusionStatus{}, err
	}
	if err := f.Kube.PatchSecret(ctx, f.TLSSecretName(), map[string][]byte{
		"tls.crt": bundle.ReceiverCertPEM, "tls.key": bundle.ReceiverKeyPEM, "ca.crt": bundle.CACertPEM,
	}); err != nil {
		return FusionStatus{}, kubeFail("put the gateway's certificate in place", err)
	}
	f.mu.Lock()
	f.since, f.notReady = f.Clock(), f.Clock()
	f.issuedFor = strings.Join(f.certHosts(), ",")
	f.mu.Unlock()
	f.writeOrgSetting(ctx) // before the gateway starts, which is when it reads it
	st, err := f.scaleAndReport(ctx, prev, 1)
	if err != nil {
		return FusionStatus{}, err
	}
	c.Audit(ctx, actor, "fusion-enabled", "fusion", f.Name, "")
	return st, nil
}

// Renew reissues the gateway's server certificate and puts it in its Secret, if FUSION is on and has been set up (the
// central operator exists). The certificate is good for 30 days and a gateway that is running keeps the one it started
// with, so without this FUSION would stop accepting senders once it expired; with it, the certificate is replaced with
// pki.OperatorRenewBefore left and, because the gateway reloads its certificate files, the running gateway takes it up on
// its own. It also gives the certificate the names the server now knows (a public address set after FUSION was first
// enabled). The server owns that cluster, so this is in-process, not the renewer the other operators' certificates use.
// It runs daily and acts only when there is something to do. Nothing is audited: it changes no one's access.
func (f *FusionControl) Renew(ctx context.Context, c *Service) error {
	if f == nil || f.Kube == nil || (f.Org != "" && c.OrgID != f.Org) {
		return nil
	}
	f.switchMu.Lock()
	defer f.switchMu.Unlock()
	if st := f.Status(ctx); !st.Available || st.State == StateOff {
		return nil
	}
	if _, err := c.Store.GetOperator(ctx, c.OrgID, operators.CentralOperatorID); err != nil {
		return nil // never enabled from here: nothing to renew
	}
	f.writeOrgSetting(ctx) // (for a FUSION turned on before the gateway stamped the organisation: it is read at its next start)
	// Only when there is something to do: the certificate is within pki.OperatorRenewBefore of its end, or the names it
	// must carry changed since this process last issued it (a process that has issued nothing yet issues once).
	hosts := f.certHosts()
	key := strings.Join(hosts, ",")
	f.mu.Lock()
	same := f.issuedFor == key
	f.mu.Unlock()
	if same && c.centralReceiverFresh(ctx) {
		return nil
	}
	_, bundle, err := c.EnsureCentralOperator(ctx, accounts.SystemActor.String(), f.destination(), hosts)
	if err != nil {
		return err
	}
	if err := f.Kube.PatchSecret(ctx, f.TLSSecretName(), map[string][]byte{
		"tls.crt": bundle.ReceiverCertPEM, "tls.key": bundle.ReceiverKeyPEM, "ca.crt": bundle.CACertPEM,
	}); err != nil {
		return err
	}
	f.mu.Lock()
	f.issuedFor = key
	f.mu.Unlock()
	return nil
}

// Disable stops the four workloads. Their volumes stay, so turning FUSION on again brings the data back.
func (f *FusionControl) Disable(ctx context.Context, c *Service, actor string) (FusionStatus, error) {
	if f != nil { // a nil switch answers "not available" below, without touching anything
		f.switchMu.Lock()
		defer f.switchMu.Unlock()
	}
	prev := f.Status(ctx)
	if !prev.Available {
		return prev, kernel.Errf(kernel.KindConflict, "%s", prev.Message)
	}
	if f.Org != "" && c.OrgID != f.Org {
		return FusionStatus{}, kernel.Errf(kernel.KindForbidden, "FUSION belongs to this server's main organisation, which is the only one that can turn it off")
	}
	st, err := f.scaleAndReport(ctx, prev, 0)
	if err != nil {
		return FusionStatus{}, err
	}
	c.Audit(ctx, actor, "fusion-disabled", "fusion", f.Name, "")
	return st, nil
}

// scaleAndReport sets every workload to replicas and answers with the state that results, without reading the cluster
// again: what is wanted is known (every workload there is, at replicas) and what is ready can only be what was ready
// before, capped. It does this only once the scale calls have returned, and what was kept from before them is dropped
// first, so no screen shows the old state while the change is under way. If a call failed part way the cluster is in a
// state this does not know, so nothing is kept and the next ask reads it.
func (f *FusionControl) scaleAndReport(ctx context.Context, prev FusionStatus, replicas int) (FusionStatus, error) {
	f.Invalidate()
	if err := f.ScaleAll(ctx, replicas); err != nil {
		f.Invalidate()
		return FusionStatus{}, err
	}
	if len(prev.Components) == 0 { // the read before it failed: there is nothing to build the answer from
		return f.Status(ctx), nil
	}
	comps := make([]FusionComponent, len(prev.Components))
	for i, c := range prev.Components {
		c.Desired, c.Ready, c.Reason, c.hard = replicas, min(c.Ready, replicas), "", false
		comps[i] = c
	}
	st := f.assess(comps)
	st.LastDataAt = f.rememberedLastData()
	f.keep(st)
	return st, nil
}

// scaleAll sets every workload, the stores before the gateway on the way up and the gateway first on the way down, so
// nothing is ever sending into something that is not there.
func (f *FusionControl) ScaleAll(ctx context.Context, replicas int) error {
	ws := f.stores()
	if replicas == 0 {
		for i, j := 0, len(ws)-1; i < j; i, j = i+1, j-1 {
			ws[i], ws[j] = ws[j], ws[i]
		}
	}
	for _, s := range ws {
		err := f.Kube.Scale(ctx, s.Kind, s.Name, replicas)
		if s.Optional && (errors.Is(err, kernel.ErrKubeNotFound) || errors.Is(err, kernel.ErrKubeForbidden)) {
			continue
		}
		if err != nil {
			return kubeFail("scale "+s.Label, err)
		}
	}
	return nil
}

func kubeFail(what string, err error) error {
	if errors.Is(err, kernel.ErrKubeForbidden) {
		return kernel.Errf(kernel.KindForbidden, "this server is not allowed to %s. Check fusionControl.enabled in the server chart", what)
	}
	return kernel.Errf(kernel.KindConflict, "could not %s: %v", what, err)
}
