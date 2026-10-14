package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalFileTriggerType starts a run for each change to a watched file or
// folder (n8n's Local File Trigger), while the daemon runs. The run's item
// is {"event": "add"|"change"|"unlink", "path": <absolute path>}.
const LocalFileTriggerType = "trigger.local_file"

// LocalFileTriggerSchema documents the config localFileConfig reads.
type LocalFileTriggerSchema struct {
	Path string `json:"path" schema:"label=Path,type=text,required,placeholder=/home/me/inbox,help=Absolute path of the file or folder to watch (~/ allowed). It need not exist yet."`

	Events string `json:"events" schema:"label=Events,type=text,placeholder=add,help=Comma-separated: add (a file appears)， change (its content or size changes)， unlink (it is removed). Empty: all three."`

	Recursive bool `json:"recursive" schema:"label=Include subfolders,type=boolean,default=false,help=Watch files in subfolders of a watched folder too."`

	Ignore string `json:"ignore" schema:"label=Ignore,type=text,placeholder=*.tmp,help=Comma-separated globs matched against each file's name and its path below the watched folder; a matching folder is skipped whole."`

	IntervalSeconds float64 `json:"interval_seconds" schema:"label=Check every (seconds),type=number,default=2,min=1,max=3600,help=How often the path is scanned for changes."`
}

// maxWatchedFiles bounds one watch; a path holding more fails activation,
// and a later scan over the bound is skipped rather than reporting files
// as removed.
const maxWatchedFiles = 50000

type localFileConfig struct {
	root      string
	events    map[string]bool
	recursive bool
	ignore    []string
	interval  time.Duration
}

// listConfig reads a comma-separated string or a list of strings.
func listConfig(v interface{}) []string {
	var parts []string
	switch t := v.(type) {
	case string:
		parts = strings.Split(t, ",")
	case []interface{}:
		for _, x := range t {
			if s, ok := x.(string); ok {
				parts = append(parts, s)
			}
		}
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseLocalFileConfig(cfg map[string]interface{}) (*localFileConfig, error) {
	root, _ := cfg["path"].(string)
	root = strings.TrimSpace(root)
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("local file trigger: %w", err)
		}
		root = filepath.Join(home, root[2:])
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("local file trigger: path must be absolute, got %q", root)
	}
	c := &localFileConfig{root: filepath.Clean(root), events: map[string]bool{}, interval: 2 * time.Second}
	events := listConfig(cfg["events"])
	if len(events) == 0 {
		events = []string{"add", "change", "unlink"}
	}
	for _, e := range events {
		if e != "add" && e != "change" && e != "unlink" {
			return nil, fmt.Errorf("local file trigger: unknown event %q (want add, change or unlink)", e)
		}
		c.events[e] = true
	}
	c.recursive, _ = cfg["recursive"].(bool)
	c.ignore = listConfig(cfg["ignore"])
	for _, g := range c.ignore {
		if _, err := filepath.Match(g, ""); err != nil {
			return nil, fmt.Errorf("local file trigger: bad ignore pattern %q: %w", g, err)
		}
	}
	if s, ok := cfg["interval_seconds"].(float64); ok && s >= 1 {
		c.interval = time.Duration(s * float64(time.Second))
	}
	return c, nil
}

type fileStamp struct {
	mod  time.Time
	size int64
}

// fileWatcher polls one path; fsnotify is not a dependency.
type fileWatcher struct {
	cfg  *localFileConfig
	fire func([]Item)
	prev map[string]fileStamp
}

var errTooManyFiles = errors.New("too many files")

func (w *fileWatcher) ignored(path string) bool {
	rel, _ := filepath.Rel(w.cfg.root, path)
	for _, g := range w.cfg.ignore {
		if ok, _ := filepath.Match(g, filepath.Base(path)); ok {
			return true
		}
		if ok, _ := filepath.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// scan stats the watched files. A missing path is an empty scan.
func (w *fileWatcher) scan() (map[string]fileStamp, error) {
	out := map[string]fileStamp{}
	info, err := os.Stat(w.cfg.root)
	if err != nil {
		return out, nil
	}
	if !info.IsDir() {
		out[w.cfg.root] = fileStamp{info.ModTime(), info.Size()}
		return out, nil
	}
	err = filepath.WalkDir(w.cfg.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == w.cfg.root {
			return nil
		}
		if w.ignored(p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !w.cfg.recursive {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if len(out) >= maxWatchedFiles {
			return errTooManyFiles
		}
		out[p] = fileStamp{fi.ModTime(), fi.Size()}
		return nil
	})
	return out, err
}

// poll scans once and fires one run per wanted change since the last scan.
func (w *fileWatcher) poll() {
	cur, err := w.scan()
	if err != nil {
		return
	}
	type change struct{ event, path string }
	var changes []change
	for p, s := range cur {
		old, seen := w.prev[p]
		switch {
		case !seen:
			changes = append(changes, change{"add", p})
		case !old.mod.Equal(s.mod) || old.size != s.size:
			changes = append(changes, change{"change", p})
		}
	}
	for p := range w.prev {
		if _, ok := cur[p]; !ok {
			changes = append(changes, change{"unlink", p})
		}
	}
	w.prev = cur
	sort.Slice(changes, func(i, j int) bool { return changes[i].path < changes[j].path })
	for _, c := range changes {
		if w.cfg.events[c.event] {
			w.fire([]Item{NewItem(map[string]interface{}{"event": c.event, "path": c.path})})
		}
	}
}

// localFileSource is the TriggerSource for LocalFileTriggerType. Files
// present at activation are the baseline and fire nothing.
type localFileSource struct{}

func (localFileSource) Activate(_ *Workflow, node *WorkflowNode, fire func([]Item)) (func(), error) {
	cfg, err := parseLocalFileConfig(node.Config)
	if err != nil {
		return nil, err
	}
	w := &fileWatcher{cfg: cfg, fire: fire}
	if w.prev, err = w.scan(); err != nil {
		return nil, fmt.Errorf("local file trigger: %s holds more than %d files", cfg.root, maxWatchedFiles)
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(cfg.interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				w.poll()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }, nil
}
