package bookmarks

import (
	"strings"
	"testing"

	"github.com/vrocwang/ghome-bookmarks2nav/internal/navcheck"
)

// A miniature places export with the shapes that matter: the three root
// containers Firefox always writes, a nested folder, a node without typeCode,
// an entry with no title, an empty folder and a data: icon to be dropped.
const firefoxJSON = `{
  "guid": "root________", "title": "", "typeCode": 2, "root": "placesRoot",
  "children": [
    { "guid": "menu________", "title": "menu", "typeCode": 2, "root": "bookmarksMenuFolder",
      "children": [
        { "guid": "a", "title": "Mozilla &amp; 更多", "typeCode": 1,
          "uri": "https://www.mozilla.org/", "iconUri": "data:image/png;base64,AAAA" },
        { "guid": "b", "title": "工具", "typeCode": 2, "children": [
            { "guid": "c", "title": "内网", "typeCode": 1, "uri": "http://nas:8080/", "tags": ["homelab"] },
            { "guid": "d", "title": "空目录", "typeCode": 2, "children": [] }
        ]},
        { "title": "没有 typeCode 的链接", "uri": "https://plain.example.com/" },
        { "guid": "e", "title": "", "typeCode": 1, "uri": "https://untitled.example.com/" }
      ]},
    { "guid": "toolbar_____", "title": "toolbar", "typeCode": 2, "root": "toolbarFolder",
      "children": [
        { "guid": "f", "title": "GitHub", "typeCode": 1, "uri": "https://github.com/" }
      ]},
    { "guid": "unfiled_____", "title": "unfiled", "typeCode": 2, "root": "unfiledBookmarksFolder",
      "children": [] }
  ]
}`

func parseFirefox(t *testing.T, doc string) Folder {
	t.Helper()
	root, err := ParseFirefox(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ParseFirefox() error = %v", err)
	}
	return root
}

func TestLooksLikeFirefoxJSON(t *testing.T) {
	if !LooksLikeFirefoxJSON([]byte(firefoxJSON)) {
		t.Error("the places export was not recognised")
	}
	if LooksLikeFirefoxJSON([]byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p></DL>`)) {
		t.Error("a Netscape export was mistaken for JSON")
	}
	if LooksLikeFirefoxJSON([]byte("  ")) {
		t.Error("an empty file was mistaken for JSON")
	}
	// Some JSON that is not a places export should not be claimed either.
	if LooksLikeFirefoxJSON([]byte(`{"hello":"world"}`)) {
		t.Error("unrelated JSON was mistaken for a places export")
	}
}

func TestParseFirefoxKeepsTheTreeAndNamesTheRoots(t *testing.T) {
	root := parseFirefox(t, firefoxJSON)

	if got := root.Count(); got != 5 {
		t.Fatalf("Count() = %d, want 5", got)
	}
	// unfiled is empty and must not survive; the two roots that hold something
	// are named the way the Bookmarks menu shows them, not "menu"/"toolbar".
	if len(root.Folders) != 2 {
		t.Fatalf("folders = %d, want the menu and the toolbar only", len(root.Folders))
	}
	if root.Folders[0].Name != "书签菜单" || root.Folders[1].Name != "书签工具栏" {
		t.Errorf("root names = %q / %q", root.Folders[0].Name, root.Folders[1].Name)
	}

	menu := root.Folders[0]
	if len(menu.Links) != 3 {
		t.Fatalf("menu links = %d, want 3 (%+v)", len(menu.Links), menu.Links)
	}
	if menu.Links[0].Name != "Mozilla & 更多" || menu.Links[0].URL != "https://www.mozilla.org/" {
		t.Errorf("first link = %+v", menu.Links[0])
	}
	// A node with a uri and no typeCode is still a link.
	if menu.Links[1].URL != "https://plain.example.com/" {
		t.Errorf("link without typeCode = %+v", menu.Links[1])
	}
	// An untitled bookmark keeps an empty name; the renderer is what refuses it.
	if menu.Links[2].URL != "https://untitled.example.com/" {
		t.Errorf("untitled link = %+v", menu.Links[2])
	}

	// The nested folder and its link survive; the empty one does not.
	if len(menu.Folders) != 1 || menu.Folders[0].Name != "工具" {
		t.Fatalf("menu folders = %+v, want just 工具", menu.Folders)
	}
	tools := menu.Folders[0]
	if len(tools.Links) != 1 || tools.Links[0].URL != "http://nas:8080/" {
		t.Errorf("工具 links = %+v", tools.Links)
	}
	if len(tools.Folders) != 0 {
		t.Errorf("the empty folder survived: %+v", tools.Folders)
	}
}

// The point of the whole path: a Firefox export has to produce a document ghome
// accepts, with everything private.
func TestRenderFirefoxExportIsAccepted(t *testing.T) {
	doc, rep, err := Render(parseFirefox(t, firefoxJSON), Options{Search: true, GroupPrivate: true})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	res, err := navcheck.Validate(doc)
	if err != nil {
		t.Fatalf("the generated document would not load in ghome: %v\n%s", err, doc)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}
	if res.Private != res.Links || res.Private == 0 {
		t.Errorf("private = %d of %d links, want all of them", res.Private, res.Links)
	}
	if rep.Links != 5 {
		t.Errorf("Report.Links = %d, want 5", rep.Links)
	}
	if res.Groups != 3 {
		t.Errorf("groups = %d, want 3 (menu, 工具, toolbar)", res.Groups)
	}
}

func TestParseFirefoxRejectsAnEmptyExport(t *testing.T) {
	if _, err := ParseFirefox(strings.NewReader(`{}`)); err == nil {
		t.Error("an empty object was accepted as an export")
	}
	if _, err := ParseFirefox(strings.NewReader(`not json`)); err == nil {
		t.Error("garbage was accepted")
	}
}
