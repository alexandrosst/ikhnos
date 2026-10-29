package cli

import (
	"bytes"
	"fmt"
	"os"
)

// SharedSecret is one secret the agent shares with a kind of pod: the flag it is given by, and the file it is read from.
type SharedSecret struct {
	Flag string
	File string
}

// SecretsDiffer fails when two of the shared secrets are the same. Each kind of pod holds one secret, so a pod that is compromised can sign
// only what it is meant to: the node probe's reports, the flow collectors' reports, the prober's request for its peers, the responders' request
// for the iperf3 credentials. Their signatures are also bound to a context of their own, but that is the second line: it only helps while the
// secrets differ, and a chart or a hand-made Secret that gave two of them the same value would undo the whole separation without a sign. A file
// that is not set or cannot be read is not compared; those are reported where the secret is read.
func SecretsDiffer(secrets ...SharedSecret) error {
	var read []SharedSecret
	var values [][]byte
	for _, s := range secrets {
		if s.File == "" {
			continue
		}
		b, err := os.ReadFile(s.File)
		if err != nil {
			continue
		}
		read = append(read, s)
		values = append(values, bytes.TrimSpace(b))
	}
	for i := range read {
		for j := i + 1; j < len(read); j++ {
			if bytes.Equal(values[i], values[j]) {
				reason := why[[2]string{read[i].Flag, read[j].Flag}]
				if reason == "" {
					reason = "one could then sign what only the other is meant to"
				}
				return fmt.Errorf("%s and %s hold the same secret: each kind of pod must have one of its own (%s)", read[i].Flag, read[j].Flag, reason)
			}
		}
	}
	return nil
}

// why says what two pods could do to each other with the same secret, for the pairs where it is worth saying.
var why = map[[2]string]string{
	{"--probe-secret-file", "--flow-secret-file"}:            "a node probe could then forge traffic reports",
	{"--probe-secret-file", "--link-peers-secret-file"}:      "the link prober could then forge node facts",
	{"--probe-secret-file", "--link-iperf-secret-file"}:      "a responder pod could then forge node facts",
	{"--flow-secret-file", "--link-peers-secret-file"}:       "the link prober could then forge traffic reports",
	{"--flow-secret-file", "--link-iperf-secret-file"}:       "a responder pod could then forge traffic reports",
	{"--link-peers-secret-file", "--link-iperf-secret-file"}: "the prober could then read the iperf3 private key",
	{"--link-hub-secret-file", "--link-peers-secret-file"}:   "the prober could then feed the hub rows and a peer list",
	{"--link-hub-secret-file", "--link-iperf-secret-file"}:   "a responder pod could then read or replace the iperf3 credentials",
	{"--link-hub-secret-file", "--flow-secret-file"}:         "a node flow collector could then feed the hub rows and a peer list",
	{"--link-hub-secret-file", "--probe-secret-file"}:        "a node probe could then feed the hub rows and a peer list",
}
