package bookmarks

import (
	"strings"
	"testing"
)

// A miniature export with the shapes that matter: a nested folder, an entity in
// a folder name, an entity and a quote in a title, a self-closing <DT><A>, a
// folder holding only folders, and a link filed at the top level.
const sample = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1" PERSONAL_TOOLBAR_FOLDER="true">书签栏</H3>
    <DL><p>
        <DT><A HREF="https://top.example.com" ADD_DATE="2" ICON="data:image/png;base64,AAAA">顶层链接</A>
        <DT><H3>K8S&amp;&amp;Docker</H3>
        <DL><p>
            <DT><A HREF="https://k8s.example.com" ADD_DATE="3">k8s &quot;入门&quot; &amp; 实践</A>
            <DT><H3>Train</H3>
            <DL><p>
                <DT><A HREF="https://train.example.com">Labs</A>
            </DL><p>
            <DT><H3>只有文件夹</H3>
            <DL><p>
                <DT><H3>更深</H3>
                <DL><p>
                    <DT><A HREF="https://deep.example.com">深层</A>
                </DL><p>
            </DL><p>
        </DL><p>
        <DT><A HREF="javascript:alert(1)">坏链接</A>
        <DT><A HREF="https://dupe.example.com">第一份</A>
        <DT><A HREF="https://dupe.example.com">重复项</A>
        <DT><A HREF="">没有地址</A>
    </DL><p>
</DL><p>
`

func parseSample(t *testing.T) Folder {
	t.Helper()
	root, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return root
}

func TestParseKeepsTheFolderTree(t *testing.T) {
	root := parseSample(t)

	if got := root.Count(); got != 8 {
		t.Fatalf("Count() = %d, want 8", got)
	}
	if got := root.Depth(); got != 5 {
		t.Errorf("Depth() = %d, want 5", got)
	}

	// The toolbar folder is the root's first child, and the top-level link is
	// filed in it, not in the root.
	if len(root.Folders) != 1 || root.Folders[0].Name != "书签栏" {
		t.Fatalf("root folders = %+v, want one named 书签栏", root.Folders)
	}
	toolbar := root.Folders[0]
	if len(toolbar.Links) != 5 {
		t.Errorf("toolbar links = %d, want 5 (%+v)", len(toolbar.Links), toolbar.Links)
	}
	if toolbar.Links[0].Name != "顶层链接" || toolbar.Links[0].URL != "https://top.example.com" {
		t.Errorf("first toolbar link = %+v", toolbar.Links[0])
	}

	// Entities are decoded in both folder names and titles, and whitespace in a
	// title is collapsed.
	k8s := toolbar.Folders[0]
	if k8s.Name != "K8S&&Docker" {
		t.Errorf("folder name = %q, want K8S&&Docker", k8s.Name)
	}
	if got := k8s.Links[0].Name; got != `k8s "入门" & 实践` {
		t.Errorf("link name = %q", got)
	}

	// A folder that only holds folders still contributes its depth, and the link
	// is filed under it.
	train := k8s.Folders[0]
	if train.Name != "Train" || len(train.Links) != 1 || train.Links[0].URL != "https://train.example.com" {
		t.Errorf("nested folder = %+v", train)
	}
	onlyFolders := k8s.Folders[1]
	if len(onlyFolders.Links) != 0 || len(onlyFolders.Folders) != 1 {
		t.Fatalf("expected a folder holding only folders, got %+v", onlyFolders)
	}
	if deep := onlyFolders.Folders[0]; deep.Name != "更深" || len(deep.Links) != 1 {
		t.Errorf("deepest folder = %+v", deep)
	}
}

// A <DL> with no <H3> in front of it -- which the export's own wrapper is --
// must not open a level of its own.
func TestParseIgnoresTheUnnamedWrapper(t *testing.T) {
	doc := `<DL><p><DT><H3>F</H3><DL><p><DT><A HREF="https://a.example.com">A</A></DL><p></DL>`
	root, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Folders) != 1 || root.Folders[0].Name != "F" {
		t.Fatalf("folders = %+v, want exactly F", root.Folders)
	}
	if len(root.Folders[0].Links) != 1 {
		t.Errorf("links = %+v, want the one under F", root.Folders[0].Links)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	root, err := Parse(strings.NewReader("not a bookmark file at all"))
	if err != nil {
		t.Fatalf("Parse() error = %v (an unknown file should just be empty)", err)
	}
	if root.Count() != 0 {
		t.Errorf("Count() = %d, want 0", root.Count())
	}
}
