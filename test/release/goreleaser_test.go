package release

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/abyssbugg/ant-router"

// repoRoot walks up from the package directory (go test's working
// directory) to the module root, so the tests do not depend on the path the
// compiler recorded for this source file.
func repoRoot(t *testing.T) string {
	t.Helper()
	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, visited, ok := findModuleRoot(start, modulePath)
	if !ok {
		t.Fatalf("module root for %s not found; searched %s", modulePath, strings.Join(visited, ", "))
	}
	return root
}

// findModuleRoot returns the nearest directory at or above start whose go.mod
// declares module path. It tolerates CRLF line endings so a Windows checkout
// without LF normalization still resolves.
func findModuleRoot(start, path string) (root string, visited []string, ok bool) {
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		visited = append(visited, dir)
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			first, _, _ := bytes.Cut(data, []byte("\n"))
			if string(bytes.TrimRight(first, "\r")) == "module "+path {
				return dir, visited, true
			}
		}
		if filepath.Dir(dir) == dir {
			return "", visited, false
		}
	}
}

// readText reads a repository file with CRLF normalized to LF. CI checkouts
// on Windows may still produce CRLF files, and the assertions here are about
// content, not line endings.
func readText(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), nil
}

// --- minimal block-YAML reader -------------------------------------------
//
// The release config uses only block mappings, block sequences and plain or
// quoted scalars. This reader accepts exactly that subset and fails on
// anything else (flow collections, anchors, multi-line scalars), so a test
// can never pass because the config was silently misread.

type yamlLine struct {
	number int
	indent int
	text   string
}

func readYAMLLines(fail func(string, ...any), data []byte) []yamlLine {
	var lines []yamlLine
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for number := 1; scanner.Scan(); number++ {
		raw := scanner.Text()
		if strings.Contains(raw, "\t") {
			fail("line %d: tabs are not valid YAML indentation", number)
		}
		trimmed := strings.TrimLeft(raw, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, yamlLine{number: number, indent: len(raw) - len(trimmed), text: trimmed})
	}
	if err := scanner.Err(); err != nil {
		fail("%v", err)
	}
	return lines
}

type yamlReader struct {
	fail  func(string, ...any)
	lines []yamlLine
	pos   int
}

// yamlError carries a reader failure out of the recursive descent.
type yamlError struct{ message string }

// decodeYAML parses data, returning the first error the reader reports.
func decodeYAML(data []byte) (result map[string]any, err error) {
	fail := func(format string, args ...any) { panic(yamlError{fmt.Sprintf(format, args...)}) }
	defer func() {
		if recovered := recover(); recovered != nil {
			failure, ok := recovered.(yamlError)
			if !ok {
				panic(recovered)
			}
			result, err = nil, fmt.Errorf("%s", failure.message)
		}
	}()
	reader := &yamlReader{fail: fail, lines: readYAMLLines(fail, data)}
	if len(reader.lines) == 0 {
		fail("empty YAML document")
	}
	value := reader.block(reader.lines[0].indent)
	if reader.pos != len(reader.lines) {
		fail("line %d: unexpected content", reader.lines[reader.pos].number)
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		fail("document root is not a mapping")
	}
	return mapping, nil
}

func parseYAML(t *testing.T, data []byte) map[string]any {
	t.Helper()
	config, err := decodeYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func (r *yamlReader) block(indent int) any {
	if strings.HasPrefix(r.lines[r.pos].text, "- ") || r.lines[r.pos].text == "-" {
		return r.sequence(indent)
	}
	return r.mapping(indent)
}

func (r *yamlReader) mapping(indent int) map[string]any {
	result := map[string]any{}
	for r.pos < len(r.lines) && r.lines[r.pos].indent == indent {
		line := r.lines[r.pos]
		if strings.HasPrefix(line.text, "- ") {
			r.fail("line %d: sequence item inside mapping", line.number)
		}
		key, value := r.splitKey(line)
		r.pos++
		if _, duplicate := result[key]; duplicate {
			r.fail("line %d: duplicate key %q", line.number, key)
		}
		result[key] = r.valueAfterKey(value, indent)
	}
	if r.pos < len(r.lines) && r.lines[r.pos].indent > indent {
		r.fail("line %d: unexpected indentation", r.lines[r.pos].number)
	}
	return result
}

func (r *yamlReader) sequence(indent int) []any {
	var result []any
	for r.pos < len(r.lines) && r.lines[r.pos].indent == indent && strings.HasPrefix(r.lines[r.pos].text+" ", "- ") {
		line := r.lines[r.pos]
		item := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		if item == "" {
			r.pos++
			if r.pos >= len(r.lines) || r.lines[r.pos].indent <= indent {
				r.fail("line %d: empty sequence item", line.number)
			}
			result = append(result, r.block(r.lines[r.pos].indent))
			continue
		}
		if key, value, isMapping := cutMappingKey(item); isMapping {
			// "- key: value" starts a mapping whose keys align with key.
			itemIndent := indent + len(line.text) - len(strings.TrimLeft(strings.TrimPrefix(line.text, "-"), " "))
			r.lines[r.pos] = yamlLine{number: line.number, indent: itemIndent, text: key + ":" + prefixSpace(value)}
			result = append(result, r.mapping(itemIndent))
			continue
		}
		r.pos++
		result = append(result, r.scalar(line, item))
	}
	return result
}

func prefixSpace(value string) string {
	if value == "" {
		return ""
	}
	return " " + value
}

func (r *yamlReader) valueAfterKey(value string, indent int) any {
	if value != "" {
		return r.scalar(r.lines[r.pos-1], value)
	}
	if r.pos >= len(r.lines) || r.lines[r.pos].indent < indent {
		return nil
	}
	next := r.lines[r.pos]
	if next.indent == indent && strings.HasPrefix(next.text, "- ") {
		return r.sequence(indent) // compact sequence under a key
	}
	if next.indent <= indent {
		return nil
	}
	return r.block(next.indent)
}

func cutMappingKey(text string) (key, value string, ok bool) {
	if strings.HasPrefix(text, `"`) || strings.HasPrefix(text, `'`) {
		return "", "", false
	}
	index := strings.Index(text, ": ")
	if index < 0 {
		if strings.HasSuffix(text, ":") {
			return strings.TrimSuffix(text, ":"), "", true
		}
		return "", "", false
	}
	return text[:index], strings.TrimSpace(text[index+2:]), true
}

func (r *yamlReader) splitKey(line yamlLine) (string, string) {
	key, value, ok := cutMappingKey(line.text)
	if !ok || key == "" {
		r.fail("line %d: expected key: value, got %q", line.number, line.text)
	}
	return key, value
}

// emptyFlowMapping is the scalar the reader returns for the flow value `{}`.
const emptyFlowMapping = "{}"

func (r *yamlReader) scalar(line yamlLine, value string) string {
	switch {
	case value == emptyFlowMapping:
		// The empty mapping is the one flow form workflows need
		// (`permissions: {}`); it is returned as a sentinel scalar.
		return emptyFlowMapping
	case strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{"):
		r.fail("line %d: flow collections are not supported by this reader", line.number)
	case strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") || value == "|" || value == ">":
		r.fail("line %d: anchors and block scalars are not supported by this reader", line.number)
	case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
		return value[1 : len(value)-1]
	case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
		return value[1 : len(value)-1]
	case strings.Contains(value, " #"):
		return strings.TrimSpace(value[:strings.Index(value, " #")])
	}
	return value
}

// --- typed accessors ---------------------------------------------------------

func loadConfig(t *testing.T) map[string]any {
	t.Helper()
	data, err := readText(filepath.Join(repoRoot(t), ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return parseYAML(t, data)
}

func field(t *testing.T, node any, path ...string) any {
	t.Helper()
	current := node
	for index, key := range path {
		mapping, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s is not a mapping", strings.Join(path[:index], "."))
		}
		value, present := mapping[key]
		if !present {
			t.Fatalf("missing %s", strings.Join(path[:index+1], "."))
		}
		current = value
	}
	return current
}

func list(t *testing.T, node any, path ...string) []any {
	t.Helper()
	values, ok := field(t, node, path...).([]any)
	if !ok {
		t.Fatalf("%s is not a sequence", strings.Join(path, "."))
	}
	return values
}

func strings_(t *testing.T, node any, path ...string) []string {
	t.Helper()
	var result []string
	for index, value := range list(t, node, path...) {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s[%d] is not a scalar", strings.Join(path, "."), index)
		}
		result = append(result, text)
	}
	return result
}

func text(t *testing.T, node any, path ...string) string {
	t.Helper()
	value, ok := field(t, node, path...).(string)
	if !ok {
		t.Fatalf("%s is not a scalar", strings.Join(path, "."))
	}
	return value
}

func sameSet(got, want []string) bool {
	got, want = slices.Clone(got), slices.Clone(want)
	sort.Strings(got)
	sort.Strings(want)
	return slices.Equal(got, want)
}

// --- tests -------------------------------------------------------------------

func TestYAMLReaderRejectsUnsupportedSyntax(t *testing.T) {
	for name, document := range map[string]string{
		"flow":   "builds: [a, b]\n",
		"anchor": "base: &base x\n",
		"block":  "text: |\n  x\n",
		// A bare "-" with nothing nested under it must fail cleanly, not
		// index past the end of the document.
		"trailing empty item": "list:\n  -\n",
		"empty item":          "list:\n  -\nnext: x\n",
	} {
		if _, err := decodeYAML([]byte(document)); err == nil {
			t.Errorf("reader accepted unsupported %s syntax", name)
		}
	}
}

func TestYAMLReaderParsesNestedStructure(t *testing.T) {
	document := "a: 1\nlist:\n  - x\n  - \"y\"\nitems:\n  - id: one\n    goos:\n      - linux\n  - id: two\nmap:\n  key: value # comment\n"
	config := parseYAML(t, []byte(document))
	if got := strings_(t, config, "list"); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("list = %v", got)
	}
	items := list(t, config, "items")
	if len(items) != 2 || text(t, items[0], "id") != "one" || text(t, items[1], "id") != "two" {
		t.Fatalf("items = %#v", items)
	}
	if got := strings_(t, items[0], "goos"); !slices.Equal(got, []string{"linux"}) {
		t.Fatalf("items[0].goos = %v", got)
	}
	if got := text(t, config, "map", "key"); got != "value" {
		t.Fatalf("map.key = %q", got)
	}
}

func TestGoreleaserBuildsCoverEveryReleasePlatform(t *testing.T) {
	config := loadConfig(t)
	if text(t, config, "version") != "2" {
		t.Fatal("version must be 2")
	}
	wantMains := map[string]string{"routerd": "./cmd/routerd", "routerctl": "./cmd/routerctl"}
	builds := list(t, config, "builds")
	seen := map[string]bool{}
	for _, build := range builds {
		id := text(t, build, "id")
		main, known := wantMains[id]
		if !known {
			t.Fatalf("unexpected build id %q", id)
		}
		seen[id] = true
		if got := text(t, build, "main"); got != main {
			t.Errorf("%s main = %q, want %q", id, got, main)
		}
		if got := strings_(t, build, "goos"); !sameSet(got, []string{"darwin", "linux", "windows"}) {
			t.Errorf("%s goos = %v, want darwin, linux, windows", id, got)
		}
		if got := strings_(t, build, "goarch"); !sameSet(got, []string{"amd64", "arm64"}) {
			t.Errorf("%s goarch = %v, want amd64, arm64", id, got)
		}
		if !slices.Contains(strings_(t, build, "env"), "CGO_ENABLED=0") {
			t.Errorf("%s must build with CGO_ENABLED=0", id)
		}
		if !slices.Contains(strings_(t, build, "flags"), "-trimpath") {
			t.Errorf("%s must build with -trimpath", id)
		}
		if got := text(t, build, "mod_timestamp"); got != "{{ .CommitTimestamp }}" {
			t.Errorf("%s mod_timestamp = %q, want reproducible commit timestamp", id, got)
		}
	}
	if len(seen) != len(wantMains) {
		t.Fatalf("builds = %v, want routerd and routerctl", seen)
	}
}

// buildinfoVariables returns the unexported package-level string variables
// of internal/buildinfo, which are the -X link targets.
func buildinfoVariables(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot(t), "internal", "buildinfo", "buildinfo.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if !name.IsExported() {
					names = append(names, name.Name)
				}
			}
		}
	}
	return names
}

func TestGoreleaserLinkFlagsTargetExistingBuildinfoVariables(t *testing.T) {
	variables := buildinfoVariables(t)
	if !sameSet(variables, []string{"version", "commit", "date"}) {
		t.Fatalf("buildinfo variables = %v; update the release config and this test together", variables)
	}
	prefix := "-X " + modulePath + "/internal/buildinfo."
	for _, build := range list(t, loadConfig(t), "builds") {
		id := text(t, build, "id")
		flags := strings_(t, build, "ldflags")
		if !slices.Contains(flags, "-s -w") {
			t.Errorf("%s ldflags must strip symbols with -s -w", id)
		}
		var targets []string
		for _, flag := range flags {
			if rest, ok := strings.CutPrefix(flag, prefix); ok {
				name, _, _ := strings.Cut(rest, "=")
				targets = append(targets, name)
			} else if strings.HasPrefix(flag, "-X ") {
				t.Errorf("%s has -X flag outside buildinfo: %q", id, flag)
			}
		}
		if !sameSet(targets, variables) {
			t.Errorf("%s -X targets = %v, want exactly %v", id, targets, variables)
		}
	}
}

func TestGoreleaserArchivesChecksumsAndSBOMs(t *testing.T) {
	config := loadConfig(t)
	archives := list(t, config, "archives")
	if len(archives) != 1 {
		t.Fatalf("archives = %d entries, want 1", len(archives))
	}
	archive := archives[0]
	if got := strings_(t, archive, "formats"); !slices.Equal(got, []string{"tar.gz"}) {
		t.Errorf("archive formats = %v, want [tar.gz]", got)
	}
	overrides := list(t, archive, "format_overrides")
	if len(overrides) != 1 || text(t, overrides[0], "goos") != "windows" || !slices.Equal(strings_(t, overrides[0], "formats"), []string{"zip"}) {
		t.Errorf("format_overrides = %#v, want windows -> [zip]", overrides)
	}
	if got := text(t, archive, "name_template"); got != "ant-router_{{ .Version }}_{{ .Os }}_{{ .Arch }}" {
		t.Errorf("archive name_template = %q", got)
	}
	if !slices.Contains(strings_(t, archive, "files"), "LICENSE") {
		t.Error("archives must include LICENSE")
	}
	if text(t, config, "checksum", "algorithm") != "sha256" || text(t, config, "checksum", "name_template") != "checksums.txt" {
		t.Error("checksum must be sha256 in checksums.txt")
	}
	sboms := list(t, config, "sboms")
	if len(sboms) != 1 || text(t, sboms[0], "artifacts") != "archive" {
		t.Errorf("sboms = %#v, want one archive SBOM", sboms)
	}
}

func TestGoreleaserSignsChecksumsWithKeylessCosignBundle(t *testing.T) {
	signs := list(t, loadConfig(t), "signs")
	if len(signs) != 1 {
		t.Fatalf("signs = %d entries, want 1", len(signs))
	}
	sign := signs[0]
	// GoReleaser publishes only after every sign command succeeds, so the
	// wrapper verifies the bundle against the installer identity before
	// anything becomes public.
	if text(t, sign, "cmd") != "bash" {
		t.Errorf("signs.cmd = %q, want bash running the sign-and-verify wrapper", text(t, sign, "cmd"))
	}
	if text(t, sign, "artifacts") != "checksum" {
		t.Error("signs must target the checksum file")
	}
	signature := text(t, sign, "signature")
	if signature != "${artifact}.sigstore.json" {
		t.Errorf("signs.signature = %q, want ${artifact}.sigstore.json", signature)
	}
	want := []string{".circleci/scripts/sign-and-verify.sh", "${artifact}", "${signature}"}
	if got := strings_(t, sign, "args"); !slices.Equal(got, want) {
		t.Errorf("signs.args = %v, want %v", got, want)
	}
}

// runSignAndVerify executes sign-and-verify.sh with a stub cosign whose
// verify-blob exit status is verifyExit.
func runSignAndVerify(t *testing.T, verifyExit int, args ...string) ([]string, string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sign-and-verify.sh runs on the Linux release image")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls.log")
	cosign := "#!/bin/sh\nprintf '%s\\n' \"cosign $*\" >> \"$CALLS\"\n" +
		"case \"$1\" in verify-blob) exit " + strconv.Itoa(verifyExit) + " ;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "cosign"), []byte(cosign), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{filepath.Join(repoRoot(t), ".circleci", "scripts", "sign-and-verify.sh")}, args...)...)
	cmd.Env = []string{"PATH=" + dir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin", "CALLS=" + calls}
	output, runErr := cmd.CombinedOutput()
	var logged []string
	if data, err := os.ReadFile(calls); err == nil {
		logged = strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	return logged, string(output), runErr
}

func TestSignAndVerifySignsThenVerifiesWithInstallerIdentity(t *testing.T) {
	calls, output, err := runSignAndVerify(t, 0, "dist/checksums.txt", "dist/checksums.txt.sigstore.json")
	if err != nil {
		t.Fatalf("sign-and-verify.sh failed: %v\n%s", err, output)
	}
	want := []string{
		"cosign sign-blob --yes --bundle=dist/checksums.txt.sigstore.json dist/checksums.txt",
		"cosign verify-blob --bundle dist/checksums.txt.sigstore.json --certificate-identity " + releaseCertificateIdentity + " --certificate-oidc-issuer " + releaseOIDCIssuer + " dist/checksums.txt",
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("cosign calls = %q, want %q", calls, want)
	}
}

func TestSignAndVerifyFailsWhenVerificationFails(t *testing.T) {
	if _, output, err := runSignAndVerify(t, 1, "dist/checksums.txt", "dist/checksums.txt.sigstore.json"); err == nil {
		t.Fatalf("sign-and-verify.sh succeeded although verify-blob failed\n%s", output)
	}
}

func TestSignAndVerifyRejectsWrongArgumentCountBeforeSigning(t *testing.T) {
	for _, args := range [][]string{nil, {"dist/checksums.txt"}, {"a", "b", "c"}, {"", "dist/x.sigstore.json"}} {
		calls, output, err := runSignAndVerify(t, 0, args...)
		if err == nil {
			t.Errorf("args %q: sign-and-verify.sh succeeded\n%s", args, output)
		}
		if len(calls) != 0 {
			t.Errorf("args %q: cosign ran before argument validation: %q", args, calls)
		}
	}
}

func TestGoreleaserHomebrewCaskAndRelease(t *testing.T) {
	config := loadConfig(t)
	if _, deprecated := config["brews"]; deprecated {
		t.Error("brews is deprecated in GoReleaser v2; use homebrew_casks")
	}
	casks := list(t, config, "homebrew_casks")
	if len(casks) != 1 {
		t.Fatalf("homebrew_casks = %d entries, want 1", len(casks))
	}
	cask := casks[0]
	if text(t, cask, "repository", "owner") != "abyssbugg" || text(t, cask, "repository", "name") != "homebrew-tap" {
		t.Error("cask must publish to abyssbugg/homebrew-tap")
	}
	// index tolerates an unset variable (the .Env.X form aborts the whole
	// release when the tap token is missing).
	if got := text(t, cask, "repository", "token"); got != `{{ index .Env "HOMEBREW_TAP_GITHUB_TOKEN" }}` {
		t.Errorf("cask token = %q, want the HOMEBREW_TAP_GITHUB_TOKEN secret via index", got)
	}
	// Prereleases must not move the tap's stable cask, and a missing tap
	// token skips only the cask instead of failing a signed release.
	wantSkip := `{{ if or .Prerelease (eq (index .Env "HOMEBREW_TAP_GITHUB_TOKEN") "") }}true{{ else }}false{{ end }}`
	if got := text(t, cask, "skip_upload"); got != wantSkip {
		t.Errorf("cask skip_upload = %q, want %q", got, wantSkip)
	}
	// The binaries are not notarized, so Gatekeeper would block the first
	// run; the cask clears quarantine on exactly the installed binaries.
	wantHook := `if OS.mac? then system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/routerd", "#{staged_path}/routerctl"] end`
	if got := text(t, cask, "hooks", "post", "install"); got != wantHook {
		t.Errorf("cask post-install hook = %q, want %q", got, wantHook)
	}
	if got := strings_(t, cask, "binaries"); !sameSet(got, []string{"routerd", "routerctl"}) {
		t.Errorf("cask binaries = %v", got)
	}
	if text(t, config, "release", "github", "owner") != "abyssbugg" || text(t, config, "release", "github", "name") != "ant-router" {
		t.Error("release must target abyssbugg/ant-router")
	}
	// Installers resolve releases through /releases/latest and
	// /releases/download/<tag>, which do not serve drafts; a draft release
	// would leave every published tag uninstallable.
	if got := text(t, config, "release", "draft"); got != "false" {
		t.Errorf("release.draft = %q, want false", got)
	}
	if got := text(t, config, "release", "prerelease"); got != "auto" {
		t.Errorf("release.prerelease = %q, want auto", got)
	}
	excluded := strings_(t, config, "changelog", "filters", "exclude")
	for _, prefix := range []string{"^docs", "^test", "^chore"} {
		if !slices.ContainsFunc(excluded, func(pattern string) bool { return strings.HasPrefix(pattern, prefix) }) {
			t.Errorf("changelog must exclude %s commits", prefix)
		}
	}
}

func TestMakefileDeclaresReleaseTargets(t *testing.T) {
	data, err := readText(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	// The snapshot builds every artifact locally without the CI-only steps
	// (signing needs the release OIDC identity, SBOMs need syft).
	if !bytes.Contains(data, []byte("goreleaser release --snapshot --clean --skip=sign,publish,sbom\n")) {
		t.Error("release-snapshot must skip sign, publish and sbom")
	}
	for _, target := range []string{"release-check", "release-snapshot", "release-test"} {
		if !bytes.Contains(data, []byte("\n"+target+":")) {
			t.Errorf("Makefile is missing target %s", target)
		}
		if !bytes.Contains(data, []byte(fmt.Sprintf(" %s", target))) || !phonyDeclares(data, target) {
			t.Errorf("Makefile target %s is not declared .PHONY", target)
		}
	}
}

func phonyDeclares(makefile []byte, target string) bool {
	for _, line := range strings.Split(string(makefile), "\n") {
		if rest, ok := strings.CutPrefix(line, ".PHONY:"); ok && slices.Contains(strings.Fields(rest), target) {
			return true
		}
	}
	return false
}

func TestGitignoreExcludesReleaseOutput(t *testing.T) {
	data, err := readText(filepath.Join(repoRoot(t), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "/dist/" || strings.TrimSpace(line) == "dist/" {
			return
		}
	}
	t.Fatal(".gitignore must exclude goreleaser's dist/ output")
}
