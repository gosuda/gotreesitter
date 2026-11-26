package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBenchmarkCompletionAndAdvisoryDiagnostics(t *testing.T) {
	script := historyStep(t, "perf-regression", "Benchmark correctness and advisory thresholds")
	for _, tc := range []struct {
		name        string
		campaign    int
		baseRSS     int
		headRSS     int
		threshold   int
		attribution int
		wantExit    int
		wantWarning string
	}{
		{name: "all complete"},
		{name: "diagnostic unavailable", attribution: 42, wantWarning: "::warning::edit attribution is unavailable"},
		{name: "benchmark execution fails", campaign: 43, wantExit: 43},
		{name: "base RSS execution fails", baseRSS: 44, wantExit: 44},
		{name: "head RSS execution fails", headRSS: 45, wantExit: 45},
		{name: "threshold regression", threshold: 1, wantWarning: "::warning::performance thresholds exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"time": "#!/bin/bash\nset -euo pipefail\ntest \"$1\" = -v\nshift\nexec \"$@\"\n",
				"git": `#!/bin/bash
set -euo pipefail
case "$1" in
  fetch)
    case "$4" in contract|legacy) exit 1;; esac
    ;;
  worktree)
    if [ "$2" = add ]; then mkdir -p "$3"; fi
    ;;
  *) exit 65;;
esac
`,
				"bash": `#!/bin/bash
set -euo pipefail
case "$1" in
  scripts/run_randomized_benchmarks.sh)
    printf 'campaign\n' >> "$CALLS"
    exit "$CAMPAIGN_EXIT"
    ;;
  scripts/run_go_edit_attribution.sh)
    printf 'attribution\n' >> "$CALLS"
    exit "$ATTRIBUTION_EXIT"
    ;;
  *) exit 66;;
esac
`,
				"go": `#!/bin/bash
set -euo pipefail
if [ "$1" = test ] && [ "$2" = -c ] && [ "$3" = -o ]; then
  if [ "$PWD" -ef "$RUNNER_TEMP" ]; then
    label=head
    status=$HEAD_RSS_EXIT
  else
    label=base
    status=$BASE_RSS_EXIT
  fi
  printf '#!/bin/bash\nprintf "rss %s\\n" >> "$CALLS"\nexit %s\n' "$label" "$status" > "$4"
  chmod +x "$4"
elif [ "$1" = run ] && [ "$2" = ./cmd/benchgate ]; then
  exit "$THRESHOLD_EXIT"
else
  exit 67
fi
`,
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("/bin/bash", "-c", strings.ReplaceAll(script, "/usr/bin/time", filepath.Join(bin, "time")))
			command.Dir = dir
			command.Env = append(os.Environ(),
				"PATH="+bin+":"+os.Getenv("PATH"),
				"BASE_SHA=base", "PERF_BENCH_CONTRACT_BASE_SHA=contract", "PERF_LEGACY_BASE_SHA=legacy",
				"RUNNER_TEMP="+dir, "GITHUB_STEP_SUMMARY="+filepath.Join(dir, "summary"), "CALLS="+filepath.Join(dir, "calls"),
				"CAMPAIGN_EXIT="+strconv.Itoa(tc.campaign),
				"BASE_RSS_EXIT="+strconv.Itoa(tc.baseRSS), "HEAD_RSS_EXIT="+strconv.Itoa(tc.headRSS),
				"THRESHOLD_EXIT="+strconv.Itoa(tc.threshold), "ATTRIBUTION_EXIT="+strconv.Itoa(tc.attribution))
			output, err := command.CombinedOutput()
			exitCode := 0
			if err != nil {
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) {
					t.Fatal(err)
				}
				exitCode = exitError.ExitCode()
			}
			if exitCode != tc.wantExit {
				t.Fatalf("step exit = %d, want %d: %s", exitCode, tc.wantExit, output)
			}
			if tc.wantWarning != "" && !strings.Contains(string(output), tc.wantWarning) {
				t.Fatalf("advisory failure lost its warning: %s", output)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			for label, want := range map[string]bool{"base": tc.campaign == 0, "head": tc.campaign == 0 && tc.baseRSS == 0} {
				if ran := strings.Contains(string(calls), "rss "+label+"\n"); ran != want {
					t.Fatalf("mandatory %s RSS invocation mismatch: %s", label, calls)
				}
			}
			if ran := strings.Contains(string(calls), "attribution\n"); ran != (tc.wantExit == 0) {
				t.Fatalf("diagnostic must follow completed benchmark work: %s", calls)
			}
		})
	}
}
