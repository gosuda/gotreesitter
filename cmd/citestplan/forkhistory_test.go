package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func historyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=CI fixture", "-c", "user.email=ci@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func historyRepository(t *testing.T, message string) string {
	t.Helper()
	dir := t.TempDir()
	historyGit(t, dir, "init", "-q")
	historyGit(t, dir, "commit", "--allow-empty", "-qm", message)
	return dir
}

func historyStep(t *testing.T, job, name string) string {
	t.Helper()
	for _, step := range readGateWorkflow(t).Jobs[job].Steps {
		if step.Name == name {
			return step.Run
		}
	}
	t.Fatalf("%s is missing %q", job, name)
	return ""
}

func runHistoryStep(dir, script string) ([]byte, error) {
	command := exec.Command("bash", "-c", script)
	command.Dir = dir
	return command.CombinedOutput()
}

func TestForkAPIBaseline(t *testing.T) {
	script := historyStep(t, "apidiff", "Fetch the released API baseline")
	const upstream = "https://github.com/odvcencio/gotreesitter.git"
	const released = "d8f15566820622a62ab33c035814c76497b695ea"
	if !strings.Contains(script, released) || !strings.Contains(script, "v0.57.0") {
		t.Fatal("fork fallback must identify the documented, immutable v0.57.0 release")
	}
	origin := historyRepository(t, "fork")
	remote := historyRepository(t, "released upstream API")
	baseline := historyGit(t, remote, "rev-parse", "HEAD")
	for _, ownTag := range []bool{false, true} {
		t.Run(map[bool]string{false: "fork without tags", true: "fork release wins"}[ownTag], func(t *testing.T) {
			dir := historyRepository(t, "candidate")
			historyGit(t, dir, "remote", "add", "origin", origin)
			url := remote
			if ownTag {
				historyGit(t, dir, "tag", "v0.80.0")
				url = filepath.Join(t.TempDir(), "unavailable-upstream")
			}
			historyGit(t, dir, "config", "url."+url+".insteadOf", upstream)
			fixture := strings.ReplaceAll(script, released, baseline)
			if output, err := runHistoryStep(dir, fixture); err != nil {
				t.Fatalf("release preparation: %v\n%s", err, output)
			}
			if ownTag {
				if tags := historyGit(t, dir, "tag", "--list"); tags != "v0.80.0" {
					t.Fatalf("upstream fallback changed fork release tags: %s", tags)
				}
			} else if got := historyGit(t, dir, "rev-parse", "v0.57.0^{commit}"); got != baseline {
				t.Fatalf("release baseline = %s, want pinned %s", got, baseline)
			}
		})
	}
	dir := historyRepository(t, "failed baseline fetch")
	historyGit(t, dir, "remote", "add", "origin", origin)
	historyGit(t, dir, "config", "url."+filepath.Join(t.TempDir(), "missing")+".insteadOf", upstream)
	if output, err := runHistoryStep(dir, script); err == nil {
		t.Fatalf("an unavailable pinned release must fail: %s", output)
	}
}

func TestForkHistoricalReceiptObjects(t *testing.T) {
	script := historyStep(t, "compact-route-lifecycle-history", "Fetch retired historical receipt commits")
	if strings.Contains(script, "--depth") {
		t.Fatal("historical proof preparation must preserve full ancestry")
	}
	revisions := regexp.MustCompile(`[0-9a-f]{40}`).FindAllString(script, -1)
	if len(revisions) != 6 {
		t.Fatalf("historical fetch lists %d revisions, want the six unreachable originals", len(revisions))
	}
	remote := historyRepository(t, "retired receipt")
	receipt := historyGit(t, remote, "rev-parse", "HEAD")
	fixture := script
	for _, revision := range revisions {
		fixture = strings.ReplaceAll(fixture, revision, receipt)
	}
	const upstream = "https://github.com/odvcencio/gotreesitter.git"
	dir := historyRepository(t, "fork without receipt")
	historyGit(t, dir, "config", "url."+remote+".insteadOf", upstream)
	if output, err := runHistoryStep(dir, fixture); err != nil {
		t.Fatalf("missing historical object fetch: %v\n%s", err, output)
	}
	historyGit(t, dir, "cat-file", "-e", receipt+"^{commit}")
	if got := historyGit(t, dir, "rev-parse", "--is-shallow-repository"); got != "false" {
		t.Fatalf("historical preparation made the repository shallow: %s", got)
	}
	historyGit(t, dir, "config", "--unset", "url."+remote+".insteadOf")
	historyGit(t, dir, "config", "url."+filepath.Join(t.TempDir(), "missing")+".insteadOf", upstream)
	if output, err := runHistoryStep(dir, fixture); err != nil {
		t.Fatalf("available receipt should not need the network: %v\n%s", err, output)
	}
	missing := historyRepository(t, "missing receipt and remote")
	historyGit(t, missing, "config", "url."+filepath.Join(t.TempDir(), "missing")+".insteadOf", upstream)
	if output, err := runHistoryStep(missing, fixture); err == nil {
		t.Fatalf("an unavailable receipt must fail: %s", output)
	}
}
