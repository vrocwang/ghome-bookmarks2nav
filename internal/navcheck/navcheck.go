// Package navcheck validates a nav.yaml document without importing ghome.
//
// It carries just enough of ghome's schema to answer the one question this tool
// cares about: would ghome accept what the writer produces? The rules below
// mirror ghome/internal/nav, which is the source of truth.
//
// The mirror is partial by design and has no drift guard: it models the fields
// this tool can emit, plus the rest of ghome's fields whose TYPE error is fatal
// there (a type error refuses the document; a bad colour, hotkey or number is
// only a warning, so those values are not re-checked). A field ghome adds later
// is invisible here until this file learns it. Replacing the mirror with
// ghome's parser would mean vendoring that project's code, which is not this
// tool's call to make while ghome carries no licence.
//
// The split matters for the user, not for tidiness: a document ghome would
// refuse leaves the page serving the previous revision and forces the operator
// to read lastError to find out why. Failing at conversion time is cheaper.
package navcheck

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Document is the part of nav.yaml that decides whether it loads.
//
// The types have to match ghome's exactly, and every field whose TYPE error is
// fatal there has to be present: a key this struct does not model is never
// decoded, so yaml.v3 never reports the type error ghome turns into a refusal.
// Values are not checked here -- ghome's value checks (bad colour, bad hotkey,
// out-of-range number) are warnings, not refusals -- with one exception: a
// duration has to parse, because ghome fails the whole document over that.
type Document struct {
	Site struct {
		Title    string `yaml:"title"`
		Subtitle string `yaml:"subtitle"`
		Logo     string `yaml:"logo"`
		Footer   string `yaml:"footer"`
	} `yaml:"site"`
	Layout string `yaml:"layout"`
	Style  struct {
		Accent     string `yaml:"accent"`
		Background string `yaml:"background"`
		Surface    string `yaml:"surface"`
		Text       string `yaml:"text"`
		Muted      string `yaml:"muted"`
		Border     string `yaml:"border"`
		Font       string `yaml:"font"`
		Radius     int    `yaml:"radius"`
		FontSize   int    `yaml:"fontSize"`
		Width      int    `yaml:"width"`
		IconSize   int    `yaml:"iconSize"`
		Density    string `yaml:"density"`
		CardStyle  string `yaml:"cardStyle"`
		CustomCSS  string `yaml:"customCss"`
	} `yaml:"style"`
	Search struct {
		Enabled *bool  `yaml:"enabled"`
		Hotkey  string `yaml:"hotkey"`
		Engines []struct {
			Name    string `yaml:"name"`
			URL     string `yaml:"url"`
			Keyword string `yaml:"keyword"`
		} `yaml:"engines"`
	} `yaml:"search"`
	Status struct {
		Enabled     *bool    `yaml:"enabled"`
		Interval    Duration `yaml:"interval"`
		Timeout     Duration `yaml:"timeout"`
		Concurrency int      `yaml:"concurrency"`
	} `yaml:"status"`
	Groups []struct {
		Name        string `yaml:"name"`
		Icon        string `yaml:"icon"`
		Description string `yaml:"description"`
		Color       string `yaml:"color"`
		Private     bool   `yaml:"private"`
		Links       []struct {
			Name        string   `yaml:"name"`
			URL         string   `yaml:"url"`
			Icon        string   `yaml:"icon"`
			Description string   `yaml:"description"`
			Tags        []string `yaml:"tags"`
			Hotkey      string   `yaml:"hotkey"`
			NewTab      *bool    `yaml:"newTab"`
			Pinned      bool     `yaml:"pinned"`
			Probe       *bool    `yaml:"probe"`
			Private     bool     `yaml:"private"`
		} `yaml:"links"`
	} `yaml:"groups"`
}

// Duration mirrors ghome's: a value that is not a duration (or a bare number of
// seconds) fails the whole document there, so it must fail here too.
type Duration time.Duration

// UnmarshalYAML accepts what ghome accepts: a number of seconds, or anything
// time.ParseDuration understands.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var v any
	if err := node.Decode(&v); err != nil {
		return err
	}
	switch got := v.(type) {
	case int:
		*d = Duration(time.Duration(got) * time.Second)
		return nil
	case float64:
		*d = Duration(time.Duration(got * float64(time.Second)))
		return nil
	case string:
		parsed, err := time.ParseDuration(got)
		if err != nil {
			return fmt.Errorf("expected a duration such as 60s, or a number of seconds")
		}
		*d = Duration(parsed)
		return nil
	}
	return fmt.Errorf("expected a duration such as 60s, or a number of seconds")
}

// Result is what the tool reports and acts on.
type Result struct {
	// Groups and Links count what is in the document.
	Groups, Links int
	// Private counts the entries a locked viewer would not receive.
	Private int
	// Warnings are problems ghome tolerates: it loads the document and tells the
	// operator about them.
	Warnings []string
}

// Loaded reports whether the document carries anything at all.
func (r Result) Loaded() bool { return r.Groups > 0 }

// dangerousSchemes can execute or embed content when they end up in an href.
// ghome blocks exactly these four and allows everything else, because a
// navigation page legitimately links to ssh://, vscode://, smb:// and friends.
var dangerousSchemes = map[string]bool{
	"javascript": true,
	"data":       true,
	"vbscript":   true,
	"blob":       true,
}

// Validate parses a nav.yaml document the way ghome would, returning the fatal
// error it would report or the warnings it would print.
func Validate(doc []byte) (Result, error) {
	var res Result

	var c Document
	if err := yaml.Unmarshal(doc, &c); err != nil {
		return res, fmt.Errorf("invalid yaml: %w", err)
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	for i, e := range c.Search.Engines {
		if strings.TrimSpace(e.Name) == "" {
			add("search.engines[%d]: name is required", i)
		}
		if !strings.Contains(e.URL, "{q}") {
			add("search.engines[%d] (%s): url must contain the {q} placeholder", i, e.Name)
		} else if err := checkURL(strings.TrimSpace(strings.ReplaceAll(e.URL, "{q}", "x"))); err != nil {
			add("search.engines[%d] (%s).url: %v", i, e.Name, err)
		}
	}

	for gi, g := range c.Groups {
		if strings.TrimSpace(g.Name) == "" {
			add("groups[%d]: name is required", gi)
		}
		links := 0
		for li, l := range g.Links {
			if strings.TrimSpace(l.Name) == "" {
				add("groups[%d].links[%d]: name is required", gi, li)
			}
			if strings.TrimSpace(l.URL) == "" {
				// No entry name in this text: ghome shows it on an
				// unauthenticated endpoint, and a private entry's name is
				// exactly what the private tier exists to withhold.
				add("groups[%d].links[%d]: url is required", gi, li)
				continue
			}
			// ghome trims the url before validating it, so a value with
			// surrounding whitespace loads there; refusing it here would fail
			// a document ghome would have served.
			if err := checkURL(strings.TrimSpace(l.URL)); err != nil {
				add("groups[%d].links[%d].url: %v", gi, li, err)
			}
			links++
			if g.Private || l.Private {
				res.Private++
			}
		}
		if links == 0 {
			// ghome drops an empty group silently, so there is nothing to warn
			// about: this mirror only reports what ghome itself would report.
			continue
		}
		res.Groups++
		res.Links += links
	}

	if res.Groups == 0 {
		add("no links found: nav.yaml needs at least one group containing at least one link")
	}
	if len(problems) > 0 {
		return res, errors.New(strings.Join(problems, "; "))
	}
	return res, nil
}

// checkURL mirrors ghome's rule: a scheme is required, the four dangerous ones
// are refused, and http(s) needs a host. The value itself is never echoed --
// ghome's own message ends up on an unauthenticated endpoint, and a stored URL
// can carry a token.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a parseable url (check the characters; the value is not echoed here because it may be private)")
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "":
		return errors.New("missing scheme (expected something like https://)")
	case dangerousSchemes[scheme]:
		return fmt.Errorf("scheme %q is not allowed", u.Scheme)
	case (scheme == "http" || scheme == "https") && u.Host == "":
		return errors.New("missing host")
	}
	return nil
}

// Decode reads a document into its typed form. Tests use it to prove that what
// the tool wrote is what it meant to write, quoting and entities included.
func Decode(doc []byte) (*Document, error) {
	var c Document
	if err := yaml.NewDecoder(bytes.NewReader(doc)).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
