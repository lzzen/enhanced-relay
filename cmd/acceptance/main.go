// Command acceptance is the AI acceptance gate (docs/ai-testing-acceptance.md).
//
// It runs the test suite with `go test -json`, collects requirement<->test
// bindings emitted by internal/testutil/req.Covers, cross-references the
// requirements manifest, and fails if any P0/P1 requirement lacks a passing
// bound test (or if any test failed). It writes machine-readable evidence to
// build/traceability.json and build/acceptance-report.json so humans review the
// report instead of re-running anything by hand.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

type options struct {
	race     bool
	manifest string
	covDir   string
	outDir   string
	pkg      string
}

// goTestEvent is a subset of `go test -json` event fields.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
}

type requirement struct {
	ID       string `json:"id"`
	Priority string `json:"priority"`
	Desc     string `json:"desc"`
}

type manifest struct {
	Requirements []requirement `json:"requirements"`
}

type reqStatus struct {
	ID        string   `json:"id"`
	Priority  string   `json:"priority"`
	Desc      string   `json:"desc"`
	Satisfied bool     `json:"satisfied"`
	Tests     []string `json:"tests"`
}

type report struct {
	Commit       string      `json:"commit"`
	GeneratedAt  string      `json:"generated_at"`
	Race         bool        `json:"race"`
	Tests        testSummary `json:"tests"`
	Requirements reqSummary  `json:"requirements"`
	Pass         bool        `json:"pass"`
}

type testSummary struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type reqSummary struct {
	Total       int         `json:"total"`
	Satisfied   int         `json:"satisfied"`
	MissingP0P1 []string    `json:"missing_p0p1"`
	Details     []reqStatus `json:"details"`
}

func main() {
	opt := parseFlags()
	if err := run(opt); err != nil {
		fmt.Fprintf(os.Stderr, "\nacceptance: FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nacceptance: OK")
}

func parseFlags() options {
	var opt options
	flag.BoolVar(&opt.race, "race", false, "run tests with the race detector (requires CGO)")
	flag.StringVar(&opt.manifest, "manifest", filepath.Join("acceptance", "requirements.json"), "requirements manifest path")
	flag.StringVar(&opt.covDir, "covdir", filepath.Join("build", "coverage"), "coverage output directory")
	flag.StringVar(&opt.outDir, "out", "build", "report output directory")
	flag.StringVar(&opt.pkg, "pkg", "./...", "package pattern to test")
	flag.Parse()
	return opt
}

func run(opt options) error {
	if err := resetDir(opt.covDir); err != nil {
		return err
	}
	if err := os.MkdirAll(opt.outDir, 0o755); err != nil {
		return err
	}

	testPass, summary, err := runTests(opt)
	if err != nil {
		return err
	}

	bindings, err := loadBindings(opt.covDir)
	if err != nil {
		return err
	}

	man, err := loadManifest(opt.manifest)
	if err != nil {
		return err
	}

	rep := buildReport(opt, summary, bindings, man, testPass)

	if err := writeReports(opt.outDir, rep, bindings); err != nil {
		return err
	}

	printSummary(rep)

	if !rep.Pass {
		if !testPass {
			return fmt.Errorf("%d test(s) failed", summary.Failed)
		}
		return fmt.Errorf("%d P0/P1 requirement(s) unsatisfied: %s",
			len(rep.Requirements.MissingP0P1), strings.Join(rep.Requirements.MissingP0P1, ", "))
	}
	return nil
}

// runTests runs `go test -json` streaming results and returns pass/fail plus a
// per-test status summary. status is keyed by "Package.Test".
func runTests(opt options) (bool, testSummary, error) {
	args := []string{"test", "-json", "-count=1"}
	if opt.race {
		args = append(args, "-race")
	}
	args = append(args, opt.pkg)

	cmd := exec.Command("go", args...)
	env := append(os.Environ(), req.CoverageDirEnv+"="+mustAbs(opt.covDir))
	if opt.race {
		env = append(env, "CGO_ENABLED=1")
	}
	cmd.Env = env
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, testSummary{}, err
	}
	if err := cmd.Start(); err != nil {
		return false, testSummary{}, err
	}

	var sum testSummary
	failedTests := map[string]bool{}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var ev goTestEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // non-JSON build noise
		}
		if ev.Test == "" {
			if ev.Action == "fail" {
				fmt.Printf("  FAIL package %s\n", ev.Package)
			}
			continue
		}
		switch ev.Action {
		case "pass":
			sum.Passed++
		case "fail":
			sum.Failed++
			failedTests[ev.Package+"."+ev.Test] = true
			fmt.Printf("  FAIL %s.%s\n", ev.Package, ev.Test)
		case "skip":
			sum.Skipped++
		}
	}
	waitErr := cmd.Wait()

	// A non-zero exit with zero recorded failures means a build/compile error.
	if waitErr != nil && sum.Failed == 0 {
		return false, sum, fmt.Errorf("go test failed to run: %w", waitErr)
	}
	return waitErr == nil, sum, nil
}

// loadBindings reads all per-process coverage files and returns req -> tests.
func loadBindings(covDir string) (map[string][]string, error) {
	out := map[string][]string{}
	files, err := filepath.Glob(filepath.Join(covDir, "cov-*.jsonl"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, fp := range files {
		f, err := os.Open(fp)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var b req.Binding
			if err := json.Unmarshal(sc.Bytes(), &b); err != nil {
				continue
			}
			key := b.Req + "\x00" + b.Test
			if seen[key] {
				continue
			}
			seen[key] = true
			out[b.Req] = append(out[b.Req], b.Test)
		}
		f.Close()
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}

func loadManifest(path string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("read manifest: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // tolerate UTF-8 BOM
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

func buildReport(opt options, sum testSummary, bindings map[string][]string, man manifest, testPass bool) report {
	rep := report{
		Commit:      gitCommit(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Race:        opt.race,
		Tests:       sum,
	}
	rep.Requirements.Total = len(man.Requirements)
	for _, r := range man.Requirements {
		tests := bindings[r.ID]
		satisfied := len(tests) > 0 // a bound test that ran; failures fail the whole gate below
		st := reqStatus{ID: r.ID, Priority: r.Priority, Desc: r.Desc, Satisfied: satisfied, Tests: tests}
		if satisfied {
			rep.Requirements.Satisfied++
		} else if r.Priority == "P0" || r.Priority == "P1" {
			rep.Requirements.MissingP0P1 = append(rep.Requirements.MissingP0P1, r.ID)
		}
		rep.Requirements.Details = append(rep.Requirements.Details, st)
	}
	sort.Strings(rep.Requirements.MissingP0P1)
	rep.Pass = testPass && len(rep.Requirements.MissingP0P1) == 0
	return rep
}

func writeReports(outDir string, rep report, bindings map[string][]string) error {
	if err := writeJSON(filepath.Join(outDir, "acceptance-report.json"), rep); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outDir, "traceability.json"), bindings)
}

func printSummary(rep report) {
	fmt.Printf("\n──────── acceptance summary ────────\n")
	fmt.Printf("tests:         %d passed, %d failed, %d skipped\n", rep.Tests.Passed, rep.Tests.Failed, rep.Tests.Skipped)
	fmt.Printf("requirements:  %d/%d satisfied\n", rep.Requirements.Satisfied, rep.Requirements.Total)
	if len(rep.Requirements.MissingP0P1) > 0 {
		fmt.Printf("MISSING P0/P1: %s\n", strings.Join(rep.Requirements.MissingP0P1, ", "))
	}
	fmt.Printf("race:          %v\n", rep.Race)
	fmt.Printf("report:        build/acceptance-report.json, build/traceability.json\n")
}

// helpers

func resetDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
