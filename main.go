// Command bookmarks-to-nav turns a browser's exported bookmarks into a ghome
// nav.yaml where every entry is private.
//
// It is a standalone tool: it does not import ghome, and its idea of a valid
// nav.yaml lives in internal/navcheck. Nothing has to be installed but this
// binary.
//
// Usage:
//
//	bookmarks-to-nav bookmarks.html > nav.yaml
//	bookmarks-to-nav --out nav.yaml --leaf --search bookmarks.html
//	cat bookmarks.html | bookmarks-to-nav -
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vrocwang/ghome-bookmarks2nav/internal/bookmarks"
	"github.com/vrocwang/ghome-bookmarks2nav/internal/navcheck"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=$(git describe --tags)"
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "bookmarks-to-nav: "+err.Error())
		os.Exit(1)
	}
}

// run is main without the process bits, so a test can drive it directly.
func run(args []string, stdout, stderr *os.File) error {
	fs := flag.NewFlagSet("bookmarks-to-nav", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		out      = fs.String("out", "", "write here instead of stdout")
		title    = fs.String("title", "", `site.title for the document (default "导航")`)
		subtitle = fs.String("subtitle", "我的书签", "site.subtitle")
		leaf     = fs.Bool("leaf", false, "name a group after the innermost folder instead of the full path")
		single   = fs.String("single", "", "put every bookmark in one group with this name, ignoring folders")
		maxName  = fs.Int("max-name", 80, "truncate bookmark titles to this many characters (0 keeps them)")
		perLink  = fs.Bool("link-private", false, "put private on every link instead of on each group")
		search   = fs.Bool("search", false, "write an explicit search: block (the filter box)")
		quiet    = fs.Bool("quiet", false, "do not print the summary")
		showVer  = fs.Bool("version", false, "print the version and exit")
	)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "bookmarks-to-nav %s — turn a browser bookmark export into a private nav.yaml.\n\n", version)
		fmt.Fprintf(stderr, "Usage:\n  bookmarks-to-nav [flags] <bookmarks.html>\n\n")
		fmt.Fprintf(stderr, "The output is a nav.yaml for ghome whose entries are all private, and it is\n"+
			"checked with the same rules ghome applies before it is written.\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Notes:
  • private entries need --source=gitea or forgejo and a PRIVATE config
    repository: the token must not be readable by everyone. In file mode they
    are hidden from everyone (that is the safe direction).
  • javascript:, data:, vbscript: and blob: links are reported and skipped: one
    of them makes ghome refuse the whole document. chrome://, file://, ssh://
    and the rest are kept, because ghome allows them.
  • the embedded base64 icons in the export are dropped: ghome resolves and
    caches site icons itself.

Examples:
  bookmarks-to-nav ~/Downloads/bookmarks.html > nav.yaml
  bookmarks-to-nav --leaf --search --out nav.yaml bookmarks.html
  cat bookmarks.html | bookmarks-to-nav -
`)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVer {
		fmt.Fprintln(stdout, "bookmarks-to-nav "+version)
		return nil
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("expected exactly one bookmark file (or '-' for stdin)")
	}

	src := fs.Arg(0)
	in := os.Stdin
	if src != "-" {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		in = f
	}

	// Two formats reach this tool: the Netscape HTML every browser exports, and
	// the JSON that is now the only built-in Firefox option. They look nothing
	// alike, so the format is detected and named in any error -- guessing wrong
	// otherwise reads as "your file is broken".
	sniff, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return err
	}
	rest := io.MultiReader(bytes.NewReader(sniff), in)

	var (
		root   bookmarks.Folder
		format string
	)
	if bookmarks.LooksLikeFirefoxJSON(sniff) {
		format = "firefox json"
		root, err = bookmarks.ParseFirefox(rest)
	} else {
		format = "netscape html"
		root, err = bookmarks.Parse(rest)
	}
	if err != nil {
		return err
	}
	if root.Count() == 0 {
		return fmt.Errorf("%s: no bookmarks found (read as %s; %s)", src, format,
			"export as HTML from Chrome/Edge/Safari, or as JSON from Firefox")
	}
	if !*quiet {
		fmt.Fprintf(stderr, "read %s as %s\n", src, format)
	}

	doc, rep, err := bookmarks.Render(root, bookmarks.Options{
		Title:        *title,
		Subtitle:     *subtitle,
		Leaf:         *leaf,
		Single:       *single,
		MaxName:      *maxName,
		GroupPrivate: !*perLink,
		Search:       *search,
	})
	if err != nil {
		return err
	}

	// The gate: ghome has to accept it. A document it refuses is worse than no
	// document, because the page keeps serving the previous revision and the
	// operator has to read lastError to find out why.
	res, err := navcheck.Validate(doc)
	if err != nil {
		return fmt.Errorf("the generated document would not load in ghome: %w", err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "note: ghome would warn: "+w)
	}
	if res.Private == 0 {
		return errors.New("the generated document has no private entries, which is the whole point")
	}

	if *out == "" {
		if _, err := stdout.Write(doc); err != nil {
			return err
		}
	} else if err := os.WriteFile(*out, doc, 0o600); err != nil {
		return err
	}

	if !*quiet {
		fmt.Fprintf(stderr, "wrote %d links in %d groups (from %d bookmarks, %d folders)",
			rep.Links, rep.Groups, root.Count(), countFolders(root))
		if rep.Duplicates > 0 {
			fmt.Fprintf(stderr, "; %d duplicate urls dropped", rep.Duplicates)
		}
		if rep.Truncated > 0 {
			fmt.Fprintf(stderr, "; %d titles truncated", rep.Truncated)
		}
		if rep.Named > 0 {
			fmt.Fprintf(stderr, "; %d untitled bookmarks named after their host", rep.Named)
		}
		fmt.Fprintln(stderr)
		for _, s := range rep.Unsupported {
			fmt.Fprintf(stderr, "skipped: %s (%s)\n", firstNonEmpty(s.Name, s.URL), s.Reason)
		}
		if len(rep.Unsupported) > 0 {
			fmt.Fprintln(stderr, "note: those are reported rather than written; one of them in the"+
				" document would make ghome refuse it")
		}
	}
	return nil
}

func countFolders(f bookmarks.Folder) int {
	n := 0
	f.Walk(func(*bookmarks.Folder) { n++ })
	if n > 0 {
		n-- // the root is not a folder the user made
	}
	return n
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "(unnamed)"
}
