package fusion

import (
	"context"
	"errors"

	"ikhnos/internal/kernel"
	"ikhnos/internal/operators"
	"ikhnos/internal/pki"
	"ikhnos/internal/store"
)

// Central is the control as the operators' port for the central operator (operators.Central). It is nil, an interface that holds
// nothing, when there is no control: a method on a nil *FusionControl would answer, but a caller that asks "is there a central
// operator" must be able to tell.
func (f *FusionControl) Central() operators.Central {
	if f == nil {
		return nil
	}
	return central{f}
}

// central is the port. The control keeps the organisation, the namespace and the cluster UID as fields, so they are read through here.
type central struct{ f *FusionControl }

var _ operators.Central = central{}

func (c central) Org() string { return c.f.Org }
func (c central) Managed(ctx context.Context) operators.ManagedStatus {
	return c.f.Status(ctx).Managed()
}
func (c central) Exposed() bool    { return c.f.Exposed() }
func (c central) Endpoint() string { return c.f.CentralEndpoint() }
func (c central) WithGatewayPort(ctx context.Context, addr string) string {
	return c.f.WithGatewayPort(ctx, addr)
}
func (c central) SetPublicAddress(addr string) { c.f.SetPublicAddress(addr) }
func (c central) GatewayService(ctx context.Context) *kernel.KubeService {
	return c.f.GatewayService(ctx)
}
func (c central) ServiceName() string { return c.f.ServiceName() }
func (c central) Namespace() string   { return c.f.Namespace }
func (c central) ClusterUID() string  { return c.f.ClusterUID }

// EnsureCentralOperator makes sure the central operator exists in this organisation and issues a fresh server
// certificate for its receiver (valid for hosts) from the operator's own CA. The first call mints the operator and its
// CA; later calls only reissue the certificate, which is safe because senders trust the CA, not one certificate.
//
// Two first calls can meet - two replicas of the server on one database, or the boot-time renewal beside a click on
// Enable (within one process FusionControl already runs them one at a time). Both see no operator, both mint a CA, and
// the second insert fails on the operator's id. That second caller must not report the raw store error: the operator is
// there, it is just not its own. It throws away the CA it minted - nothing has used it, and a certificate from it
// would not verify against the CA that won - and goes round again as a later call, issuing from the winner's CA.
func (c *Service) EnsureCentralOperator(ctx context.Context, actor string, dest store.Destination, hosts []string) (store.Operator, operators.OperatorTLSBundle, error) {
	op, err := c.Store.GetOperator(ctx, c.OrgID, operators.CentralOperatorID)
	if errors.Is(err, store.ErrNotFound) {
		bundle, caKeyPEM, err := operators.MintOperatorTLS(c.Core, operators.CentralOperatorID, hosts)
		if err != nil {
			return store.Operator{}, operators.OperatorTLSBundle{}, kernel.Errf(kernel.KindInternal, "could not mint the central operator's certificates: %v", err)
		}
		op = store.Operator{
			ID: operators.CentralOperatorID, OrgID: c.OrgID, Name: "Central (FUSION)", Status: store.OperatorActive,
			Destination: dest, ReceiverAuth: store.ReceiverAuthMTLS,
			ClientCACertPEM: bundle.CACertPEM, ClientCAKeyPEM: caKeyPEM,
			CreatedBy: actor, CreatedAt: c.Now(),
		}
		createErr := c.AuditedWrite(c.Store.CreateOperator(ctx, op, nil,
			store.WithAudit(c.AuditRow(c.OrgID, actor, "operator-created", "operator", op.ID, "central (FUSION)"))), "operator-created")
		if createErr == nil {
			return op, bundle, nil
		}
		// Lost the race? Then the operator exists now and is the one to use. Anything else (the audit trail, the
		// database) is the caller's error as it was.
		won, getErr := c.Store.GetOperator(ctx, c.OrgID, operators.CentralOperatorID)
		if errors.Is(getErr, store.ErrNotFound) && errors.Is(createErr, store.ErrExists) {
			// The id is taken and not in this organisation: FUSION is one per server, and another organisation has it.
			return store.Operator{}, operators.OperatorTLSBundle{}, kernel.Errf(kernel.KindConflict, "the central operator belongs to another organisation")
		}
		if getErr != nil {
			return store.Operator{}, operators.OperatorTLSBundle{}, createErr
		}
		op = won
	} else if err != nil {
		return store.Operator{}, operators.OperatorTLSBundle{}, err
	}
	if op.Status != store.OperatorActive {
		return store.Operator{}, operators.OperatorTLSBundle{}, kernel.Errf(kernel.KindConflict, "the central operator was revoked")
	}
	issuer, caPEM, err := c.Operators.OperatorIssuer(ctx, op)
	if err != nil {
		return store.Operator{}, operators.OperatorTLSBundle{}, err
	}
	certPEM, keyPEM, err := issuer.IssueOperatorReceiverTLS(op.ID, c.OrgID, hosts)
	if err != nil {
		return store.Operator{}, operators.OperatorTLSBundle{}, err
	}
	// In the ledger like every other certificate the server issues: it is how the dates shown for the operator, and
	// Renew's "is this still fresh", know what the gateway holds. One that cannot be recorded is not handed out.
	if err := c.Operators.RecordOperatorCert(ctx, actor, op, store.OperatorCertReceiver, "", certPEM); err != nil {
		return store.Operator{}, operators.OperatorTLSBundle{}, err
	}
	c.Operators.NoteCertEnds(ctx, op)
	return op, operators.OperatorTLSBundle{ReceiverCertPEM: certPEM, ReceiverKeyPEM: keyPEM, CACertPEM: caPEM}, nil
}

// centralReceiverFresh says whether the gateway's newest certificate in the ledger still has more than
// pki.OperatorRenewBefore left, so that Renew has nothing to do.
func (c *Service) centralReceiverFresh(ctx context.Context) bool {
	certs, err := c.Store.ListOperatorCerts(ctx, c.OrgID, operators.CentralOperatorID) // newest first
	if err != nil {
		return false
	}
	for _, e := range certs {
		if e.Kind == store.OperatorCertReceiver {
			return e.NotAfter.Sub(c.Now()) > pki.OperatorRenewBefore
		}
	}
	return false
}
