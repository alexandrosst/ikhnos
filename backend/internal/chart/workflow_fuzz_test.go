package chart

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The fuzz workflow keeps the input that made a target fail as an artifact. upload-artifact refuses a path with a "." segment in it, and the
// package a target lives in is written ./internal/x for `go test`; a path built from it ("backend/./internal/x/...") made the upload fail,
// so the one failure the run exists to keep was lost. The matrix carries the directory without the leading ./ for the artifact.
func TestTheFuzzWorkflowUploadsTheFailingInputFromAPathWithNoDotSegment(t *testing.T) {
	wf, err := os.ReadFile("../../../.github/workflows/fuzz.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := regexp.MustCompile(`(?m)^\s*path:\s*(backend/.*testdata/fuzz/.*?)\s*$`).FindStringSubmatch(string(wf))
	if path == nil {
		t.Fatal("the fuzz workflow does not upload the failing input from backend/.../testdata/fuzz/...")
	}
	if strings.Contains(path[1], "matrix.pkg") {
		t.Errorf("the artifact path %q is built from matrix.pkg, which starts with ./ and upload-artifact refuses", path[1])
	}
	if !strings.Contains(path[1], "matrix.dir") {
		t.Errorf("the artifact path %q is not built from matrix.dir", path[1])
	}
	script, err := os.ReadFile("../../../scripts/fuzz-targets.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `"dir":"\1"`) {
		t.Error("scripts/fuzz-targets.sh does not put a dir (the package without ./) in each entry")
	}
}
