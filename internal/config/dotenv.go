package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv loads KEY=value pairs from .env files into the process environment
// so config `${VAR}` references resolve without an explicit `export`. It reads
// <configDir>/.env then ./.env; a variable already set in the real environment
// always wins (and an earlier file wins over a later one), so nothing the user
// has actually exported is ever overridden.
func loadDotEnv(configDir string) {
	seen := map[string]bool{}
	paths := []string{filepath.Join(configDir, ".env"), ".env"}
	for _, p := range paths {
		applyDotEnv(p, seen)
	}
}

func applyDotEnv(path string, seen map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := parseDotEnvLine(sc.Text())
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		if _, exists := os.LookupEnv(key); exists {
			continue // real environment wins
		}
		_ = os.Setenv(key, val)
	}
}

// parseDotEnvLine parses one .env line into KEY,VALUE. It skips blanks and
// comments, tolerates a leading `export `, and strips matching surrounding
// single/double quotes from the value.
func parseDotEnvLine(line string) (key, val string, ok bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}
	s = strings.TrimPrefix(s, "export ")
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:eq])
	val = strings.TrimSpace(s[eq+1:])
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	if key == "" {
		return "", "", false
	}
	return key, val, true
}
