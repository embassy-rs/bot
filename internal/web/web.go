// Package web serves the review queue as a plain server-rendered page.
package web

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"embassy.dev/bot/internal/bot"
	"embassy.dev/bot/internal/models"
	"embassy.dev/bot/toolkit/log"
	"github.com/sqlbunny/errors"
)

//go:embed queue.html
var queueHTML string

var queueTemplate = template.Must(template.New("queue").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
}).Parse(queueHTML))

// label is one label chip: BG is GitHub's color for the label, FG a text color
// that reads on it, and URL the queue with this label added to, or removed
// from, the filter -- so both the filter bar and the labels on a row are
// toggles.
type label struct {
	Name string
	On   bool
	URL  string
	BG   template.CSS
	FG   template.CSS
}

// repoChip is one repo in the filter bar, and the same toggle on a queue row.
// Repos have no color of their own the way labels do, so they're drawn in the
// page's palette rather than GitHub's.
type repoChip struct {
	Name string
	On   bool
	URL  string
}

type queuePR struct {
	Repo    string
	RepoURL string
	Number  int64
	Title   string
	Author  string
	URL     string
	Age     string
	Since   string
	Labels  []label
}

type queueData struct {
	Queue    []queuePR
	Repos    []repoChip
	Labels   []label
	Filtered bool
	Now      string
}

func QueueHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	f := newFilter(r)

	prs, err := bot.Queue(ctx, f.repos, f.labels)
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Both bars offer what's on the *unfiltered* queue. See queryStrings.
	allRepos, err := bot.QueueRepos(ctx)
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	allLabels, err := bot.QueueLabels(ctx)
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	labels, err := bot.LabelColors(ctx)
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	colors := newColors(labels)

	now := time.Now()
	data := queueData{
		Filtered: f.on(),
		Now:      now.UTC().Format("2006-01-02 15:04:05 MST"),
	}
	for _, name := range allRepos {
		data.Repos = append(data.Repos, newRepoChip(name, f))
	}
	for _, name := range allLabels {
		// The bar spans repos, so there's no one repo to take the color from.
		data.Labels = append(data.Labels, newLabel(name, colors.anyColor(name), f))
	}
	for _, pr := range prs {
		// Bare name, no owner: everything the bot watches is under the one org,
		// so the prefix would be the same on every row and tell nobody anything.
		name := pr.R.Repo().Name
		row := queuePR{
			Repo:    name,
			RepoURL: f.toggleRepo(name),
			Number:  pr.Number,
			Title:   pr.Title,
			Author:  pr.Author,
			URL:     pr.HTMLURL,
			Age:     humanizeAge(now.Sub(pr.FirstReviewableAt.Time)),
			Since:   pr.FirstReviewableAt.Time.UTC().Format(time.RFC3339),
		}
		for _, name := range pr.Labels {
			row.Labels = append(row.Labels, newLabel(name, colors.color(pr.RepoID, name), f))
		}
		data.Queue = append(data.Queue, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = queueTemplate.Execute(w, data)
	if err != nil {
		// Too late for a status code, the template writes as it goes.
		log.Error(ctx, errors.StackTrace(err))
	}
}

// filter is what the page is narrowed to, read out of
// `?repo=a&repo=b&label=c`. The two dimensions are independent, so flipping one
// chip has to leave the other dimension exactly as it was.
type filter struct {
	repos  []string
	labels []string
}

func newFilter(r *http.Request) filter {
	return filter{
		repos:  queryValues(r, "repo"),
		labels: queryValues(r, "label"),
	}
}

// queryValues reads a repeated query parameter, dropping blanks and repeats so
// they can't show up twice in the toggle links built from it.
func queryValues(r *http.Request, key string) []string {
	var values []string
	for _, v := range r.URL.Query()[key] {
		if v == "" || slices.Contains(values, v) {
			continue
		}
		values = append(values, v)
	}
	return values
}

func (f filter) on() bool {
	return len(f.repos) != 0 || len(f.labels) != 0
}

// toggleRepo and toggleLabel are the queue with name flipped in or out of that
// one dimension, the other left alone.
func (f filter) toggleRepo(name string) string {
	return filterURL(toggle(f.repos, name), f.labels)
}

func (f filter) toggleLabel(name string) string {
	return filterURL(f.repos, toggle(f.labels, name))
}

// toggle flips name in or out of a selection.
//
// Sorted, so the same set of chips is always the same URL however you got
// there: the values are ORed, so click order carries no meaning and letting it
// into the link would just mint a second URL for a filter that already has one.
func toggle(selected []string, name string) []string {
	var out []string
	for _, s := range selected {
		if s != name {
			out = append(out, s)
		}
	}
	if !slices.Contains(selected, name) {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// filterURL is the queue page under a given selection.
func filterURL(repos, labels []string) string {
	q := url.Values{}
	for _, r := range repos {
		q.Add("repo", r)
	}
	for _, l := range labels {
		q.Add("label", l)
	}
	if len(q) == 0 {
		return "/"
	}
	return "/?" + q.Encode()
}

func newRepoChip(name string, f filter) repoChip {
	return repoChip{
		Name: name,
		On:   slices.Contains(f.repos, name),
		URL:  f.toggleRepo(name),
	}
}

func newLabel(name, color string, f filter) label {
	color = hexColor(color)
	return label{
		Name: name,
		On:   slices.Contains(f.labels, name),
		URL:  f.toggleLabel(name),
		BG:   template.CSS(color),
		FG:   template.CSS(textColor(color)),
	}
}

// colors answers "what color does GitHub paint this label". Colors belong to a
// repo, not to a name, so the same name can be two colors on two repos; where
// there's no repo to ask about, anyColor picks one and sticks to it.
type colors struct {
	byRepo map[repoLabel]string
	byName map[string]string
}

type repoLabel struct {
	repoID int64
	name   string
}

func newColors(labels models.LabelSlice) *colors {
	c := &colors{
		byRepo: map[repoLabel]string{},
		byName: map[string]string{},
	}
	for _, l := range labels {
		c.byRepo[repoLabel{l.RepoID, l.Name}] = l.Color
		if _, ok := c.byName[l.Name]; !ok {
			c.byName[l.Name] = l.Color
		}
	}
	return c
}

func (c *colors) color(repoID int64, name string) string {
	color, ok := c.byRepo[repoLabel{repoID, name}]
	if !ok {
		return c.byName[name]
	}
	return color
}

func (c *colors) anyColor(name string) string {
	return c.byName[name]
}

// GitHub's own default label gray, used for a label we've somehow never seen
// the color of.
const defaultColor = "ededed"

// hexColor sanitizes a stored color into the six bare hex digits GitHub hands
// out, so it can go into a style attribute as-is.
func hexColor(s string) string {
	s = strings.ToLower(s)
	if len(s) != 6 || strings.TrimLeft(s, "0123456789abcdef") != "" {
		return defaultColor
	}
	return s
}

// textColor picks black or white text for a label color, the way GitHub does:
// by perceived brightness, so pale labels get dark text and vivid ones white.
func textColor(hex string) string {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return "ffffff"
	}
	r, g, b := (v>>16)&0xff, (v>>8)&0xff, v&0xff
	if (r*299+g*587+b*114)/1000 > 140 {
		return "24292f"
	}
	return "ffffff"
}

func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
