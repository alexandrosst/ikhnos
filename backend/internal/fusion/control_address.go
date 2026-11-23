package fusion

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"ikhnos/internal/accounts"
	"ikhnos/internal/kernel"
	"ikhnos/internal/operators"
	"ikhnos/internal/store"
)

func (f *FusionControl) CentralHost() string { return f.Name + "-central." + f.Namespace + ".svc" }

// CentralEndpoint is what an exporting regional operator is pointed at: the public address when there is one, else
// the gateway's in-cluster Service (OTLP gRPC).
func (f *FusionControl) CentralEndpoint() string {
	if a := f.publicAddress(); a != "" {
		return a
	}
	return f.CentralHost() + ":4317"
}

// Exposed is whether operators in other clusters can reach the gateway.
func (f *FusionControl) Exposed() bool { return f.publicAddress() != "" }

func (f *FusionControl) publicAddress() string {
	f.addrMu.RLock()
	defer f.addrMu.RUnlock()
	return f.PublicAddress
}

// setPublicAddressIfUnset is how the server learns an address: it never replaces one that was set meanwhile (by the
// administrator through SetPublicAddress, which always wins).
func (f *FusionControl) setPublicAddressIfUnset(addr string) {
	f.addrMu.Lock()
	if f.PublicAddress == "" {
		f.PublicAddress = addr
	}
	f.addrMu.Unlock()
}

// SetPublicAddress records where other clusters reach the gateway ("" = this cluster only). It is what the admin
// types under Reachable at on the central operator; the server's startup flag (--fusion-central-address) is only the
// first value. Nothing is reissued: senders verify the gateway by its stable name (operatorServerName).
func (f *FusionControl) SetPublicAddress(addr string) {
	f.addrMu.Lock()
	f.PublicAddress = addr
	f.addrMu.Unlock()
}

// How often the gateway's Service is looked at while it has no address (a cloud takes a minute or two to give a
// LoadBalancer one, so asking more often finds nothing new).
const fusionAddressEvery = 30 * time.Second

// DiscoverAddress records where other clusters can reach the gateway, when the gateway's Service says so (a
// LoadBalancer that has an address, or a NodePort) and nobody has recorded one: it is what saves the administrator the
// `kubectl get svc` and the typing. An address recorded by hand always wins and is never replaced, not even by a
// different one the Service now shows. It returns the address it recorded, "" if it recorded none; it is cheap to call
// often (the Service is read at most every fusionAddressEvery, and only while there is nothing recorded).
func (f *FusionControl) DiscoverAddress(ctx context.Context, c *Service) (string, error) {
	if f == nil || f.Kube == nil || (f.Org != "" && c.OrgID != f.Org) || f.publicAddress() != "" {
		return "", nil
	}
	f.discoverMu.Lock()
	defer f.discoverMu.Unlock()
	if now := f.Clock(); !f.discoverAt.IsZero() && now.Sub(f.discoverAt) < fusionAddressEvery {
		return "", nil
	}
	f.discoverAt = f.Clock()
	op, err := c.Store.GetOperator(ctx, c.OrgID, operators.CentralOperatorID)
	if err != nil || op.Status != store.OperatorActive {
		return "", nil // not set up yet (or revoked): there is nothing to record an address on
	}
	if op.Address != "" { // recorded by hand, here or by another replica: the server only has to learn it
		f.setPublicAddressIfUnset(op.Address)
		return "", nil
	}
	addr, err := f.Kube.ServiceAddress(ctx, f.ServiceName())
	if err != nil {
		if errors.Is(err, kernel.ErrKubeNotFound) || errors.Is(err, kernel.ErrKubeForbidden) { // an older Role cannot read it: no address, no complaint
			return "", nil
		}
		return "", err
	}
	if addr == "" {
		return "", nil
	}
	// Validated, audited and refused for the same reasons as the administrator's "Reachable at", but written only
	// while no address is recorded: the look at the operator above and this write are not one step, and a person
	// who typed an address in between must keep it (in the database and in what this server answers with).
	current, wrote, err := c.Operators.RecordOperatorAddressIfUnset(ctx, accounts.SystemActor.String(), operators.CentralOperatorID, addr)
	if err != nil {
		return "", err
	}
	f.setPublicAddressIfUnset(current)
	if !wrote {
		return "", nil
	}
	return current, nil
}

// DiscoverAddressSoon is DiscoverAddress off the request path: for the screens that notice the gateway has no address
// yet. A failure is logged and tried again later.
func (f *FusionControl) DiscoverAddressSoon(c *Service) {
	if f == nil || f.Kube == nil || f.publicAddress() != "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), fusionReadTimeout)
		defer cancel()
		if _, err := f.DiscoverAddress(ctx, c); err != nil {
			c.Log.Warn("could not record the FUSION gateway's address", "err", err)
		}
	}()
}

// fusionServiceEvery is how long a read of the gateway's Service is reused: it changes when somebody edits the
// release, not from one second to the next, and every screen that shows the address asks.
const fusionServiceEvery = 15 * time.Second

// GatewayService is the gateway's own Service, so an address recorded for it can be compared with what the Service
// really publishes; nil when that cannot be told (this server's Role cannot read Services, the Service is not there, or
// the KubeAPI is one that cannot say), in which case nothing is claimed either way.
func (f *FusionControl) GatewayService(ctx context.Context) *kernel.KubeService {
	if f == nil || f.Kube == nil {
		return nil
	}
	insp, ok := f.Kube.(kernel.ServiceInspector)
	if !ok {
		return nil
	}
	f.svcMu.Lock()
	defer f.svcMu.Unlock()
	if !f.svcAt.IsZero() && f.Clock().Sub(f.svcAt) < fusionServiceEvery {
		return f.svc
	}
	cctx, cancel := context.WithTimeout(ctx, fusionKubeTimeout)
	defer cancel()
	svc, err := insp.Service(cctx, f.ServiceName())
	f.svcAt = f.Clock()
	if err != nil {
		f.svc = nil
		return nil
	}
	f.svc = &svc
	return f.svc
}

// WithGatewayPort completes an address typed without a port. Anything with a port is returned as it is. A bare host gets the
// port the gateway really listens on from outside: the Service's node port when it is a NodePort (nothing listens on 4317
// there: the receiver's own port is only the Service's), and otherwise the receiver's, which is what validOperatorAddress
// adds when it is left alone.
func (f *FusionControl) WithGatewayPort(ctx context.Context, addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	var ae *net.AddrError
	if _, _, err := net.SplitHostPort(addr); !errors.As(err, &ae) || !strings.Contains(ae.Err, "missing port") {
		return addr
	}
	if svc := f.GatewayService(ctx); svc != nil && svc.Type == "NodePort" && svc.NodePort != 0 {
		return net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(svc.NodePort))
	}
	return addr
}

// ServiceName is the gateway's Service (what `kubectl get svc` names to read its address).
func (f *FusionControl) ServiceName() string { return f.Name + "-central" }

// certHosts are the names the gateway's server certificate must carry: its Service in every spelling, the stable name
// senders verify it by, and the public address's host.
func (f *FusionControl) certHosts() []string {
	// The last name is the one that never changes (operatorServerName): senders that reach the gateway at its public
	// address verify against it, so the address, or the IP behind it, can change without reissuing anything.
	h := []string{f.Name + "-central", f.Name + "-central." + f.Namespace, f.CentralHost(), f.CentralHost() + ".cluster.local",
		operators.OperatorServerName(store.Operator{ID: operators.CentralOperatorID})}
	if pa := f.publicAddress(); pa != "" {
		host := pa
		if i := strings.LastIndex(host, ":"); i >= 0 && !strings.HasSuffix(host, "]") {
			host = host[:i]
		}
		h = append(h, strings.Trim(host, "[]"))
	}
	return h
}

// ForgetGatewayService drops the remembered read of the gateway's Service, so the next ask reads it again.
func (f *FusionControl) ForgetGatewayService() {
	f.svcMu.Lock()
	f.svcAt = time.Time{}
	f.svcMu.Unlock()
}
