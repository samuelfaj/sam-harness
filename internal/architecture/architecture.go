package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const modulePrefix = "github.com/samuelfaj/sam-harness/"

// ForbiddenImports maps a repository-relative package directory to internal
// import path prefixes it must not depend on. The checker walks real Go source
// rather than a reconstructed graph.
func ForbiddenImports() map[string][]string {
	return map[string][]string{
		"cmd/sam-harness": {
			"github.com/samuelfaj/sam-harness/internal/adopt",
			"github.com/samuelfaj/sam-harness/internal/apply",
			"github.com/samuelfaj/sam-harness/internal/bootstrap",
			"github.com/samuelfaj/sam-harness/internal/check",
			"github.com/samuelfaj/sam-harness/internal/config",
			"github.com/samuelfaj/sam-harness/internal/doctor",
			"github.com/samuelfaj/sam-harness/internal/pipeline",
			"github.com/samuelfaj/sam-harness/internal/planner",
			"github.com/samuelfaj/sam-harness/internal/render",
			"github.com/samuelfaj/sam-harness/internal/scan",
		},
		"internal/model": {
			"github.com/samuelfaj/sam-harness/internal/",
		},
		"internal/repo": {
			"github.com/samuelfaj/sam-harness/internal/",
		},
		"internal/scan": {
			"github.com/samuelfaj/sam-harness/internal/adopt",
			"github.com/samuelfaj/sam-harness/internal/apply",
			"github.com/samuelfaj/sam-harness/internal/bootstrap",
			"github.com/samuelfaj/sam-harness/internal/cli",
			"github.com/samuelfaj/sam-harness/internal/pipeline",
			"github.com/samuelfaj/sam-harness/internal/planner",
			"github.com/samuelfaj/sam-harness/internal/render",
		},
		"internal/pipeline": {
			"github.com/samuelfaj/sam-harness/internal/adopt",
			"github.com/samuelfaj/sam-harness/internal/apply",
			"github.com/samuelfaj/sam-harness/internal/bootstrap",
			"github.com/samuelfaj/sam-harness/internal/cli",
			"github.com/samuelfaj/sam-harness/internal/doctor",
			"github.com/samuelfaj/sam-harness/internal/planner",
			"github.com/samuelfaj/sam-harness/internal/render",
			"github.com/samuelfaj/sam-harness/internal/scan",
			"github.com/samuelfaj/sam-harness/internal/status",
		},
		"internal/config": {
			"github.com/samuelfaj/sam-harness/internal/cli",
			"github.com/samuelfaj/sam-harness/internal/pipeline",
			"github.com/samuelfaj/sam-harness/internal/planner",
			"github.com/samuelfaj/sam-harness/internal/adopt",
		},
	}
}

// Check reports every forbidden internal import in root. It does not invent
// layers: only the declared package directories are constrained.
func Check(root string) error {
	root = filepath.Clean(root)
	forbidden := ForbiddenImports()
	fileSet := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if skippedDirectory(name) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		pkgDir := pathDir(rel)
		blocked := forbiddenFor(pkgDir, forbidden)
		if len(blocked) == 0 {
			return nil
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, spec := range file.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			if forbiddenImport(imp, blocked) {
				violations = append(violations, fmt.Sprintf("%s imports %s", rel, imp))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("architecture boundary violations:\n%s", strings.Join(violations, "\n"))
}

func skippedDirectory(name string) bool {
	switch name {
	case ".git", "testdata", "vendor", "node_modules", "dist", "build", ".sam-harness":
		return true
	default:
		return false
	}
}

func pathDir(rel string) string {
	dir := path.Dir(rel)
	if dir == "." {
		return ""
	}
	return dir
}

func forbiddenFor(pkgDir string, forbidden map[string][]string) []string {
	if rules, ok := forbidden[pkgDir]; ok {
		return append([]string(nil), rules...)
	}
	return nil
}

func forbiddenImport(imp string, blocked []string) bool {
	if !strings.HasPrefix(imp, modulePrefix) {
		return false
	}
	for _, prefix := range blocked {
		if prefix == imp || strings.HasPrefix(imp, prefix) {
			return true
		}
	}
	return false
}
