package jevpick

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// File chooser capture: a click on an "Upload" button ends in the browser's
// native file dialog, which nothing can answer from here. ArmFileChooser makes
// the page keep the file input such a click was about instead (the click is
// cancelled, also for an input made and clicked in script), WaitFileChooser
// says when that happened, and SetChooserFiles puts files into that input as if
// they were picked. The page's input and change listeners see them.

// Chooser describes the file input a click would have opened the dialog for.
type Chooser struct {
	Multiple bool   `json:"multiple"`
	Accept   string `json:"accept"`
}

// ChooserFile is one file to give the page; Data is base64.
type ChooserFile struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Data string `json:"data"`
}

// armChooserJS is idempotent per document; a navigation drops it, so it is
// run again before every action.
const armChooserJS = `(() => {
  if (window.__jevChooser) return true;
  const st = window.__jevChooser = { input: null };
  document.addEventListener('click', e => {
    const t = e.target;
    if (t instanceof HTMLInputElement && t.type === 'file') { e.preventDefault(); st.input = t; }
  }, true);
  const click = HTMLInputElement.prototype.click;
  HTMLInputElement.prototype.click = function () {
    if (this.type === 'file') { st.input = this; return; }
    return click.apply(this, arguments);
  };
  return true;
})()`

const takeChooserJS = `(() => {
  const i = window.__jevChooser && window.__jevChooser.input;
  return i ? { multiple: !!i.multiple, accept: i.accept || '' } : null;
})()`

const setChooserFilesJS = `files => {
  const st = window.__jevChooser, el = st && st.input;
  if (!el) return false;
  const dt = new DataTransfer();
  for (const f of files) {
    const bin = atob(f.data), bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    dt.items.add(new File([bytes], f.name, { type: f.mime }));
  }
  el.files = dt.files;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
  st.input = null;
  return true;
}`

// ArmFileChooser makes the current document keep, not open, a file chooser.
func (b *Browser) ArmFileChooser() error { return b.Evaluate(armChooserJS, false, nil) }

// WaitFileChooser waits up to d for a click to have asked for a file chooser;
// nil when none did. A page may ask a moment after the click (a promise).
func (b *Browser) WaitFileChooser(ctx context.Context, d time.Duration) (*Chooser, error) {
	deadline := time.Now().Add(d)
	for {
		var c *Chooser
		err := b.Evaluate(takeChooserJS, false, &c)
		if err != nil || c != nil {
			return c, err
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(60 * time.Millisecond):
		}
	}
}

// SetChooserFiles gives the captured input files.
func (b *Browser) SetChooserFiles(files []ChooserFile) error {
	spec, err := json.Marshal(files)
	if err != nil {
		return err
	}
	var ok bool
	if err := b.Evaluate("("+setChooserFilesJS+")("+string(spec)+")", false, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the page no longer has the file chooser it asked for: %w", ErrStale)
	}
	return nil
}
