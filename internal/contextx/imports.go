package contextx

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	goSingleImport = regexp.MustCompile(`(?m)^\s*import\s+"(\.[^"]+)"`)
	jsFromImport   = regexp.MustCompile(`(?m)(?:from|import)\s+['"](\.[^'"]+)['"]`)
	jsRequire      = regexp.MustCompile(`require\(\s*['"](\.[^'"]+)['"]\s*\)`)
)

const maxRelativeImports = 5

// RelativeImportPaths returns resolved same-repo paths for relative imports
// in content (./ and ../). Stdlib and third-party imports are ignored.
func RelativeImportPaths(filename, content string) []string {
	if content == "" {
		return nil
	}
	dir := filepath.ToSlash(filepath.Dir(filename))
	seen := map[string]struct{}{}
	var out []string

	add := func(spec string) {
		if len(out) >= maxRelativeImports {
			return
		}
		resolved := resolveRelative(dir, spec)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		for _, m := range goSingleImport.FindAllStringSubmatch(content, -1) {
			add(m[1])
		}
		addGoImportBlock(content, add)
	default:
		for _, m := range jsFromImport.FindAllStringSubmatch(content, -1) {
			add(m[1])
		}
		for _, m := range jsRequire.FindAllStringSubmatch(content, -1) {
			add(m[1])
		}
	}
	return out
}

func addGoImportBlock(content string, add func(string)) {
	const start = "import ("
	idx := strings.Index(content, start)
	if idx < 0 {
		return
	}
	rest := content[idx+len(start):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return
	}
	block := rest[:end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		// optional alias: foo "./bar"
		if i := strings.Index(line, "\""); i >= 0 {
			line = line[i:]
		}
		spec := strings.Trim(line, `"`)
		if strings.HasPrefix(spec, ".") {
			add(spec)
		}
	}
}

func resolveRelative(dir, spec string) string {
	spec = strings.TrimSpace(spec)
	if !strings.HasPrefix(spec, ".") {
		return ""
	}
	joined := filepath.ToSlash(filepath.Clean(filepath.Join(dir, spec)))
	joined = strings.TrimPrefix(joined, "/")
	return joined
}
