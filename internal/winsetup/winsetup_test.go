package winsetup

import (
	"regexp"
	"strings"
	"testing"
)

func TestPSQuoteWrapsInSingleQuotes(t *testing.T) {
	if got, want := psQuote(`C:\docker-wcow`), `'C:\docker-wcow'`; got != want {
		t.Errorf("psQuote = %s, want %s", got, want)
	}
}

func TestPSQuoteEscapesSingleQuotes(t *testing.T) {
	// A single quote is the only character that is special inside a PowerShell
	// single-quoted literal; it is escaped by doubling. Backslashes must pass
	// through untouched or Windows paths break.
	if got, want := psQuote(`C:\jon's data`), `'C:\jon''s data'`; got != want {
		t.Errorf("psQuote = %s, want %s", got, want)
	}
}

func TestInstallArgsOmitsEmptyDataRoot(t *testing.T) {
	// Empty must produce no arguments so the script keeps its own default.
	if args := (InstallOptions{}).installArgs(); len(args) != 0 {
		t.Errorf("installArgs = %v, want none", args)
	}
}

func TestInstallArgsIncludesDataRoot(t *testing.T) {
	args := InstallOptions{DataRoot: `C:\docker-wcow`}.installArgs()
	want := []string{"-DataRoot", `'C:\docker-wcow'`}
	if len(args) != len(want) {
		t.Fatalf("installArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("installArgs[%d] = %s, want %s", i, args[i], want[i])
		}
	}
}

// InstallContainerd takes no options, and its script is passed through
// untouched so the wrapping cannot change its behaviour.
func TestWrapScriptLeavesArglessScriptAlone(t *testing.T) {
	const body = "param()\nWrite-Host hi"
	if got := wrapScript(body); got != body {
		t.Errorf("wrapScript = %q, want it unchanged", got)
	}
}

// -EncodedCommand cannot take arguments, so an args-bearing call must become a
// script block invocation for the param() block to bind them.
func TestWrapScriptInvokesScriptBlockWithArgs(t *testing.T) {
	got := wrapScript("param([string]$DataRoot)", "-DataRoot", `'C:\x'`)
	want := "& {\nparam([string]$DataRoot)\n} -DataRoot 'C:\\x'"
	if got != want {
		t.Errorf("wrapScript = %q, want %q", got, want)
	}
}

// scriptParams returns the parameter names declared by the embedded setup
// script's param() block.
func scriptParams(t *testing.T) map[string]string {
	t.Helper()
	start := strings.Index(script, "param(")
	if start < 0 {
		t.Fatal("embedded script has no param() block")
	}
	end := strings.Index(script[start:], "\n)")
	if end < 0 {
		t.Fatal("embedded script param() block is unterminated")
	}
	block := script[start : start+end]
	out := map[string]string{}
	re := regexp.MustCompile(`\$(\w+)\s*=\s*'([^']*)'`)
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// The Go side passes -DataRoot to the script. If the script ever stops
// declaring that parameter, the argument silently fails to bind and the
// override is lost, so assert the two stay in sync.
func TestScriptDeclaresEveryParamInstallArgsPasses(t *testing.T) {
	params := scriptParams(t)
	args := InstallOptions{DataRoot: `C:\x`}.installArgs()
	for i := 0; i < len(args); i += 2 {
		name := strings.TrimPrefix(args[i], "-")
		if _, ok := params[name]; !ok {
			t.Errorf("installArgs passes -%s but the script declares no such parameter", name)
		}
	}
}

func TestScriptDataRootDefaultsToScriptChoice(t *testing.T) {
	// Empty default keeps back-compat: the script resolves it to
	// <InstallDir>\data, so callers that do not care get the old layout.
	params := scriptParams(t)
	if got, ok := params["DataRoot"]; !ok || got != "" {
		t.Errorf("DataRoot default = %q (present=%v), want empty", got, ok)
	}
	if !strings.Contains(script, `$DataRoot = Join-Path $InstallDir 'data'`) {
		t.Error("script does not fall back to <InstallDir>\\data when DataRoot is empty")
	}
}

// Isolation must stay 'auto'. Pinning it to process leaves containers unable
// to start on client editions, which is the regression this default fixed.
func TestScriptIsolationDefaultsToAuto(t *testing.T) {
	params := scriptParams(t)
	if got := params["Isolation"]; got != "auto" {
		t.Errorf("Isolation default = %q, want auto", got)
	}
}

// Without "group" in daemon.json the pipe ACL is Administrators/LocalSystem
// only and non-elevated docker clients get "permission denied".
func TestScriptWritesGroupIntoDaemonConfig(t *testing.T) {
	params := scriptParams(t)
	if got := params["Group"]; got != "docker-users" {
		t.Errorf("Group default = %q, want docker-users", got)
	}
	if !strings.Contains(script, "group       = $Group") {
		t.Error("daemon.json does not include the pipe access group")
	}
}
