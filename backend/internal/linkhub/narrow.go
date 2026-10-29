package linkhub

import (
	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// Narrow cuts a row down to the fields the link series are made of: where the connection ended and how it went. It is a copy, so the agent's own
// row is untouched. The fields it drops are the ones that say what in the cluster the traffic belonged to; a test sets every field of a row and
// holds that what is left produces the same series as the whole row (TestNarrowingKeepsEverythingTheSeriesAreMadeOf).
func Narrow(r *ikhnosv1.RawFlow) *ikhnosv1.RawFlow {
	if r == nil {
		return nil
	}
	return &ikhnosv1.RawFlow{
		Client:            r.Client,
		LocalIp:           r.LocalIp,
		PeerIp:            r.PeerIp,
		Port:              r.Port,
		Connections:       r.Connections,
		BytesOut:          r.BytesOut,
		BytesIn:           r.BytesIn,
		SegsOut:           r.SegsOut,
		Retransmits:       r.Retransmits,
		RtoRetransmits:    r.RtoRetransmits,
		BufferDrops:       r.BufferDrops,
		RttUs:             r.RttUs,
		FailedAttempts:    r.FailedAttempts,
		FailedRefused:     r.FailedRefused,
		FailedTimeout:     r.FailedTimeout,
		FailedReset:       r.FailedReset,
		FailedUnreachable: r.FailedUnreachable,
	}
}
