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

type queuePR struct {
	Repo   string
	Number int64
	Title  string
	Author string
	URL    string
	Age    string
	Since  string
	Labels []label
}

type queueData struct {
	Queue    []queuePR
	Labels   []label
	Filtered bool
	Now      string
}

func QueueHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	selected := selectedLabels(r)

	prs, err := bot.Queue(ctx, selected)
	if err != nil {
		log.Error(ctx, errors.StackTrace(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// The filter bar offers every label on the *unfiltered* queue: the labels
	// are ORed, so listing only the ones on show would make every other label
	// unreachable the moment one is picked.
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
		Filtered: len(selected) != 0,
		Now:      now.UTC().Format("2006-01-02 15:04:05 MST"),
	}
	for _, name := range allLabels {
		// The bar spans repos, so there's no one repo to take the color from.
		data.Labels = append(data.Labels, newLabel(name, colors.anyColor(name), selected))
	}
	for _, pr := range prs {
		repo := pr.R.Repo()
		row := queuePR{
			Repo:   repo.Owner + "/" + repo.Name,
			Number: pr.Number,
			Title:  pr.Title,
			Author: pr.Author,
			URL:    pr.HTMLURL,
			Age:    humanizeAge(now.Sub(pr.FirstReviewableAt.Time)),
			Since:  pr.FirstReviewableAt.Time.UTC().Format(time.RFC3339),
		}
		for _, name := range pr.Labels {
			row.Labels = append(row.Labels, newLabel(name, colors.color(pr.RepoID, name), selected))
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

// selectedLabels reads the filter out of `?label=a&label=b`, dropping blanks
// and repeats so they can't show up twice in the toggle links built from it.
func selectedLabels(r *http.Request) []string {
	var labels []string
	for _, l := range r.URL.Query()["label"] {
		if l == "" || slices.Contains(labels, l) {
			continue
		}
		labels = append(labels, l)
	}
	return labels
}

func newLabel(name, color string, selected []string) label {
	color = hexColor(color)
	return label{
		Name: name,
		On:   slices.Contains(selected, name),
		URL:  toggleURL(selected, name),
		BG:   template.CSS(color),
		FG:   template.CSS(textColor(color)),
	}
}

// toggleURL is the queue with name flipped in or out of the filter.
func toggleURL(selected []string, name string) string {
	q := url.Values{}
	for _, l := range selected {
		if l != name {
			q.Add("label", l)
		}
	}
	if !slices.Contains(selected, name) {
		q.Add("label", name)
	}
	if len(q) == 0 {
		return "/"
	}
	return "/?" + q.Encode()
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
