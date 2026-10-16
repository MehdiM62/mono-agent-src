package tasks

import "testing"

// A task names who added it, as its created event does, wherever it is read:
// alone, in a list, in the board's columns.
func TestTasksNameTheirAuthor(t *testing.T) {
	s, _, _ := newTestStore(t)
	want := map[int64]string{}
	for _, a := range []Actor{human, bot("workflow:exec-7"), {Kind: Agent}, {Kind: Capture, Name: SourceChrome}} {
		in := AddInput{Title: "from " + a.Label()}
		if a.Kind == Capture {
			in.SourceKind = SourceChrome
		}
		task, _, err := s.Add(bg, "default", in, a)
		if err != nil {
			t.Fatal(err)
		}
		if task.Author != a.Label() {
			t.Errorf("added by %s: author %q", a.Label(), task.Author)
		}
		want[task.ID] = a.Label()
	}
	// A later event changes the last one, not the author.
	if _, err := s.Comment(bg, "default", 1, "later", human); err != nil {
		t.Fatal(err)
	}
	ts, err := s.List(bg, "default", Filter{Statuses: []Status{StatusInbox}}, human)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range ts {
		if task.Author != want[task.ID] {
			t.Errorf("#%d in a list: author %q, want %q", task.ID, task.Author, want[task.ID])
		}
	}
	if task, _, err := s.Get(bg, "default", 1); err != nil || task.Author != "you" || task.LastEvent.Kind != "comment" {
		t.Errorf("#1 alone: %+v %v", task, err)
	}
	b, err := s.Board(bg, "default", 0, human)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range b.Tasks[StatusInbox] {
		if task.Author != want[task.ID] {
			t.Errorf("#%d on the board: author %q, want %q", task.ID, task.Author, want[task.ID])
		}
	}
	if len(b.Tasks[StatusInbox]) != len(want) {
		t.Errorf("the board's Inbox: %d tasks", len(b.Tasks[StatusInbox]))
	}
}
