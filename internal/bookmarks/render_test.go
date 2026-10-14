package bookmarks

import (
	"strings"
	"testing"

	"github.com/vrocwang/ghome-bookmarks2nav/internal/navcheck"
)

func renderSample(t *testing.T, opts Options) (string, Report) {
	t.Helper()
	doc, rep, err := Render(parseSample(t), opts)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return string(doc), rep
}

// The gate that matters: ghome has to accept the document, and every entry has
// to be private. A generated config the page refuses is worse than none.
func TestRenderProducesAPrivateDocumentGhomeAccepts(t *testing.T) {
	for _, opts := range []Options{
		{},
		{GroupPrivate: true},
		{Leaf: true, GroupPrivate: true},
		{Single: "全部书签", Search: true},
		{MaxName: 10},
	} {
		doc, _ := renderSample(t, opts)

		res, err := navcheck.Validate([]byte(doc))
		if err != nil {
			t.Fatalf("%+v: ghome rejects the document: %v\n%s", opts, err, doc)
		}
		if len(res.Warnings) != 0 {
			t.Errorf("%+v: warnings = %v, want none", opts, res.Warnings)
		}
		if res.Private == 0 {
			t.Errorf("%+v: Private = 0, want every entry private", opts)
		}
		// 8 bookmarks: one duplicate and one javascript: url are dropped.
		if res.Links != 5 {
			t.Errorf("%+v: Links = %d, want 5", opts, res.Links)
		}
	}
}

func TestRenderDropsDuplicatesAndUnusableURLs(t *testing.T) {
	doc, rep := renderSample(t, Options{})

	if rep.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", rep.Duplicates)
	}
	if len(rep.Unsupported) != 2 {
		t.Fatalf("Unsupported = %+v, want the javascript: link and the empty href", rep.Unsupported)
	}
	if strings.Contains(doc, "javascript:") {
		t.Error("the javascript: url was written out; one of those makes ghome refuse the document")
	}
	if strings.Count(doc, "https://dupe.example.com") != 1 {
		t.Errorf("the duplicate url appears %d times, want once", strings.Count(doc, "https://dupe.example.com"))
	}
	if rep.Links != 5 || rep.Groups != 4 {
		t.Errorf("Report = %d links / %d groups, want 5 / 4", rep.Links, rep.Groups)
	}
}

func TestRenderTitleTruncationIsRuneSafe(t *testing.T) {
	folder := Folder{Folders: []*Folder{{Name: "组", Links: []Link{
		{Name: "一二三四五六七八九十", URL: "https://a.example.com"},
	}}}}
	doc, rep, err := Render(folder, Options{MaxName: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"一二三四…"`) {
		t.Errorf("document = %s, want the title truncated to 4 runes plus an ellipsis", doc)
	}
	if rep.Truncated != 1 {
		t.Errorf("Truncated = %d, want 1", rep.Truncated)
	}
}

// The two spellings of "private" have to mean the same thing to ghome.
func TestRenderPrivateSpellings(t *testing.T) {
	perLink, _ := renderSample(t, Options{GroupPrivate: false})
	if strings.Count(perLink, "private: true") != 5 {
		t.Errorf("per-link document has %d private markers, want one per link",
			strings.Count(perLink, "private: true"))
	}
	perGroup, _ := renderSample(t, Options{GroupPrivate: true})
	if strings.Count(perGroup, "private: true") != 4 {
		t.Errorf("per-group document has %d private markers, want one per group",
			strings.Count(perGroup, "private: true"))
	}
}

func TestRenderRefusesAnEmptyExport(t *testing.T) {
	if _, _, err := Render(Folder{}, Options{}); err == nil {
		t.Error("Render() accepted an empty tree")
	}
}

// A scheme ghome allows is kept: the tool mirrors nav.checkURL rather than
// guessing which links are useful.
func TestUnsupportedOnlyRefusesWhatGhomeRefuses(t *testing.T) {
	allowed := []string{
		"https://a.example.com", "http://a.example.com", "chrome://bookmarks",
		"file:///Users/me/x.pdf", "ssh://git@host", "mailto:a@b.c", "vscode://file/x",
	}
	for _, u := range allowed {
		if reason := unsupported(u); reason != "" {
			t.Errorf("unsupported(%q) = %q, want it kept", u, reason)
		}
	}
	refused := map[string]string{
		"javascript:alert(1)": "javascript",
		"data:text/html,x":    "data",
		"vbscript:x":          "vbscript",
		"blob:https://a/b":    "blob",
		"go.dev":              "no scheme",
		"":                    "empty",
	}
	for u, want := range refused {
		if reason := unsupported(u); !strings.Contains(reason, want) {
			t.Errorf("unsupported(%q) = %q, want it to mention %q", u, reason, want)
		}
	}
}

// Titles are where quoting bites: a colon, a quote, a backslash and an "&" all
// have to survive the round trip through ghome's own parser.
func TestYAMLStringSurvivesTheParser(t *testing.T) {
	folder := Folder{Folders: []*Folder{{Name: `a "b": c \ d & e`, Links: []Link{
		{Name: `标题: "引号" & 反斜杠 \ 与冒号`, URL: "https://a.example.com"},
	}}}}
	doc, _, err := Render(folder, Options{})
	if err != nil {
		t.Fatal(err)
	}
	doc2, err := navcheck.Decode(doc)
	if err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, doc)
	}
	if got := doc2.Groups[0].Name; got != `a "b": c \ d & e` {
		t.Errorf("group name round-tripped to %q", got)
	}
	if got := doc2.Groups[0].Links[0].Name; got != `标题: "引号" & 反斜杠 \ 与冒号` {
		t.Errorf("link name round-tripped to %q", got)
	}
}

// ghome trims a url before checking it, so an href with padding loads there.
// The tool has to do the same -- and write the TRIMMED value, so the string it
// validated is the string the page serves.
func TestRenderTrimsURLsAndWritesTheTrimmedValue(t *testing.T) {
	folder := Folder{Folders: []*Folder{{
		Name: "组",
		Links: []Link{
			{Name: "前导空格", URL: " https://padded.example.com/"},
			{Name: "尾随制表符", URL: "https://tabbed.example.com/\t"},
			{Name: "尾随 NBSP", URL: "https://nbsp.example.com/\u00a0"},
			{Name: "正常", URL: "https://plain.example.com/"},
		},
	}}}

	doc, rep, err := Render(folder, Options{})

	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	// All four survive: none of them is refused, and none is skipped.
	if len(rep.Unsupported) != 0 {
		t.Errorf("Unsupported = %+v, want none", rep.Unsupported)
	}
	if rep.Links != 4 {
		t.Errorf("Links = %d, want 4", rep.Links)
	}
	parsed, err := navcheck.Decode(doc)
	if err != nil {
		t.Fatalf("document does not parse: %v\n%s", err, doc)
	}
	urls := map[string]bool{}
	for _, g := range parsed.Groups {
		for _, l := range g.Links {
			urls[l.URL] = true
			if l.URL != strings.TrimSpace(l.URL) {
				t.Errorf("url written with padding: %q", l.URL)
			}
		}
	}
	for _, want := range []string{
		"https://padded.example.com/",
		"https://tabbed.example.com/",
		"https://nbsp.example.com/",
		"https://plain.example.com/",
	} {
		if !urls[want] {
			t.Errorf("url %q missing; have %v", want, urls)
		}
	}
	if _, err := navcheck.Validate(doc); err != nil {
		t.Fatalf("ghome would refuse it: %v\n%s", err, doc)
	}
}
