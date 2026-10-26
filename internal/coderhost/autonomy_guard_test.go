package coderhost

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A static guard: the autonomy, ledger and parked-question sources decide who
// answers and write the answers down. They may queue a note (Enqueue) and move
// questions, decisions and the session's state; they may not name any function
// that confirms a mission, approves a plan or a task session, resolves an HIL
// item, or changes settings, access or budgets. A future change that needs one
// of them has to change this list on purpose.

// approvalWriters are the identifiers that write an approval or widen what a
// session may do. Matching is by name, on every identifier, so a method call, a
// field, a function value and a composite-literal key are all caught.
var approvalWriters = []string{
	// mission confirmation (this package)
	"Confirm", "ConsumeConfirm", "PeekConfirm", "Revoke", "MissionConfirm", "confirmKey",
	"StashMissionToken", "TakeMissionToken", "WantsMission", "wantsMissionMark", "confirmPrefix", "sweepConfirms", "dropConfirm",
	// plan and task-session approval, human-in-the-loop send gates (other packages)
	"Approve", "ApproveSession", "ApprovedBy", "DecideSubmitItem", "DecideSiteItem", "HoldUpload", "HoldFill", "Hold",
	"Confirmed", "MissionConfig", "MissionAccess",
	// operator-only settings, access and budget raises, tree shape
	"SaveSettings", "SetUnattended", "SetBudget", "carveBudget", "SetRole", "Spawn", "SetChild", "OperatorOnly", "capAccess", "childAccess",
	"SetCommand", // ending or re-marking a command is the host loop's
}

var autonomyFiles = []string{"autonomy*.go", "ledger*.go", "parked*.go"}

// tablesAllowed are the only tables these files may name in SQL.
var tablesAllowed = map[string]bool{"coder_session_questions": true, "coder_session_decisions": true, "coder_session_commands": true, "coder_sessions": true}

var (
	sqlTable   = regexp.MustCompile(`(?i)\b(?:FROM|INTO|UPDATE|JOIN)\s+([a-z_][a-z_0-9]*)`)
	sqlCmdWrit = regexp.MustCompile(`(?i)(?:UPDATE|INSERT\s+INTO|DELETE\s+FROM)\s+coder_session_commands`)
	sqlSession = regexp.MustCompile(`(?i)UPDATE\s+coder_sessions\s+SET\s+([a-z_]+)`)
)

func nonTestFiles(t *testing.T, patterns ...string) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, p := range patterns {
		m, err := filepath.Glob(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range m {
			if !strings.HasSuffix(f, "_test.go") && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

func parse(t *testing.T, file string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAutonomySourcesNameNoApprovalWriter(t *testing.T) {
	files := nonTestFiles(t, autonomyFiles...)
	if len(files) < 4 {
		t.Fatalf("the guard sees only %v: it would pass on nothing", files)
	}
	forbidden := map[string]bool{}
	for _, n := range approvalWriters {
		forbidden[n] = true
	}
	for _, file := range files {
		f := parse(t, file)
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(p, "mono-agent/internal/") {
				t.Errorf("%s imports %s: these files decide who answers, they use nothing outside the package", file, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if forbidden[x.Name] {
					t.Errorf("%s: names %s, an approval/settings writer", file, x.Name)
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				s, _ := strconv.Unquote(x.Value)
				for _, m := range sqlTable.FindAllStringSubmatch(s, -1) {
					// prose such as "from the session" is not SQL: only a SQL-looking literal is read
					if looksLikeSQL(s) && !tablesAllowed[strings.ToLower(m[1])] {
						t.Errorf("%s: SQL touches %s", file, m[1])
					}
				}
				if sqlCmdWrit.MatchString(s) {
					t.Errorf("%s: writes coder_session_commands directly; queue through Enqueue", file)
				}
				for _, m := range sqlSession.FindAllStringSubmatch(s, -1) {
					if m[1] != "state" {
						t.Errorf("%s: updates coder_sessions.%s; only state is the autonomy paths' to move", file, m[1])
					}
				}
			}
			return true
		})
	}
}

func looksLikeSQL(s string) bool {
	// the SQL in this package is written in capitals; ordinary prose is not
	return strings.Contains(s, "SELECT ") || strings.Contains(s, "UPDATE ") || strings.Contains(s, "INSERT ") || strings.Contains(s, "DELETE ")
}

// Every place in the package that queues a command or settles a question is
// listed. A new one fails here until it is reviewed against the no-approval
// rule and added, with the test above or its own, covering it.
func TestEveryCommandQueueingSiteIsKnown(t *testing.T) {
	want := map[string]bool{
		"checkin.go:checkinOne":            true, // Enqueue: a host check-in (checkin- turn id)
		"children_control.go:SendToChild":  true, // Enqueue: a master's send (master- turn id, issuer)
		"children_control.go:ControlChild": true, // Enqueue: pause/resume/stop by a master
		"children.go:Spawn":                true, // Enqueue: a master's first message to a new child
		"parked_deliver.go:deliver":        true, // Enqueue: an answer note (answer- turn id)
	}
	// Only the parked-question code may deliver an answer, apply a default or write the ledger.
	ownedByParked := map[string]bool{"deliver": true, "applyDefault": true, "Record": true, "redeliver": true}
	got := map[string]bool{}
	for _, file := range nonTestFiles(t, "*.go") {
		f := parse(t, file)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == "Enqueue" {
							got[file+":"+fd.Name.Name] = true
						}
						if ownedByParked[sel.Sel.Name] && !strings.HasPrefix(file, "parked") && !strings.HasPrefix(file, "ledger") {
							t.Errorf("%s:%s calls %s, which only the parked-question code may", file, fd.Name.Name, sel.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("new Enqueue call site %s: review it against the no-approval rule and list it", k)
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf("listed call site %s is gone: update the list", k)
		}
	}
}
