package comm

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"
)

// imapReadOpts is what one comm.email_read run asks the server for.
type imapReadOpts struct {
	host       string
	port       int
	useTLS     bool
	username   string
	password   string
	mailbox    string
	limit      int
	unreadOnly bool
	// since is the previous run's cursor when since_last_run is on (a zero
	// cursor on the first run); nil when it is off.
	since *imapCursor
	// baselineOnly reads no messages: it only finds the mailbox's highest
	// UID, so a first since_last_run run with baseline on starts the cursor
	// there and returns nothing.
	baselineOnly bool
}

// imapReadResult is the messages read, newest first, plus the mailbox
// state a since_last_run cursor is built from.
type imapReadResult struct {
	messages    []map[string]interface{}
	uidValidity uint32
	// afterUID is the UID the read started after: the cursor's, or 0 when
	// there was none or the mailbox's UIDVALIDITY changed.
	afterUID uint32
	maxUID   uint32
}

// dialEmailIMAP opens the connection to the IMAP server; tests swap it for
// a fake server.
var dialEmailIMAP = func(ctx context.Context, host string, port int, useTLS bool) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	nd := &net.Dialer{Timeout: 15 * time.Second}
	if !useTLS {
		return nd.DialContext(ctx, "tcp", addr)
	}
	d := &tls.Dialer{NetDialer: nd, Config: &tls.Config{ServerName: host}}
	return d.DialContext(ctx, "tcp", addr)
}

// readIMAPMail reads messages over the raw IMAP text protocol by UID. Like
// fetchOutlookMail it only reads: EXAMINE opens the mailbox read-only and
// bodies are fetched with BODY.PEEK, so no message gets the \Seen flag.
//
// Without a usable cursor it returns the newest opts.limit messages. With
// one (opts.since set, same UIDVALIDITY) it returns the oldest opts.limit
// messages whose UID is above the cursor, so a backlog larger than the
// limit is read over the following runs rather than skipped.
func readIMAPMail(ctx context.Context, opts imapReadOpts) (*imapReadResult, error) {
	conn, err := dialEmailIMAP(ctx, opts.host, opts.port, opts.useTLS)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	tc := textproto.NewConn(conn)
	defer tc.Close()

	ctxDone := make(chan struct{})
	defer close(ctxDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-ctxDone:
		}
	}()

	readLine := func() (string, error) {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return tc.ReadLine()
	}
	// run sends one tagged command and returns its untagged lines.
	run := func(tag, cmd string) ([]string, error) {
		if _, err := fmt.Fprintf(conn, "%s %s\r\n", tag, cmd); err != nil {
			return nil, err
		}
		var untagged []string
		for {
			line, err := readLine()
			if err != nil {
				return nil, err
			}
			switch {
			case strings.HasPrefix(line, tag+" OK"):
				return untagged, nil
			case strings.HasPrefix(line, tag+" NO"), strings.HasPrefix(line, tag+" BAD"):
				return nil, fmt.Errorf("IMAP error: %s", line)
			}
			untagged = append(untagged, line)
		}
	}

	if _, err := readLine(); err != nil {
		return nil, fmt.Errorf("greeting: %w", err)
	}
	if _, err := run("A1", "LOGIN "+imapQuote(opts.username)+" "+imapQuote(opts.password)); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	lines, err := run("A2", "EXAMINE "+imapQuote(opts.mailbox))
	if err != nil {
		return nil, fmt.Errorf("EXAMINE %s: %w", opts.mailbox, err)
	}
	res := &imapReadResult{}
	for _, line := range lines {
		var v uint32
		if _, err := fmt.Sscanf(line, "* OK [UIDVALIDITY %d]", &v); err == nil {
			res.uidValidity = v
		}
	}
	if s := opts.since; s != nil && s.LastUID > 0 && s.UIDValidity == res.uidValidity {
		res.afterUID = s.LastUID
	}

	criteria := "ALL"
	if res.afterUID > 0 {
		criteria = fmt.Sprintf("UID %d:*", res.afterUID+1)
	}
	if opts.unreadOnly && !opts.baselineOnly {
		criteria += " UNSEEN"
	}
	lines, err = run("A3", "UID SEARCH "+criteria)
	if err != nil {
		return nil, fmt.Errorf("SEARCH: %w", err)
	}
	var uids []uint32
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "* SEARCH")
		if !ok {
			continue
		}
		for _, f := range strings.Fields(rest) {
			// "n:*" always matches the highest UID, even one at or below n.
			if u, err := strconv.ParseUint(f, 10, 32); err == nil && uint32(u) > res.afterUID {
				uids = append(uids, uint32(u))
			}
		}
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if opts.baselineOnly {
		if len(uids) > 0 {
			res.maxUID = uids[len(uids)-1]
		}
		uids = nil
	}
	if len(uids) > opts.limit {
		if res.afterUID > 0 {
			uids = uids[:opts.limit]
		} else {
			uids = uids[len(uids)-opts.limit:]
		}
	}

	if len(uids) > 0 {
		set := make([]string, len(uids))
		for i, u := range uids {
			set[i] = strconv.FormatUint(uint64(u), 10)
		}
		if _, err := fmt.Fprintf(conn, "A4 UID FETCH %s (UID FLAGS ENVELOPE BODY.PEEK[TEXT]<0.2048>)\r\n", strings.Join(set, ",")); err != nil {
			return nil, err
		}
		for {
			resp, err := readIMAPResponse(readLine, tc.R)
			if err != nil {
				return nil, fmt.Errorf("FETCH: %w", err)
			}
			if strings.HasPrefix(resp.text, "A4 OK") {
				break
			}
			if strings.HasPrefix(resp.text, "A4 NO") || strings.HasPrefix(resp.text, "A4 BAD") {
				return nil, fmt.Errorf("FETCH: %s", resp.text)
			}
			attrs, ok := parseFetchResponse(resp)
			if !ok {
				continue
			}
			uid, err := strconv.ParseUint(imapString(attrs["UID"]), 10, 32)
			if err != nil {
				continue
			}
			res.messages = append(res.messages, imapMessageItem(uint32(uid), attrs))
			if uint32(uid) > res.maxUID {
				res.maxUID = uint32(uid)
			}
		}
	}

	_, _ = fmt.Fprintf(conn, "A5 LOGOUT\r\n")

	sort.Slice(res.messages, func(i, j int) bool {
		return res.messages[i]["uid"].(int) > res.messages[j]["uid"].(int)
	})
	return res, nil
}

// imapQuote writes s as an IMAP quoted string. Only \ and " are escaped;
// other bytes go as they are (Go's %q would turn non-ASCII into \u escapes,
// which an IMAP server reads literally).
func imapQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// imapMessageItem maps one FETCH response onto the node's item fields.
func imapMessageItem(uid uint32, attrs map[string]interface{}) map[string]interface{} {
	msg := map[string]interface{}{"uid": int(uid), "read": false}
	flags, _ := attrs["FLAGS"].([]interface{})
	for _, f := range flags {
		if strings.EqualFold(imapString(f), `\Seen`) {
			msg["read"] = true
		}
	}
	if env, ok := attrs["ENVELOPE"].([]interface{}); ok {
		envelopeFields(env, msg)
	}
	for name, v := range attrs {
		if strings.HasPrefix(name, "BODY[TEXT]") {
			body := imapString(v)
			if len(body) > 2048 {
				body = body[:2048] + "…"
			}
			msg["body"] = strings.TrimSpace(body)
		}
	}
	return msg
}
