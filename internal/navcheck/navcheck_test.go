package navcheck

import (
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

// An empty group is dropped by ghome, not fatal, so it is a warning.
func TestValidateWarnsAboutAnEmptyGroup(t *testing.T) {
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
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "empty") {
		t.Errorf("Warnings = %v, want one about the empty group", res.Warnings)
	}
}
