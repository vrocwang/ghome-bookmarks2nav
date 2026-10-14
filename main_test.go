package main

import (
	"os"

	"gopkg.in/yaml.v3"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	// Read the document back the way a consumer would: the CLI's own check is
	// inside Render, so what is verified here is the shape a user receives.
	var parsed struct {
		Site struct {
			Title string `yaml:"title"`
		} `yaml:"site"`
		Links  int `yaml:"-"`
		Search struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"search"`
		Groups []struct {
			Name    string `yaml:"name"`
			Private bool   `yaml:"private"`
			Links   []struct {
				Name string `yaml:"name"`
				URL  string `yaml:"url"`
			} `yaml:"links"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("the generated document does not parse: %v\n%s", err, doc)
	}
	if len(parsed.Groups) == 0 {
		t.Fatalf("no groups in the document\n%s", doc)
	}
	var private, links int
	for _, g := range parsed.Groups {
		if !g.Private {
			t.Errorf("group %q is not private", g.Name)
		}
		for _, l := range g.Links {
			links++
			if l.Name == "" || l.URL == "" {
				t.Errorf("incomplete entry %+v", l)
			}
		}
		private += len(g.Links)
	}
	if private != links || private == 0 {
		t.Errorf("private = %d of %d links, want all of them", private, links)
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
