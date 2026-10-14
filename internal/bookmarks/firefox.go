package bookmarks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// firefoxNode is one entry in a Firefox places export.
//
// Firefox's "Backup…" and "Export Bookmarks to JSON…" both write this shape:
// a recursive tree with typeCode 1 for a bookmark and 2 for a folder. There is
// no HTML export in the UI any more, so this is the format most Firefox users
// will hand the tool.
type firefoxNode struct {
	Title string `json:"title"`
	// Root is set on the top-level containers ("bookmarksMenuFolder",
	// "toolbarFolder", "unfiledBookmarksFolder"). Their Title is the internal
	// identifier -- "menu", "toolbar" -- which makes a poor group name.
	Root     string        `json:"root"`
	URI      string        `json:"uri"`
	TypeCode int           `json:"typeCode"`
	Children []firefoxNode `json:"children"`
	// The folding state and the icon travel along; the icon is a data: URI of a
	// few kilobytes and is dropped like the ICON attribute in an HTML export.
	IconURI string `json:"iconUri"`
}

// LooksLikeFirefoxJSON reports whether data is a places export rather than a
// Netscape HTML one. Detection is worth having because the two formats look
// nothing alike and a wrong guess produces "no bookmarks found", which reads
// like a broken file rather than a wrong format.
func LooksLikeFirefoxJSON(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return false
	}
	// Cheap discriminator before paying for a parse: every places export has at
	// least one of these.
	for _, marker := range []string{`"typeCode"`, `"text/x-moz-place`, `"children"`} {
		if strings.Contains(trimmed, marker) {
			return true
		}
	}
	return false
}

// ParseFirefox reads a Firefox places export (the JSON from "Backup…" or
// "Export Bookmarks to JSON…").
//
// The two top-level containers Firefox always writes — the bookmarks menu and
// the toolbar — are kept as folders so that their names do not make every group
// one level deeper than it looks in the Browser Library. "Other Bookmarks"
// ("unfiled") is kept as well when it holds anything.
func ParseFirefox(r io.Reader) (Folder, error) {
	var root firefoxNode
	if err := json.NewDecoder(r).Decode(&root); err != nil {
		return Folder{}, fmt.Errorf("read firefox bookmarks: %w", err)
	}

	// An export is one object; tolerate a list of them, which some tools write.
	if root.Title == "" && root.URI == "" && len(root.Children) == 0 {
		return Folder{}, ErrNoBookmarks
	}

	out := Folder{}
	for _, child := range root.Children {
		sub := convertFirefox(child)
		// A container that held nothing is not a group.
		if len(sub.Links) == 0 && len(sub.Folders) == 0 {
			continue
		}
		out.Folders = append(out.Folders, sub)
	}
	return out, nil
}

// rootNames are the display names for the containers Firefox roots the tree
// with. They match what the Bookmarks menu shows, instead of the internal
// identifier that sits in the export.
var rootNames = map[string]string{
	"bookmarksMenuFolder":    "书签菜单",
	"toolbarFolder":          "书签工具栏",
	"unfiledBookmarksFolder": "其他书签",
	"mobileFolder":           "移动端书签",
	"placesRoot":             "书签",
}

func convertFirefox(n firefoxNode) *Folder {
	name := cleanText(n.Title)
	if display, ok := rootNames[n.Root]; ok {
		name = display
	}
	folder := &Folder{Name: name}
	if isBookmark(n) {
		folder.Links = append(folder.Links, newLink(n))
		return folder
	}
	for _, child := range n.Children {
		if isBookmark(child) {
			folder.Links = append(folder.Links, newLink(child))
			continue
		}
		sub := convertFirefox(child)
		// A container that holds nothing is not worth a group.
		if len(sub.Links) > 0 || len(sub.Folders) > 0 {
			folder.Folders = append(folder.Folders, sub)
		}
	}
	return folder
}

func isBookmark(n firefoxNode) bool {
	if n.TypeCode == 1 {
		return true
	}
	// Some exports omit typeCode; a node with a uri and no children is a link.
	return n.TypeCode != 2 && strings.TrimSpace(n.URI) != "" && len(n.Children) == 0
}

func newLink(n firefoxNode) Link {
	return Link{Name: cleanText(n.Title), URL: strings.TrimSpace(n.URI)}
}
