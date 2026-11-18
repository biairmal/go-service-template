package app

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitecture keeps modules independent: no module imports another
// module, and platform imports neither app nor a module.
func TestArchitecture(t *testing.T) {
	trackSources(t)
	// Ask go for the module path: gonew rewrites imports, not string literals.
	mod, err := exec.Command("go", "list", "-f", "{{.Module.Path}}", ".").Output()
	if err != nil {
		t.Fatalf("go list module: %v", err)
	}
	prefix := strings.TrimSpace(string(mod)) + "/internal/"
	out, err := exec.Command("go", "list", "-f",
		`{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}`,
		"../...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	imports := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		imports[f[0]] = f[1:]
	}
	for _, v := range violations(prefix, imports) {
		t.Error(v)
	}
}

// trackSources stats every Go file under internal/. go list runs in a child
// process, which the test cache cannot see; touching the files here makes a
// new or edited file rerun this test instead of reusing a cached pass.
func trackSources(t *testing.T) {
	t.Helper()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		_, err = os.Stat(path)
		return err
	})
	if err != nil {
		t.Fatalf("walk internal/: %v", err)
	}
}

// violations lists every forbidden import in pkg → imports. prefix is the
// module's internal/ import path, ending in a slash.
func violations(prefix string, imports map[string][]string) []string {
	var out []string
	for pkg, deps := range imports {
		for _, dep := range deps {
			if !allowed(prefix, pkg, dep) {
				out = append(out, pkg+" must not import "+dep)
			}
		}
	}
	return out
}

// allowed reports whether pkg may import dep.
func allowed(prefix, pkg, dep string) bool {
	area, name := split(prefix, pkg)
	depArea, depName := split(prefix, dep)
	switch {
	case area == "module" && depArea == "module":
		return name == depName
	case area == "platform":
		return depArea != "app" && depArea != "module"
	default:
		return true
	}
}

// split returns the top-level area under internal/ (app, platform, module) and,
// for modules, the module name. Packages outside internal/ return "".
func split(prefix, pkg string) (area, module string) {
	rest, ok := strings.CutPrefix(pkg, prefix)
	if !ok {
		return "", ""
	}
	parts := strings.Split(rest, "/")
	if parts[0] == "module" && len(parts) > 1 {
		return "module", parts[1]
	}
	return parts[0], ""
}

func TestArchitectureRules(t *testing.T) {
	// Any module path works: the rules never name the template.
	const modulePath = "example.com/client/svc/internal/"
	const m = modulePath + "module/"
	tests := []struct {
		pkg, dep string
		ok       bool
	}{
		{m + "a", m + "b", false},
		{m + "a/sub", m + "b/sub", false},
		{m + "a/sub", m + "a", true},
		{m + "a", m + "a/sub", true},
		{m + "a", modulePath + "platform/httpx", true},
		{modulePath + "platform/httpx", modulePath + "app", false},
		{modulePath + "platform/httpx", m + "a", false},
		{modulePath + "platform/httpx", modulePath + "platform/apierr", true},
		{modulePath + "app", m + "a", true},
		{m + "a", "net/http", true},
	}
	for _, tt := range tests {
		if got := allowed(modulePath, tt.pkg, tt.dep); got != tt.ok {
			t.Errorf("allowed(%s, %s) = %v, want %v", tt.pkg, tt.dep, got, tt.ok)
		}
	}
}
