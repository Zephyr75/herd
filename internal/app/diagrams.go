package app

// Diagrams: agents draw them with the archify skill, as standalone HTML pages
// in the run directory named after their artifact (PLAN.html,
// IMPLEMENT-2-flow.html…), and embed them in it as ![title](PLAN.html).
// herd serves them to its webview at /run/<task id>/<file>, sandboxed: an
// agent wrote them, so they get no network and no access to the app.

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"herd/internal/store"
)

// Diagram is one HTML page in a task's run directory.
type Diagram struct {
	File  string    `json:"file"`
	Title string    `json:"title"`
	Step  string    `json:"step"` // the step whose artifact it belongs to, e.g. Implement 2; "" when none
	Mod   time.Time `json:"mod"`
}

const diagramPrefix = "/run/"

var htmlTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// Diagrams lists the HTML pages in the task's run directory, in step order.
func (a *App) Diagrams(taskID string) ([]Diagram, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return nil, err
	}
	dir := a.runDir(a.projectOf(t), t)
	entries, _ := os.ReadDir(dir)
	steps, _ := a.db.Steps(t.ID)
	agents := a.settings.Agents()
	out := []Diagram{}
	order := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		d := Diagram{File: e.Name(), Title: strings.TrimSuffix(e.Name(), ".html")}
		if info, err := e.Info(); err == nil {
			d.Mod = info.ModTime()
		}
		if b := head(filepath.Join(dir, e.Name()), 16<<10); b != nil {
			if m := htmlTitle.FindSubmatch(b); m != nil && strings.TrimSpace(string(m[1])) != "" {
				d.Title = strings.TrimSpace(string(m[1]))
			}
		}
		order[d.File] = len(steps)
		for _, s := range steps {
			ag := agentFor(agents, s.Agent)
			stem := strings.TrimSuffix(ag.ArtifactName(s.N), filepath.Ext(ag.ArtifactName(s.N)))
			if stem != "" && (strings.HasPrefix(d.File, stem+".") || strings.HasPrefix(d.File, stem+"-")) {
				d.Step, order[d.File] = ag.DisplayName(s.N), s.Seq
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].File] != order[out[j].File] {
			return order[out[i].File] < order[out[j].File]
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

func head(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	k, _ := f.Read(b)
	return b[:k]
}

// withTheme gives a page herd's theme: archify's native diagrams read
// ?theme=, its explainer pages only data-theme on <html>.
func withTheme(b []byte, theme string) []byte {
	lower := bytes.ToLower(b)
	i := bytes.Index(lower, []byte("<html"))
	if i < 0 {
		return b
	}
	j := bytes.IndexByte(lower[i:], '>')
	if j < 0 || bytes.Contains(lower[i:i+j], []byte("data-theme")) {
		return b
	}
	at := i + len("<html")
	return slices.Concat(b[:at], []byte(` data-theme="`+theme+`"`), b[at:])
}

// embedFit lets an embedded page size its iframe to the diagram, so no empty
// band shows under it: the canvas is cut to the content with a small equal
// margin above and below, then its aspect ratio (an explainer page: its height) goes to
// herd by postMessage, which works across the sandbox.
const embedFit = `<style>html[data-embed="true"] .diagram-container{padding:0!important}</style>
<script>(function(){
function trim(s,v){var t=Infinity,b=-Infinity;for(var i=0;i<s.children.length;i++){var r;try{r=s.children[i].getBBox()}catch(e){continue}
if(!r.width&&!r.height||r.width>=v.width*.95&&r.height>=v.height*.9)continue;t=Math.min(t,r.y);b=Math.max(b,r.y+r.height)}
if(b>t){var m=Math.min(32,t-v.y);s.setAttribute('viewBox',[v.x,t-m,v.width,Math.min(v.height,b-t+2*m)].join(' '))}}
function post(){var s=document.querySelector('.diagram-container svg'),v=s&&s.viewBox&&s.viewBox.baseVal;
if(v&&v.width&&v.height)trim(s,v);
parent.postMessage({herdDiagram:v&&v.width&&v.height?{ratio:v.width/v.height}:{height:document.documentElement.scrollHeight}},'*')}
addEventListener('load',post)})()</script>`

// diagramCSP keeps a page self-contained and away from the app, even when
// opened outside its sandboxed iframe.
const diagramCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; media-src data: blob:; sandbox allow-scripts allow-downloads"

// RunFiles serves the diagrams to the webview, for Wails' asset server.
func (a *App) RunFiles() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, diagramPrefix)
		i := strings.LastIndex(rest, "/")
		if !ok || i < 0 || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		taskID, file := rest[:i], rest[i+1:]
		var t store.Task
		var err error
		if t, err = a.db.Task(taskID); err != nil || !strings.HasSuffix(file, ".html") || file != filepath.Base(file) || strings.HasPrefix(file, ".") {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join(a.runDir(a.projectOf(t), t), file))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if th := r.URL.Query().Get("theme"); th == "dark" || th == "light" {
			b = withTheme(b, th)
		}
		if r.URL.Query().Get("embed") == "1" {
			b = append(b, embedFit...)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", diagramCSP)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
}
