package agent

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// sharedSecret is one secret the agent shares with a kind of pod: the flag it is given by, and the file it is read from.
type sharedSecret struct {
	flag string
	file string
}

// secretsDiffer fails when two of the shared secrets are the same. Each kind of pod holds one secret, so a pod that is compromised can sign
// only what it is meant to: the node probe's reports, the flow collectors' reports, the prober's request for its peers, the responders' request
// for the iperf3 credentials. Their signatures are also bound to a context of their own, but that is the second line: it only helps while the
// secrets differ, and a chart or a hand-made Secret that gave two of them the same value would undo the whole separation without a sign. A file
// that is not set or cannot be read is not compared; those are reported where the secret is read.
func secretsDiffer(secrets ...sharedSecret) error {
	var read []sharedSecret
	var values [][]byte
	for _, s := range secrets {
		if s.file == "" {
			continue
		}
		b, err := os.ReadFile(s.file)
		if err != nil {
			continue
		}
		read = append(read, s)
		values = append(values, bytes.TrimSpace(b))
	}
	for i := range read {
		for j := i + 1; j < len(read); j++ {
			if bytes.Equal(values[i], values[j]) {
				return fmt.Errorf("%s and %s hold the same secret: each kind of pod must have one of its own, or one of them could sign what only the other is meant to (%s)", read[i].flag, read[j].flag, strings.TrimSpace(why[[2]string{read[i].flag, read[j].flag}]))
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
}
