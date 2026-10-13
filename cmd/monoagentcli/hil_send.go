package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/spf13/cobra"
)

// Sending always waits for approval (plan coder-bubble-v2, point 3).
//
// An outbound node started from an agent context is queued as a HIL item of
// kind "send" (internal/hilsend). These commands are the operator's side of
// that queue; every one that decides or changes something refuses an agent.
//
//	hil sends [--state S]                     list send items (any state)
//	hil show <id>                             one item, the approval card's payload
//	hil edit <id> [--text T] [--subject S]    change the text; the item stays pending
//	hil resolve <id> --approve|--reject       decide; --approve runs the exact payload once
//	hil attachment <id> <n> [--stream]        a safe local file for attachment n
//	hil recipient <id>                        the recipient's people record
//	hil allow list|add|remove                 per-destination "always allow"

// hilSendRunner builds the Runner that performs an approved send. A variable
// so tests run a fake instead of starting a process.
var hilSendRunner = func(cfg *globalConfig) hilsend.Runner {
	return func(ctx context.Context, r hilsend.Run) (json.RawMessage, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		items := make([]map[string]interface{}, 0, len(r.Input))
		for _, in := range r.Input {
			items = append(items, map[string]interface{}{"json": in})
		}
		stdin, err := json.Marshal(map[string]interface{}{"config": r.Config, "input": items})
		if err != nil {
			return nil, err
		}
		args := []string{}
		if cfg.DBPath != "" {
			args = append(args, "--db-path", cfg.DBPath)
		}
		if cfg.ProfileID != "" {
			args = append(args, "--profile", cfg.ProfileID)
		}
		args = append(args, "node", "run", r.NodeType, "--stdin", "--output", "json")
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), hilsend.EnvApprovalID+"="+r.ApprovalID, hilsend.EnvApprovalProof+"="+r.Proof)
		cmd.Stdin = bytes.NewReader(stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 800 {
				msg = msg[len(msg)-800:]
			}
			if msg == "" {
				msg = err.Error()
			}
			return nil, errors.New(msg)
		}
		out := bytes.TrimSpace(stdout.Bytes())
		if !json.Valid(out) {
			return json.RawMessage(strconv.Quote(string(out))), nil
		}
		return json.RawMessage(out), nil
	}
}

func hilSendStore(cfg *globalConfig, db *sql.DB) hilsend.Store {
	return hilsend.Store{DB: db, Profile: cfg.ProfileID}
}

func hilSendError(err error) error {
	switch {
	case errors.Is(err, hilsend.ErrNotFound):
		return errNotFound("send item not found")
	case errors.Is(err, hilsend.ErrNotSend):
		return errInvalidInput("that Human-in-Loop item is not a send item")
	}
	return err
}

// requireOperator refuses an agent: it may prepare a send, never decide one.
func requireOperator(what string) error {
	if _, err := callerFor("").operator(what); err != nil {
		return err
	}
	if _, agent := hilsend.Detect(); agent {
		return operatorOnlyError("%s is set, so an agent is running this command: only the operator can %s; run it in your own terminal", hilsend.EnvAgentContext, what)
	}
	return nil
}

func printSend(cmd *cobra.Command, cfg *globalConfig, p hilsend.Payload) error {
	if cfg.JSONOutput {
		return writeJSONTo(cmd.OutOrStdout(), p)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  %s  %s via %s  to %s\n", p.ID, p.State, p.Action, p.Channel, p.Recipient.Name)
	if p.Subject != "" {
		fmt.Fprintf(w, "Subject: %s\n", p.Subject)
	}
	fmt.Fprintf(w, "%s\n", p.Text)
	if p.Error != "" {
		fmt.Fprintf(w, "Error: %s\n", p.Error)
	}
	return nil
}

func withSendDB(cfg *globalConfig, fn func(*sql.DB, hilsend.Store) error) error {
	db, err := initDB(cfg)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer db.Close()
	return fn(db.DB, hilSendStore(cfg, db.DB))
}

// resolveSend decides a send item; shared by `hil resolve` and the older
// `hil approve|reject`.
func resolveSend(cmd *cobra.Command, cfg *globalConfig, st hilsend.Store, id string, approve bool, edit hilsend.Edit, always bool) error {
	if err := requireOperator("approve or reject a send"); err != nil {
		return err
	}
	p, err := hilsend.Resolve(cmd.Context(), st, id, approve, edit, always, hilSendRunner(cfg))
	if err != nil {
		return hilSendError(err)
	}
	return printSend(cmd, cfg, p)
}

func newHILResolveCmd(cfg *globalConfig) *cobra.Command {
	var approve, reject, always bool
	var text, subject string
	cmd := &cobra.Command{
		Use:   "resolve <id>",
		Short: "Approve or reject a send an agent prepared (operator only)",
		Long: "Decide a pending send item. --approve performs the exact previewed payload once (with --edit-text / " +
			"--edit-subject applied first) through the original node and records the publication; --reject marks " +
			"it rejected and never sends. Deciding again is a no-op that reports the item's state. " +
			"--always-allow (with --approve) lets later sends to the same destination from an agent go out without a card.",
		Example: `  monoagentcli --json hil resolve 7c1e --approve
  monoagentcli --json hil resolve 7c1e --approve --edit-text "Hi Ada, shorter version"
  monoagentcli --json hil resolve 7c1e --reject`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if approve == reject {
				return errInvalidInput("give exactly one of --approve or --reject")
			}
			if reject && (cmd.Flags().Changed("edit-text") || cmd.Flags().Changed("edit-subject") || always) {
				return errInvalidInput("--edit-text, --edit-subject and --always-allow only go with --approve")
			}
			var edit hilsend.Edit
			if cmd.Flags().Changed("edit-text") {
				edit.Text = &text
			}
			if cmd.Flags().Changed("edit-subject") {
				edit.Subject = &subject
			}
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				return resolveSend(cmd, cfg, st, args[0], approve, edit, always)
			})
		},
	}
	cmd.Flags().BoolVar(&approve, "approve", false, "Send it")
	cmd.Flags().BoolVar(&reject, "reject", false, "Never send it")
	cmd.Flags().StringVar(&text, "edit-text", "", "Replace the text before sending")
	cmd.Flags().StringVar(&subject, "edit-subject", "", "Replace the subject before sending")
	cmd.Flags().BoolVar(&always, "always-allow", false, "Also allow this destination for good (approve only)")
	return cmd
}

func newHILSendsCmd(cfg *globalConfig) *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "sends",
		Short: "List send items (every state, oldest first)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				var states []hilsend.State
				if state != "" {
					states = append(states, hilsend.State(state))
				}
				items, err := st.List(cmd.Context(), states...)
				if err != nil {
					return err
				}
				if cfg.JSONOutput {
					return writeJSONTo(cmd.OutOrStdout(), items)
				}
				for _, p := range items {
					fmt.Fprintf(cmd.OutOrStdout(), "%s  %-8s %-8s %-10s %s\n", p.ID, p.State, p.Action, p.Channel, p.Recipient.Name)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "Only this state: pending|approved|rejected|sent|failed")
	return cmd
}

func newHILShowCmd(cfg *globalConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one send item (the approval card's payload)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				p, err := st.Get(cmd.Context(), args[0])
				if err != nil {
					return hilSendError(err)
				}
				return printSend(cmd, cfg, p)
			})
		},
	}
}

func newHILEditCmd(cfg *globalConfig) *cobra.Command {
	var text, subject string
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change a pending send's text or subject (operator only); it stays pending and re-previews",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireOperator("edit a send"); err != nil {
				return err
			}
			var t, s *string
			if cmd.Flags().Changed("text") {
				t = &text
			}
			if cmd.Flags().Changed("subject") {
				s = &subject
			}
			if t == nil && s == nil {
				return errInvalidInput("give --text and/or --subject")
			}
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				p, err := st.Edit(cmd.Context(), args[0], t, s)
				if err != nil {
					return hilSendError(err)
				}
				return printSend(cmd, cfg, p)
			})
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "New text")
	cmd.Flags().StringVar(&subject, "subject", "", "New subject")
	return cmd
}

// attachmentRoots are the only folders a preview may read from: the
// profile's folder (the coder chat folder is that folder), the downloads
// folder, and the coder workspace root.
func attachmentRoots(db *sql.DB, profile string) []string {
	roots := []string{profiledir.Root(db, profile)}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".monoagent", "downloads"))
	}
	if s, err := loadCoderSettings(db); err == nil && s.WorkspaceRoot != "" {
		roots = append(roots, s.WorkspaceRoot)
	}
	return roots
}

func newHILAttachmentCmd(cfg *globalConfig) *cobra.Command {
	var stream bool
	cmd := &cobra.Command{
		Use:   "attachment <id> <n>",
		Short: "Resolve attachment n (0-based) of a send item to a safe local file",
		Long: "Prints {path,name,mime,size,kind} for attachment n, but only when the file lies under the profile's " +
			"folder, the downloads folder or the coder workspace folder (links and symbolic links out of those are " +
			"refused). --stream writes the file's bytes to stdout instead, for previews that cannot read the disk.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return errInvalidInput("attachment index %q is not a number", args[1])
			}
			return withSendDB(cfg, func(db *sql.DB, st hilsend.Store) error {
				p, err := st.Get(cmd.Context(), args[0])
				if err != nil {
					return hilSendError(err)
				}
				f, err := hilsend.AttachmentFile(p, n, attachmentRoots(db, cfg.ProfileID))
				if err != nil {
					return errInvalidInput("%v", err)
				}
				if !stream {
					return writeJSONTo(cmd.OutOrStdout(), f)
				}
				fh, err := os.Open(f.Path)
				if err != nil {
					return err
				}
				defer fh.Close()
				_, err = io.Copy(cmd.OutOrStdout(), io.LimitReader(fh, hilsend.MaxPreviewBytes))
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&stream, "stream", false, "Write the file's bytes to stdout")
	return cmd
}

// hilRecipient is the recipient card's data.
type hilRecipient struct {
	hilsend.Recipient
	Found    bool   `json:"found"`
	Platform string `json:"platform,omitempty"`
	Username string `json:"username,omitempty"`
	FullName string `json:"fullName,omitempty"`
	Image    string `json:"imageUrl,omitempty"`
	Contact  string `json:"contactDetails,omitempty"`
	Website  string `json:"website,omitempty"`
	Intro    string `json:"introduction,omitempty"`
	Job      string `json:"jobTitle,omitempty"`
	Category string `json:"category,omitempty"`
}

func newHILRecipientCmd(cfg *globalConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "recipient <id>",
		Short: "Show the people record of a send item's recipient",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSendDB(cfg, func(db *sql.DB, st hilsend.Store) error {
				p, err := st.Get(cmd.Context(), args[0])
				if err != nil {
					return hilSendError(err)
				}
				out := hilRecipient{Recipient: p.Recipient}
				if p.Recipient.PersonID != "" {
					var img, contact, web, intro, job, cat, name sql.NullString
					err := db.QueryRowContext(cmd.Context(),
						`SELECT platform, platform_username, full_name, image_url, contact_details, website, introduction, job_title, category
						 FROM people WHERE id = ?`, p.Recipient.PersonID).
						Scan(&out.Platform, &out.Username, &name, &img, &contact, &web, &intro, &job, &cat)
					if err == nil {
						out.Found, out.FullName, out.Image, out.Contact = true, name.String, img.String, contact.String
						out.Website, out.Intro, out.Job, out.Category = web.String, intro.String, job.String, cat.String
					}
				}
				return writeJSONTo(cmd.OutOrStdout(), out)
			})
		},
	}
}

func newHILAllowCmd(cfg *globalConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allow",
		Short: "Destinations an agent may send to without approval (operator only)",
	}
	list := &cobra.Command{
		Use: "list", Short: "List the always-allow rules", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				rules, err := st.Allows(cmd.Context())
				if err != nil {
					return err
				}
				return writeJSONTo(cmd.OutOrStdout(), rules)
			})
		},
	}
	var a hilsend.Allow
	add := &cobra.Command{
		Use: "add", Short: "Always allow one destination", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireOperator("allow a destination"); err != nil {
				return err
			}
			if a.Action == "" || a.Recipient == "" {
				return errInvalidInput("--action and --recipient are required")
			}
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				if err := st.Allow(cmd.Context(), a); err != nil {
					return err
				}
				return writeJSONTo(cmd.OutOrStdout(), a)
			})
		},
	}
	add.Flags().StringVar(&a.Action, "action", "", "email|dm|comment|post|connect|publish")
	add.Flags().StringVar(&a.Platform, "platform", "", "Sending platform (the card's account platform)")
	add.Flags().StringVar(&a.Account, "account", "", "Sending account handle")
	add.Flags().StringVar(&a.Recipient, "recipient", "", "Profile URL, address or name exactly as the card shows it")
	remove := &cobra.Command{
		Use: "remove <rule-id>", Short: "Stop always allowing a destination", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireOperator("remove an always-allow rule"); err != nil {
				return err
			}
			return withSendDB(cfg, func(_ *sql.DB, st hilsend.Store) error {
				ok, err := st.Disallow(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if !ok {
					return errNotFound("no such always-allow rule")
				}
				return writeJSONTo(cmd.OutOrStdout(), map[string]any{"removed": args[0]})
			})
		},
	}
	cmd.AddCommand(list, add, remove)
	return cmd
}

// delegateSendResolve routes the older `hil approve|reject <id>` to the send
// path when id is a send item. handled is false for an ordinary HIL item.
func delegateSendResolve(cmd *cobra.Command, cfg *globalConfig, db *sql.DB, id string, approve bool, data string) (handled bool, err error) {
	st := hilSendStore(cfg, db)
	if _, gerr := st.Get(cmd.Context(), id); gerr != nil {
		return false, nil
	}
	var edit hilsend.Edit
	if approve && data != "" {
		var d struct {
			Text    *string `json:"text"`
			Subject *string `json:"subject"`
		}
		if jerr := json.Unmarshal([]byte(data), &d); jerr != nil {
			return true, errInvalidInput("--data is not valid JSON: %v", jerr)
		}
		edit = hilsend.Edit{Text: d.Text, Subject: d.Subject}
	}
	return true, resolveSend(cmd, cfg, st, id, approve, edit, false)
}

// approveIfSend is delegateSendResolve on a database it opens itself.
func approveIfSend(cmd *cobra.Command, cfg *globalConfig, id string, approve bool, data string) (bool, error) {
	db, err := initDB(cfg)
	if err != nil {
		return false, nil
	}
	defer db.Close()
	return delegateSendResolve(cmd, cfg, db.DB, id, approve, data)
}

// requirePublicationApproval gates `publication register` from an agent: a
// publication it claims must be one the operator approved. approvalID names the
// send item (as returned when it was queued); an approved send registers its
// own publication, so this is only for a publication made outside that path.
// Operators, and profiles that turned sendApproval off, are not asked.
func requirePublicationApproval(cfg *globalConfig, approvalID string) error {
	if _, agent := hilsend.Detect(); !agent {
		return nil
	}
	return withImageDB(cfg, func(db *sql.DB) error {
		if !hilsend.Enabled(db) {
			return nil
		}
		if approvalID != "" {
			if p, err := hilSendStore(cfg, db).Get(context.Background(), approvalID); err == nil && p.State == hilsend.StateSent {
				return nil
			}
		}
		return operatorOnlyError("registering a publication from an agent needs an approved send: prepare the send (it is queued for approval, and registers itself once approved), or pass --approval <hil id> of a send that was approved and sent")
	})
}
