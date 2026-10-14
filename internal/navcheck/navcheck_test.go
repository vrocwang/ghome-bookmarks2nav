package navcheck

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateAcceptsAConvertedDocument(t *testing.T) {
	doc := []byte(`site:
  title: "我的导航"
groups:
  - name: "书签栏 / 学习"
    private: true
    links:
      - {name: "A & B", url: "https://a.example.com"}
      - {name: "内网", url: "http://nas:8080/"}
`)
	res, err := Validate(doc)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if res.Groups != 1 || res.Links != 2 || res.Private != 2 {
		t.Errorf("Result = %+v, want 1 group / 2 links / 2 private", res)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", res.Warnings)
	}
}

// The rules have to match ghome's, or the tool either refuses good documents or
// waves through one the page will reject.
func TestValidateMirrorsGhomeRules(t *testing.T) {
	fatal := map[string]string{
		"yaml syntax":        "groups: [\n",
		"no links":           "groups:\n  - name: g\n    links: []\n",
		"missing url":        "groups:\n  - name: g\n    links:\n      - {name: a}\n",
		"dangerous scheme":   "groups:\n  - name: g\n    links:\n      - {name: a, url: \"javascript:alert(1)\"}\n",
		"missing name":       "groups:\n  - name: g\n    links:\n      - {url: \"https://a.example.com\"}\n",
		"engine without q":   "groups:\n  - name: g\n    links:\n      - {name: a, url: \"https://a.example.com\"}\nsearch:\n  engines:\n    - {name: e, url: \"https://e.example.com/\"}\n",
		"group without name": "groups:\n  - links:\n      - {name: a, url: \"https://a.example.com\"}\n",
	}
	for name, doc := range fatal {
		if _, err := Validate([]byte(doc)); err == nil {
			t.Errorf("%s: accepted, want it refused", name)
		}
	}

	// The schemes ghome allows, including the four that merely look odd.
	ok := []string{
		"https://a.example.com", "http://a.example.com", "chrome://bookmarks",
		"file:///Users/me/x.pdf", "ssh://git@host", "mailto:a@b.c", "vscode://file/x",
		"smb://server/share", "rdp://host", "tel:+123",
	}
	for _, u := range ok {
		doc := "groups:\n  - name: g\n    links:\n      - {name: a, url: \"" + u + "\"}\n"
		if _, err := Validate([]byte(doc)); err != nil {
			t.Errorf("%s: refused (%v), want it accepted", u, err)
		}
	}
}

// The message ends up on endpoints that answer without a token, so it must not
// carry the value or the entry name.
func TestValidateNeverEchoesValues(t *testing.T) {
	docs := []string{
		"groups:\n  - name: g\n    links:\n      - {name: 财务系统, url: \"\"}\n",
		"groups:\n  - name: g\n    links:\n      - {name: 日记, url: \"https://diary.internal:8080/%zz\"}\n",
	}
	for _, doc := range docs {
		_, err := Validate([]byte(doc))
		if err == nil {
			t.Fatalf("accepted: %s", doc)
		}
		for _, secret := range []string{"财务系统", "日记", "diary.internal", "%zz"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q echoes %q", err, secret)
			}
		}
	}
}

// An empty group is dropped by ghome SILENTLY: no refusal, and no warning
// either. This mirror only reports what ghome itself would report, so it must
// not invent one.
func TestValidateDropsAnEmptyGroupSilently(t *testing.T) {
	res, err := Validate([]byte(`groups:
  - name: kept
    links:
      - {name: a, url: "https://a.example.com"}
  - name: empty
    links: []
`))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if res.Groups != 1 || res.Links != 1 {
		t.Errorf("Result = %+v, want the empty group left out of the counts", res)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none: ghome drops empty groups quietly", res.Warnings)
	}
}

// ghome trims a url before validating it, so surrounding whitespace loads
// there. Refusing it here would reject a document ghome would have served.
func TestValidateTrimsURLsLikeGhome(t *testing.T) {
	doc := func(url string) string {
		return "groups:\n  - name: g\n    links:\n      - {name: a, url: " + strconv.Quote(url) + "}\n"
	}
	for _, url := range []string{
		" https://a.example.com",
		"https://a.example.com ",
		"\thttps://a.example.com\t",
		"\u00a0https://a.example.com",
	} {
		if _, err := Validate([]byte(doc(url))); err != nil {
			t.Errorf("url %q: refused (%v), want it accepted", url, err)
		}
	}
	// A genuinely broken url is still refused, whitespace or not.
	if _, err := Validate([]byte(doc(" https://a.example.com/%zz "))); err == nil {
		t.Error("an unparseable url was accepted")
	}
	// The engine url gets the same treatment.
	engine := "search:\n  engines:\n    - {name: e, url: " + strconv.Quote(" https://e.example.com/?q={q} ") +
		"}\ngroups:\n  - name: g\n    links:\n      - {name: a, url: \"https://a.example.com\"}\n"
	if _, err := Validate([]byte(engine)); err != nil {
		t.Errorf("padded engine url refused: %v", err)
	}
}

// A key this mirror does not model is never decoded, so yaml.v3 never reports
// the type error ghome turns into a refusal. These are the ones the writer could
// plausibly grow into; a type error in any of them must fail the document.
func TestValidateRefusesTypeErrorsInEveryModelledField(t *testing.T) {
	good := "groups:\n  - name: g\n    links:\n      - {name: a, url: \"https://a.example.com\"}\n"
	cases := map[string]string{
		"style.radius":         "style:\n  radius: x\n",
		"style.accent":         "style:\n  accent: [x]\n",
		"style as scalar":      "style: 42\n",
		"status.interval":      "status:\n  interval: soon\n",
		"status.interval list": "status:\n  interval: [x]\n",
		"status.concurrency":   "status:\n  concurrency: \"abc\"\n",
		"group.color":          "groups:\n  - name: g\n    color: [x]\n",
		"group.icon":           "groups:\n  - name: g\n    icon: [x]\n",
		"group.description":    "groups:\n  - name: g\n    description: [x]\n",
		"link.icon":            "link-icon",
		"link.tags":            "link-tags",
		"link.hotkey":          "link-hotkey",
		"link.newTab":          "link-newtab",
	}
	linkField := map[string]string{
		"link-icon":   "icon: [x]",
		"link-tags":   "tags: x",
		"link-hotkey": "hotkey: [x]",
		"link-newtab": "newTab: maybe",
	}
	for name, prefix := range cases {
		doc := prefix
		if body, ok := linkField[prefix]; ok {
			doc = "groups:\n  - name: g\n    links:\n      - {name: a, url: \"https://a.example.com\", " + body + "}\n"
		} else if strings.HasPrefix(doc, "groups:") {
			doc += ""
		}
		if strings.Contains(doc, "groups:") && !strings.Contains(doc, "links:") {
			doc += "    links:\n      - {name: a, url: \"https://a.example.com\"}\n"
		}
		if _, err := Validate([]byte(doc)); err == nil {
			t.Errorf("%s: accepted, want it refused (ghome fails the document over this)\n%s", name, doc)
		}
	}
	if _, err := Validate([]byte(good)); err != nil {
		t.Errorf("the plain document was refused: %v", err)
	}
}
