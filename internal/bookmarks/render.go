package bookmarks

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Options control how a tree becomes a nav.yaml document.
type Options struct {
	// Title is written as site.title. Defaults to "导航" like ghome does.
	Title string
	// Subtitle is written as site.subtitle.
	Subtitle string
	// Leaf uses only the innermost folder name for a group instead of its full
	// path. Short names are easier to scan; the path keeps two folders with the
	// same name apart.
	Leaf bool
	// Single puts every bookmark in one group with this name, ignoring the
	// folder structure.
	Single string
	// MaxName truncates a bookmark title to this many runes, 0 for no limit.
	// Exports carry titles like a 200-character YouTube headline, which is more
	// than a nav entry can show.
	MaxName int
	// GroupPrivate marks each group private instead of every link, which is
	// shorter and means the same thing.
	GroupPrivate bool
	// Search turns the filter box on. A converted export is long -- hundreds of
	// bookmarks -- and the box is how you find one, so the tool offers it even
	// though ghome's default is already on.
	Search bool
}

// Report describes what a conversion did, so the tool can say it out loud.
type Report struct {
	Groups     int
	Links      int
	Duplicates int
	Truncated  int
	// Named counts entries that had no title and were labelled by host.
	Named       int
	Unsupported []Skipped
}

// Skipped is a bookmark that was left out, and why.
type Skipped struct {
	Name   string
	URL    string
	Reason string
}

// group is a nav.yaml group in the making.
type group struct {
	name  string
	links []Link
}

// Render converts a parsed export into a nav.yaml document.
//
// Every emitted link is private: a bookmark export is a personal list, and the
// whole point of `private: true` is that the page hands it only to a viewer who
// can read the config repository. The flip side is that the config must live in
// a private repository and the source must be gitea/forgejo, or the private tier
// stays hidden from everyone -- the tool prints that reminder.
func Render(root Folder, opts Options) ([]byte, Report, error) {
	var rep Report
	groups, skipped := collect(root, opts)
	rep.Unsupported = skipped

	// Deduplicate by URL across the whole document: two entries for one URL
	// share one icon and one probe target, so the second is noise. The first
	// occurrence wins, which keeps the folder the user filed it in first.
	seen := map[string]bool{}
	for gi := range groups {
		kept := groups[gi].links[:0]
		for _, l := range groups[gi].links {
			if seen[l.URL] {
				rep.Duplicates++
				continue
			}
			seen[l.URL] = true
			kept = append(kept, l)
		}
		groups[gi].links = kept
	}
	// A group whose links all went away is not worth a card.
	kept := groups[:0]
	for _, g := range groups {
		if len(g.links) > 0 {
			kept = append(kept, g)
		}
	}
	groups = kept

	if len(groups) == 0 {
		if len(skipped) > 0 {
			// Everything was dropped for a concrete reason; say so instead of
			// the generic "no bookmarks found", which reads like a bad file.
			return nil, rep, fmt.Errorf("all %d bookmarks were skipped: %s", len(skipped), skipped[0].Reason)
		}
		return nil, rep, ErrNoBookmarks
	}

	title := opts.Title
	if title == "" {
		title = "导航"
	}

	var b strings.Builder
	b.WriteString("# 由浏览器书签导出转换而来（bookmarks-to-nav）。\n")
	b.WriteString("# 每一条都是 private：只有在页面上用「能读配置仓的令牌」解锁后才看得到。\n")
	b.WriteString("# 这要求 --source=gitea/forgejo 且配置仓是私有仓，详见 README 的「私密链接」。\n")
	b.WriteString("site:\n")
	fmt.Fprintf(&b, "  title: %s\n", yamlString(title))
	if opts.Subtitle != "" {
		fmt.Fprintf(&b, "  subtitle: %s\n", yamlString(opts.Subtitle))
	}
	if opts.Search {
		b.WriteString("\nsearch:\n  enabled: true\n")
	}
	b.WriteString("\ngroups:\n")

	for _, g := range groups {
		rep.Groups++
		fmt.Fprintf(&b, "  - name: %s\n", yamlString(g.name))
		if opts.GroupPrivate {
			b.WriteString("    private: true\n")
		}
		b.WriteString("    links:\n")
		for _, l := range g.links {
			rep.Links++
			name := l.Name
			if strings.TrimSpace(name) == "" {
				// ghome requires a name, and a blank one makes it refuse the
				// whole document. Firefox exports allow an untitled bookmark,
				// so fall back to the host -- there is always something to call
				// it by.
				name = hostLabel(l.URL)
				rep.Named++
			}
			if opts.MaxName > 0 {
				if r := []rune(name); len(r) > opts.MaxName {
					name = strings.TrimSpace(string(r[:opts.MaxName])) + "…"
					rep.Truncated++
				}
			}
			fmt.Fprintf(&b, "      - {name: %s, url: %s", yamlString(name), yamlString(l.URL))
			if !opts.GroupPrivate {
				b.WriteString(", private: true")
			}
			b.WriteString("}\n")
		}
	}
	doc := []byte(b.String())
	if err := verify(doc); err != nil {
		// The writer is supposed to make this impossible; if it ever happens,
		// failing is better than writing a document that stops the page from
		// updating and leaves the operator to read /healthz for the reason.
		return nil, rep, fmt.Errorf("internal error: the generated document is not the shape ghome expects: %w", err)
	}
	return doc, rep, nil
}

// verify checks the invariants ghome requires of the document this tool writes.
//
// It deliberately does not re-check ghome's rules -- there is no second copy of
// its schema here. The writer already decides which links are servable (one
// scheme rule, applied while collecting) and gives every entry a name. What is
// left is the shape: a document that parses, groups that hold at least one link,
// and entries with the two keys ghome requires. This is a net under the writer,
// not a validator for hand-written configs.
func verify(doc []byte) error {
	var parsed struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Links []struct {
				Name string `yaml:"name"`
				URL  string `yaml:"url"`
			} `yaml:"links"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		return fmt.Errorf("it does not parse: %w", err)
	}
	if len(parsed.Groups) == 0 {
		return errors.New("it has no groups")
	}
	for gi, g := range parsed.Groups {
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("groups[%d] has no name", gi)
		}
		if len(g.Links) == 0 {
			// ghome refuses a document where no group holds a link at all,
			// and a group with no links is a card with nothing in it.
			return fmt.Errorf("groups[%d] (%s) has no links", gi, g.Name)
		}
		for li, l := range g.Links {
			if strings.TrimSpace(l.Name) == "" {
				return fmt.Errorf("groups[%d].links[%d] has no name", gi, li)
			}
			if strings.TrimSpace(l.URL) == "" {
				return fmt.Errorf("groups[%d].links[%d] (%s) has no url", gi, li, l.Name)
			}
		}
	}
	return nil
}

// collect walks the tree, producing one group per folder that holds links.
func collect(root Folder, opts Options) ([]group, []Skipped) {
	var (
		groups  []group
		skipped []Skipped
	)

	// keep decides whether a link is usable. The caller trims first and keeps
	// the trimmed value, so the string validated here is the string written:
	// ghome trims a url before checking it, and validating one string while
	// emitting another is how a page ends up with an href nobody checked.
	keep := func(l Link) bool {
		if l.URL == "" {
			skipped = append(skipped, Skipped{Name: l.Name, Reason: "no href"})
			return false
		}
		if reason := unsupported(l.URL); reason != "" {
			skipped = append(skipped, Skipped{Name: l.Name, URL: l.URL, Reason: reason})
			return false
		}
		return true
	}

	if opts.Single != "" {
		var links []Link
		root.Walk(func(f *Folder) {
			for _, l := range f.Links {
				l.URL = strings.TrimSpace(l.URL)
				if keep(l) {
					links = append(links, l)
				}
			}
		})
		if len(links) > 0 {
			groups = append(groups, group{name: opts.Single, links: links})
		}
		return groups, skipped
	}

	// path is the chain of folder names from the root, used as the group name.
	var walk func(f Folder, path []string)
	walk = func(f Folder, path []string) {
		var links []Link
		for _, l := range f.Links {
			l.URL = strings.TrimSpace(l.URL)
			if keep(l) {
				links = append(links, l)
			}
		}
		if len(links) > 0 {
			groups = append(groups, group{name: groupName(path, opts.Leaf), links: links})
		}
		for _, sub := range f.Folders {
			walk(*sub, append(path, sub.Name))
		}
	}
	walk(root, nil)

	// Folders that hold the most links first: a nav page reads better when the
	// biggest group is not buried, and the order is stable across re-exports.
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].links) != len(groups[j].links) {
			return len(groups[i].links) > len(groups[j].links)
		}
		return groups[i].name < groups[j].name
	})
	return groups, skipped
}

// hostLabel names an untitled bookmark after its host, which is at least
// recognisable in a list of links.
func hostLabel(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "未命名链接"
	}
	return u.Hostname()
}

// groupName turns a folder path into a readable group name.
func groupName(path []string, leaf bool) string {
	parts := make([]string, 0, len(path))
	for _, p := range path {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "书签"
	}
	if leaf {
		return parts[len(parts)-1]
	}
	return strings.Join(parts, " / ")
}

// unsupported reports why ghome would refuse this URL, or "" when it is fine.
//
// It answers the same question ghome's own check does -- does this url have a
// scheme, is it one of the four dangerous ones, and does http(s) have a host --
// and it answers it the same way: url.Parse, not string prefixes. A prefix test
// gets http(s) wrong for every url whose whitespace TrimSpace removes but
// url.Parse does not (a trailing NBSP, for one), and the two mirrors then
// disagree about a url ghome accepts.
//
// Since the caller trims the url before calling this, the value checked here is
// the value written, and verify() checks the shape on the way out.
func unsupported(raw string) string {
	// Trim first, the way ghome does: TrimSpace removes Unicode whitespace that
	// url.Parse does not (a trailing NBSP, say), so parsing the raw value would
	// refuse a url ghome accepts.
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "empty url"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "not a parseable url"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return "no scheme (ghome needs one, like https://)"
	}
	switch scheme {
	case "javascript", "data", "vbscript", "blob":
		return "scheme " + scheme + " is refused by ghome"
	}
	if (scheme == "http" || scheme == "https") && u.Host == "" {
		return "http(s) url without a host"
	}
	return ""
}

// yamlString quotes a value for a nav.yaml flow mapping.
//
// Double quotes throughout, with the escapes YAML actually defines. Bookmark
// titles need it: they routinely contain ':' and '"', and a folder here is
// called "K8S&&Docker". Control characters are dropped rather than escaped --
// YAML has no portable escape for every one, and a browser would not have
// exported them.
func yamlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				// Escaped, not dropped: dropping can leave the value empty,
				// and ghome refuses a document over an empty name -- one
				// control character in one title would fail the whole export.
				// YAML double-quoted scalars define \xNN.
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
