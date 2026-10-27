package link

import (
	"os"
	"regexp"
	"testing"
)

// iperf3's server ends each test by cancelling a thread, and glibc then needs libgcc_s.so.1, which the distroless `base` image lacks
// and `cc` has. On `base` the server aborted at the end of its first test ("libgcc_s.so.1 must be installed for pthread_cancel to work")
// and every throughput test counted as a timeout. Nothing else in the build runs the binary, so this keeps the base from being changed back.
func TestTheLinkImageCarriesTheLibraryIperf3ServerNeedsToEndATest(t *testing.T) {
	b, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^FROM\s+(\S+)\s+AS\s+link\s*$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("the Dockerfile has no stage called link")
	}
	if got := string(m[1]); !regexp.MustCompile(`^gcr\.io/distroless/cc-debian\d+(:|@)`).MatchString(got) {
		t.Errorf("the link image is built on %s: it must be a distroless cc image, which carries libgcc_s.so.1", got)
	}
}
