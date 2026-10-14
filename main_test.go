package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vrocwang/ghome-bookmarks2nav/internal/navcheck"
)

const exportHTML = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><H3>书签栏</H3>
    <DL><p>
        <DT><A HREF="https://a.example.com" ICON="data:image/png;base64,AAAA">A &amp; B</A>
        <DT><H3>内网</H3>
        <DL><p>
            <DT><A HREF="http://nas:8080/">NAS</A>
            <DT><A HREF="chrome://bookmarks">浏览器内部页</A>
        </DL><p>
    </DL><p>
</DL><p>
`

// build compiles the command once per test run: this is the only way to test
// the thing users actually invoke, and it is a small binary.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bookmarks-to-nav")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestCLIConvertsAnExport(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "bookmarks.html")
	if err := os.WriteFile(in, []byte(exportHTML), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "nav.yaml")

	cmd := exec.Command(bin, "--out", out, "--leaf", "--search", in)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, stdout)
	}
	summary := string(stdout)
	// Three bookmarks, none dropped: ghome's scheme denylist is javascript:,
	// data:, vbscript: and blob:, so a chrome:// bookmark is kept -- this tool
	// mirrors that rule rather than deciding for itself which links are useful.
	if !strings.Contains(summary, "3 links in 2 groups") {
		t.Errorf("summary = %q, want 3 links in 2 groups", summary)
	}
	if strings.Contains(summary, "skipped") {
		t.Errorf("summary = %q, want nothing skipped", summary)
	}

	doc, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The whole point: ghome has to accept it, and everything in it is private.
	res, err := navcheck.Validate(doc)
	if err != nil {
		t.Fatalf("the generated document would not load in ghome: %v\n%s", err, doc)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}
	if res.Private != res.Links || res.Private == 0 {
		t.Errorf("private entries = %d of %d links, want all of them", res.Private, res.Links)
	}
	parsed, err := navcheck.Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	// --leaf: the group is named after the innermost folder.
	for _, g := range parsed.Groups {
		if g.Name == "书签栏 / 内网" {
			t.Error("--leaf produced a full path group name")
		}
	}
	if parsed.Search.Enabled == nil || !*parsed.Search.Enabled {
		t.Error("--search did not write an enabled search block")
	}
	// Entities are decoded before they are written.
	if !strings.Contains(string(doc), "A & B") {
		t.Errorf("document = %s, want the decoded title", doc)
	}
	// And a scheme ghome allows survives the trip.
	if !strings.Contains(string(doc), "chrome://bookmarks") {
		t.Errorf("document = %s, want the chrome:// bookmark kept", doc)
	}
}

// Firefox has no HTML export in its UI any more, so its JSON is the format most
// Firefox users will hand the tool.
const firefoxExport = `{"guid":"root________","title":"","typeCode":2,"root":"placesRoot","children":[
 {"guid":"menu________","title":"menu","typeCode":2,"root":"bookmarksMenuFolder","children":[
  {"guid":"a","title":"Mozilla","typeCode":1,"uri":"https://www.mozilla.org/","iconUri":"data:image/png;base64,AAAA"},
  {"guid":"b","title":"","typeCode":1,"uri":"https://untitled.example.com/"}]},
 {"guid":"toolbar_____","title":"toolbar","typeCode":2,"root":"toolbarFolder","children":[
  {"guid":"c","title":"NAS","typeCode":1,"uri":"http://nas:8080/"}]}]}`

func TestCLIConvertsAFirefoxJSONExport(t *testing.T) {
	bin := build(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "bookmarks.json")
	if err := os.WriteFile(in, []byte(firefoxExport), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := &strings.Builder{}
	cmd := exec.Command(bin, "--search", in)
	cmd.Stdout = stdout
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "firefox json") {
		t.Errorf("stderr = %q, want it to say which format it read", stderr.String())
	}

	doc := stdout.String()
	res, err := navcheck.Validate([]byte(doc))
	if err != nil {
		t.Fatalf("the generated document would not load in ghome: %v\n%s", err, doc)
	}
	if res.Warnings != nil {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}
	if res.Private != res.Links || res.Private == 0 {
		t.Errorf("private = %d of %d links", res.Private, res.Links)
	}
	// The roots are named the way the Bookmarks menu shows them, and an
	// untitled bookmark is named after its host rather than left blank.
	for _, want := range []string{"书签菜单", "书签工具栏", "untitled.example.com"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document is missing %q:\n%s", want, doc)
		}
	}
	if !strings.Contains(stderr.String(), "untitled bookmarks named after their host") {
		t.Errorf("stderr = %q, want the summary to mention the naming fallback", stderr.String())
	}
}

func TestCLIReadsStdinAndFailsOnGarbage(t *testing.T) {
	bin := build(t)

	cmd := exec.Command(bin, "-")
	cmd.Stdin = strings.NewReader(exportHTML)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stdin mode failed: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, "-")
	cmd.Stdin = strings.NewReader("this is not a bookmark export")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a non-export was accepted:\n%s", out)
	}
	if !strings.Contains(string(out), "no bookmarks found") {
		t.Errorf("stderr = %q, want it to explain what was wrong", out)
	}
}
