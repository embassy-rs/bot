package web

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// Label chips are colored through a style attribute, which html/template is
// entitled to replace wholesale with ZgotmplZ if it doesn't like what's in it.
func TestRendersLabelColors(t *testing.T) {
	f := filter{labels: []string{"bug"}}
	data := queueData{
		Filtered: true,
		Labels: []label{
			newLabel("bug", "d73a4a", f),
			newLabel("enhancement", "a2eeef", f),
		},
		Queue: []queuePR{{
			Repo:   "embassy",
			Title:  "Fix it",
			Labels: []label{newLabel("bug", "d73a4a", f)},
		}},
	}

	var out strings.Builder
	err := queueTemplate.Execute(&out, data)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()

	if strings.Contains(html, "ZgotmplZ") {
		t.Error("template refused to emit a color into the style attribute")
	}
	// Vivid red gets white text, pale cyan gets dark text.
	for _, want := range []string{
		`style="background:#d73a4a;color:#ffffff"`,
		`style="background:#a2eeef;color:#24292f"`,
		`href="/?label=bug&amp;label=enhancement"`, // the unselected chip adds itself
		`class="label off"`,                        // ...and is drawn as not selected
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in rendered page", want)
		}
	}
}

// Flipping a chip must leave the other dimension of the filter untouched, so
// picking a repo can't silently drop the labels you'd already picked.
func TestToggle(t *testing.T) {
	both := filter{repos: []string{"xarxa"}, labels: []string{"bug"}}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"add the first label", filter{}.toggleLabel("bug"), "/?label=bug"},
		{"remove the only label", filter{labels: []string{"bug"}}.toggleLabel("bug"), "/"},
		{"add a second label", filter{labels: []string{"bug"}}.toggleLabel("ci"), "/?label=bug&label=ci"},
		{"remove one of two labels", filter{labels: []string{"bug", "ci"}}.toggleLabel("bug"), "/?label=ci"},

		{"add the first repo", filter{}.toggleRepo("xarxa"), "/?repo=xarxa"},
		{"remove the only repo", filter{repos: []string{"xarxa"}}.toggleRepo("xarxa"), "/"},
		{"add a second repo", filter{repos: []string{"embassy"}}.toggleRepo("xarxa"), "/?repo=embassy&repo=xarxa"},

		// The other dimension survives in both directions.
		{"label toggle keeps repos", both.toggleLabel("ci"), "/?label=bug&label=ci&repo=xarxa"},
		{"repo toggle keeps labels", both.toggleRepo("embassy"), "/?label=bug&repo=embassy&repo=xarxa"},
		{"clearing labels keeps repos", both.toggleLabel("bug"), "/?repo=xarxa"},
		{"clearing repos keeps labels", both.toggleRepo("xarxa"), "/?label=bug"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Errorf("got %q, want %q", test.got, test.want)
			}
		})
	}
}

// Both filter bars render, and a repo chip is a plain chip rather than a label
// wearing a color it doesn't have.
func TestRendersRepoFilter(t *testing.T) {
	f := filter{repos: []string{"xarxa"}}
	data := queueData{
		Filtered: true,
		Repos:    []repoChip{newRepoChip("embassy", f), newRepoChip("xarxa", f)},
		Queue: []queuePR{{
			Repo:    "xarxa",
			RepoURL: f.toggleRepo("xarxa"),
			Title:   "Add a thing",
		}},
	}

	var out strings.Builder
	err := queueTemplate.Execute(&out, data)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()

	for _, want := range []string{
		`<span class="caption">Repos</span>`,
		`class="chip off" href="/?repo=embassy&amp;repo=xarxa"`, // unselected: adds itself
		`class="chip" href="/"`,                                 // selected: clears itself
		`<td class="repo"><a href="/">xarxa</a></td>`,           // the row toggles it off again
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in rendered page", want)
		}
	}
	if strings.Contains(html, "embassy-rs/") {
		t.Error("owner prefix leaked into the page")
	}
}

// A color we don't have, or one that isn't a color, mustn't reach the page.
func TestHexColor(t *testing.T) {
	tests := map[string]string{
		"d73a4a":         "d73a4a",
		"D73A4A":         "d73a4a",
		"":               defaultColor,
		"#d73a4a":        defaultColor,
		"red; evil: yes": defaultColor,
	}

	for in, want := range tests {
		got := hexColor(in)
		if got != want {
			t.Errorf("hexColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewFilterDedupes(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?label=bug&label=&label=bug&label=ci&repo=xarxa&repo=xarxa", nil)
	f := newFilter(r)

	if want := []string{"bug", "ci"}; !slices.Equal(f.labels, want) {
		t.Errorf("labels = %v, want %v", f.labels, want)
	}
	if want := []string{"xarxa"}; !slices.Equal(f.repos, want) {
		t.Errorf("repos = %v, want %v", f.repos, want)
	}
	if !f.on() {
		t.Error("want the filter reported as on")
	}
	if newFilter(httptest.NewRequest(http.MethodGet, "/", nil)).on() {
		t.Error("want a bare / reported as unfiltered")
	}
}
