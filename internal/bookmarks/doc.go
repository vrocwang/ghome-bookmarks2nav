// Package bookmarks converts a browser's exported bookmarks into a ghome
// nav.yaml document.
//
// The input is the Netscape bookmark format every browser writes when you ask
// it to export bookmarks: nested <DL> lists, one <DT><H3> per folder and one
// <DT><A HREF> per bookmark. The two things that make it awkward are that each
// <A> may carry a base64 data-URI icon of a few kilobytes (ghome resolves icons
// itself, so those are dropped) and that its text is HTML-escaped.
//
// This is a standalone tool: it does not import ghome and carries no copy of
// its schema. It knows the shape it has to produce, keeps to it while writing,
// and checks the result against the three requirements that matter (a document
// that parses, groups that hold links, entries with a name and a url).
package bookmarks
