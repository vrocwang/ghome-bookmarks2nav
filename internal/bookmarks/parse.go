// Package bookmarks converts a browser's exported bookmarks into a ghome
// nav.yaml.
//
// The input is the Netscape bookmark format every browser writes when you ask
// it to export bookmarks: nested <DL> lists, one <DT><H3> per folder and one
// <DT><A HREF> per bookmark. The two things that make it awkward are that each
// <A> may carry a base64 data-URI icon of a few kilobytes (ghome resolves icons
// itself, so those are dropped) and that its text is HTML-escaped.
//
// Nothing here decides policy beyond the shape of the tree: internal/package
// users build a nav.yaml document, the command in cmd/bookmarks-to-nav applies
// the flags.
package bookmarks

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Link is one bookmark.
type Link struct {
	// Name is the bookmark's title, with entities decoded and whitespace
	// collapsed.
	Name string
	// URL is the href, unchanged.
	URL string
}

// Folder is a bookmark folder, or the root of the export.
type Folder struct {
	// Name is the folder title; empty for the root.
	Name string
	// Links are the bookmarks filed directly in this folder.
	Links []Link
	// Folders are its subfolders, in export order.
	Folders []*Folder
	// DepthCapped reports that the export nested folders deeper than maxDepth,
	// so the deepest levels were folded into their ancestor. It is set on the
	// root.
	DepthCapped bool
}

// Depth reports how deep the tree goes below this folder.
func (f Folder) Depth() int {
	max := 0
	for _, sub := range f.Folders {
		if d := sub.Depth(); d > max {
			max = d
		}
	}
	return max + 1
}

// Count reports how many bookmarks the tree holds.
func (f Folder) Count() int {
	n := len(f.Links)
	for _, sub := range f.Folders {
		n += sub.Count()
	}
	return n
}

// Walk calls fn for every folder, parents before children.
func (f *Folder) Walk(fn func(*Folder)) {
	fn(f)
	for i := range f.Folders {
		f.Folders[i].Walk(fn)
	}
}

// ErrNoBookmarks means the file parsed but held nothing to convert.
var ErrNoBookmarks = errors.New("no bookmarks found")

// maxDepth bounds how deep the folder tree may go.
//
// It exists because depth is cheap for a file and expensive for everything
// downstream: the tree operations are recursive, and a group name is the whole
// folder path, so an export nested a million levels deep would overflow the
// stack and, before that, produce a document far larger than its input. A real
// export is under ten levels; sixty is generous. Deeper folders are folded into
// the deepest level that fits rather than being thrown away, and the caller is
// told so it can say it out loud.
const maxDepth = 60

// Parse reads a Netscape bookmark file.
func Parse(r io.Reader) (Folder, error) {
	return parse(r)
}

// ParseWithReport reads a Netscape bookmark file.
//
// It is deliberately tolerant: an export is machine-written and only ever read
// back by the browser that wrote it, so stray attributes, unknown tags and
// folders that only hold other folders are all normal. What it will not do is
// invent structure: a link belongs to the folder it appears in.
func parse(r io.Reader) (Folder, error) {
	z := html.NewTokenizer(r)

	root := &Folder{}
	// The stack holds one node per open <DL>, so a link lands in the folder it
	// is nested under. Nodes are separate allocations rather than addresses into
	// a slice: appending a child would otherwise invalidate the parent pointer.
	stack := []*Folder{root}
	// pending is the folder the next <DL> opens, set by <DT><H3>.
	var pending string
	var havePending bool

	// Link text arrives as one or more text tokens, so it is accumulated until
	// </A>.
	var (
		current *Link
		text    strings.Builder
	)

	closeLink := func() {
		if current == nil {
			return
		}
		current.Name = cleanText(text.String())
		top := stack[len(stack)-1]
		top.Links = append(top.Links, *current)
		current, text = nil, strings.Builder{}
	}

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return Folder{}, fmt.Errorf("read bookmarks: %w", err)
			}
			// A truncated export can end inside an <A>; its bookmark is still
			// one the user saved, so keep it rather than dropping the last one.
			closeLink()
			return *root, nil

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "dl":
				// A <DL> with no announcing <H3> (the export's outer wrapper, or
				// a folder the browser left unnamed) continues the open folder
				// rather than opening another level.
				if !havePending && len(stack) > 0 {
					continue
				}
				// Past the cap the level is not opened: links keep landing in
				// the deepest folder that fits, so a pathological file costs
				// bounded work instead of a stack overflow.
				if len(stack) > maxDepth {
					root.DepthCapped = true
					pending, havePending = "", false
					continue
				}
				child := &Folder{Name: pending}
				pending, havePending = "", false
				parent := stack[len(stack)-1]
				parent.Folders = append(parent.Folders, child)
				stack = append(stack, child)

			case "h3":
				pending, havePending = readTitle(z), true

			case "a":
				closeLink() // defensive: a <DT><A> is self-contained
				link := &Link{}
				for hasAttr {
					var key, value []byte
					key, value, hasAttr = z.TagAttr()
					if strings.EqualFold(string(key), "href") {
						link.URL = string(value)
					}
				}
				current = link

			default:
				// dd, p, dt, hr, meta, title, h1, ...: structure or metadata,
				// nothing to read.
			}

		case html.TextToken:
			if current != nil {
				text.Write(z.Text())
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "dl":
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			case "a":
				closeLink()
			}
		}
	}
}

// readTitle consumes tokens until </H3> and returns the decoded, cleaned title.
func readTitle(z *html.Tokenizer) string {
	var b strings.Builder
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return cleanText(b.String())
		case html.TextToken:
			b.Write(z.Text())
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "h3" {
				return cleanText(b.String())
			}
		}
	}
}

var whitespace = regexp.MustCompile(`\s+`)

// cleanText normalises whitespace and drops the byte-order mark.
//
// It deliberately does NOT decode HTML entities: html.Tokenizer.Text() hands
// back text that is already unescaped, so a second pass would turn a bookmark
// genuinely titled "A&amp;B" (which the browser writes as "A&amp;amp;B") into
// "A&B" -- silently rewriting what the user saved.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\ufeff", "")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}
