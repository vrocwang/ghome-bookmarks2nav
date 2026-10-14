// Package bookmarks converts a browser's exported bookmarks into a ghome
// nav.yaml document.
//
// The input is the Netscape bookmark format every browser writes when you ask
// it to export bookmarks: nested <DL> lists, one <DT><H3> per folder and one
// <DT><A HREF> per bookmark. The two things that make it awkward are that each
// <A> may carry a base64 data-URI icon of a few kilobytes (ghome resolves icons
// itself, so those are dropped) and that its text is HTML-escaped.
//
// This is a standalone tool: it knows the shape of a nav.yaml document through
// internal/navcheck rather than importing ghome, so it builds and ships on its
// own.
package bookmarks
