package chart

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Every workflow that installs Helm names its version, and they name the same one. The release workflow once installed "the latest" while CI
// pinned v3.18.6: a Helm that ghcr.io does not take a chart from (it answers "not found" when asked for the manifest it was just given, to tag
// it) was the one that published, and only the release failed, after CI had passed.
func TestEveryWorkflowThatInstallsHelmNamesTheSameVersion(t *testing.T) {
	files, err := filepath.Glob("../../../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found (%v)", err)
	}
	step := regexp.MustCompile(`(?m)^\s*-\s*uses:\s*azure/setup-helm@\S+\s*\n((?:[ \t]+\S.*\n?)*)`)
	version := regexp.MustCompile(`(?m)^\s*version:\s*(\S+)\s*$`)
	seen := map[string][]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range step.FindAllStringSubmatch(string(b), -1) {
			// The step's own keys are the indented lines that follow it, up to the next step.
			own := regexp.MustCompile(`(?m)^\s*-\s`).Split(m[1], 2)[0]
			v := version.FindStringSubmatch(own)
			if v == nil {
				t.Errorf("%s installs Helm without naming a version: it would install the latest", filepath.Base(f))
				continue
			}
			seen[v[1]] = append(seen[v[1]], filepath.Base(f))
		}
	}
	if len(seen) == 0 {
		t.Fatal("no workflow installs Helm: this test looks at the wrong files")
	}
	if len(seen) > 1 {
		t.Errorf("the workflows install different Helm versions: %v", seen)
	}
}
