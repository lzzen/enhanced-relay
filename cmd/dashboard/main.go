// Command dashboard renders a self-contained HTML monitoring panel from the
// acceptance evidence in build/ (acceptance-report.json, traceability.json and,
// if present, mutation.json). The output build/dashboard.html has no external
// dependencies (works offline, no GitHub) so humans can see CI status at a
// glance instead of reading raw JSON. Regenerated on every `make verify`/`ci`.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir := "build"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := render(dir); err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("dashboard: wrote %s\n", filepath.Join(dir, "dashboard.html"))
}

func render(dir string) error {
	combined := map[string]json.RawMessage{}
	for key, file := range map[string]string{
		"acceptance":   "acceptance-report.json",
		"traceability": "traceability.json",
		"mutation":     "mutation.json",
	} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			if os.IsNotExist(err) {
				continue // optional inputs
			}
			return err
		}
		combined[key] = json.RawMessage(data)
	}
	if _, ok := combined["acceptance"]; !ok {
		return fmt.Errorf("missing %s (run `make verify` first)", filepath.Join(dir, "acceptance-report.json"))
	}

	payload, err := json.Marshal(combined) // json.Marshal escapes <,>,& -> safe to inline
	if err != nil {
		return err
	}
	html := injectData(pageTemplate, string(payload))
	return os.WriteFile(filepath.Join(dir, "dashboard.html"), []byte(html), 0o644)
}

func injectData(tmpl, data string) string {
	const marker = "/*__DATA__*/"
	for i := 0; i+len(marker) <= len(tmpl); i++ {
		if tmpl[i:i+len(marker)] == marker {
			return tmpl[:i] + data + tmpl[i+len(marker):]
		}
	}
	return tmpl
}
