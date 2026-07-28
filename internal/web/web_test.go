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
	data := queueData{
		Filtered: true,
		Labels: []label{
			newLabel("bug", "d73a4a", []string{"bug"}),
			newLabel("enhancement", "a2eeef", []string{"bug"}),
		},
		Queue: []queuePR{{
			Repo:   "embassy-rs/embassy",
			Title:  "Fix it",
			Labels: []label{newLabel("bug", "d73a4a", []string{"bug"})},
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

func TestToggleURL(t *testing.T) {
	tests := []struct {
		selected []string
		name     string
		want     string
	}{
		{nil, "bug", "/?label=bug"},
		{[]string{"bug"}, "bug", "/"},
		{[]string{"bug"}, "ci", "/?label=bug&label=ci"},
		{[]string{"bug", "ci"}, "bug", "/?label=ci"},
	}
	for _, test := range tests {
		got := toggleURL(test.selected, test.name)
		if got != test.want {
			t.Errorf("toggleURL(%v, %q) = %q, want %q", test.selected, test.name, got, test.want)
		}
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

func TestSelectedLabelsDedupes(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?label=bug&label=&label=bug&label=ci", nil)
	got := selectedLabels(r)
	want := []string{"bug", "ci"}
	if !slices.Equal(got, want) {
		t.Errorf("selectedLabels = %v, want %v", got, want)
	}
}
