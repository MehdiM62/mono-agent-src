package hilsend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/publication"
)

var (
	// ErrPublicationNeedsApproval refuses a publication registered by
	// whatever is not the operator's own terminal (an agent, the chat, an
	// AI-started run) that names no send the operator approved and that was
	// sent.
	ErrPublicationNeedsApproval = errors.New("registering a publication from an agent needs an approved send: prepare the send (it is queued for approval, and registers itself once approved), or name the id of a send that was approved and sent")
	// ErrPublicationApprovalUsed refuses an approval that already registered
	// a publication: one approval registers one publication.
	ErrPublicationApprovalUsed = errors.New("that approved send already registered a publication: one approval registers one publication")
)

// PublicationMismatchError refuses a publication that is not the one the
// approved send made: What (platform, remote id) differs.
type PublicationMismatchError struct{ What, Send, Publication string }

func (e *PublicationMismatchError) Error() string {
	return fmt.Sprintf("the approved send's %s is %q, not the publication's %q: an approval registers only the publication its send made", e.What, e.Send, e.Publication)
}

// PublicationClaim is an approval a registration claimed; nil when none was
// needed.
type PublicationClaim struct{ id, profile, before, after string }

// Release gives the claim back, when the registration then failed.
func (c *PublicationClaim) Release(ctx context.Context, db *sql.DB) {
	if c == nil {
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE hil_pending SET node_config = ? WHERE id = ? AND profile_id = ? AND node_config = ?`,
		c.before, c.id, c.profile, c.after)
}

// publicationIdentity is what makes two registrations of e the same
// publication, as publication.Store.Register tells a duplicate: its
// idempotency key, else its remote id on its platform, account and kind,
// else its URL there; "" when nothing does (each registration is new).
func publicationIdentity(e publication.Entry) string {
	platform, kind := strings.ToLower(strings.TrimSpace(e.Platform)), strings.ToLower(strings.TrimSpace(e.Kind))
	switch {
	case e.IdempotencyKey != "":
		return "key:" + e.IdempotencyKey
	case e.RemoteID != "":
		return "remote:" + strings.Join([]string{platform, e.Account, kind, e.RemoteID}, "\x00")
	case e.URL != "":
		return "url:" + strings.Join([]string{platform, e.Account, kind, e.URL}, "\x00")
	}
	return ""
}

// ClaimPublicationApproval checks a publication e that whatever is not the
// operator's own terminal registers, and claims the approval for it. With
// approvals of sends on (Enabled), approvalID must name a send of profile
// that was approved and sent, made on e's platform, whose returned remote id
// (when the send's result has one) is e's, and that registered no other
// publication: not by its own registration (ConsumeForSend), not by an
// earlier claim. The same publication again (a retry: publicationIdentity
// matches) passes with a nil claim, since its registration is idempotent.
// The claim is nil too when approvals of sends are off. The operator's own
// registrations need no check; the caller decides who that is.
func ClaimPublicationApproval(ctx context.Context, db *sql.DB, profile, approvalID string, e publication.Entry) (*PublicationClaim, error) {
	var none *PublicationClaim
	if !Enabled(db) {
		return none, nil
	}
	if strings.TrimSpace(approvalID) == "" {
		return none, ErrPublicationNeedsApproval
	}
	r, err := (Store{DB: db, Profile: profile}).find(ctx, approvalID)
	if err != nil {
		return none, ErrPublicationNeedsApproval
	}
	p, rp, err := r.payload()
	if err != nil || p.State != StateSent {
		return none, ErrPublicationNeedsApproval
	}
	identity := publicationIdentity(e)
	if rp.Publication != "" {
		if identity != "" && rp.Publication == identity {
			return none, nil
		}
		return none, ErrPublicationApprovalUsed
	}
	sent := strings.TrimSpace(p.Account.Platform)
	if sent == "" {
		sent = strings.TrimSpace(p.Channel)
	}
	if !strings.EqualFold(sent, strings.TrimSpace(e.Platform)) {
		return none, &PublicationMismatchError{"platform", sent, e.Platform}
	}
	if id, mine := sentRemoteID(p.Result), strings.TrimSpace(e.RemoteID); id != "" && id != mine {
		return none, &PublicationMismatchError{"remote id", id, mine}
	}
	rp.Publication = identity
	if identity == "" {
		rp.Publication = "claimed " + time.Now().UTC().Format(time.RFC3339)
	}
	claimed, err := json.Marshal(rp)
	if err != nil {
		return none, err
	}
	// The row as it was read: a registration that claimed it in between
	// changed it, and this one finds nothing to update.
	res, err := db.ExecContext(ctx, `UPDATE hil_pending SET node_config = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND profile_id = ? AND status = 'sent' AND node_config = ?`, string(claimed), r.id, profile, r.nc)
	if err != nil {
		return none, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return none, ErrPublicationApprovalUsed
	}
	return &PublicationClaim{id: r.id, profile: profile, before: r.nc, after: string(claimed)}, nil
}

// sentRemoteID is the remote id a sent send's node returned (`node run
// --output json`: {"main":[{"json":{...}}]}): the first of its main items'
// that names one.
func sentRemoteID(result any) string {
	out, _ := result.(map[string]any)
	items, _ := out["main"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		j, _ := m["json"].(map[string]any)
		for _, k := range []string{"remote_id", "post_id", "comment_id", "message_id", "id", "uri"} {
			switch v := j[k].(type) {
			case string:
				if v = strings.TrimSpace(v); v != "" {
					return v
				}
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
	}
	return ""
}

// ConsumeForSend spends an approved send's approval on the publication its
// own run recorded (the first of entries, as the store holds it): it
// registered that one, so a later registration with the approval passes only
// for that same publication.
// Only the run that performs the send (EnvApprovalID and EnvApprovalProof
// in its environment, the proof the approval handed it) spends it.
func ConsumeForSend(ctx context.Context, db *sql.DB, profile string, entries []publication.Entry) {
	id, proof := env(EnvApprovalID), env(EnvApprovalProof)
	if id == "" || proof == "" || profile == "" || len(entries) == 0 {
		return
	}
	r, err := (Store{DB: db, Profile: profile}).find(ctx, id)
	if err != nil {
		return
	}
	_, rp, err := r.payload()
	sum := sha256.Sum256([]byte(proof))
	if err != nil || rp.ProofHash == "" || rp.ProofHash != hex.EncodeToString(sum[:]) || rp.Publication != "" {
		return
	}
	if rp.Publication = publicationIdentity(entries[0]); rp.Publication == "" {
		rp.Publication = "sent " + time.Now().UTC().Format(time.RFC3339)
	}
	raw, err := json.Marshal(rp)
	if err != nil {
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE hil_pending SET node_config = ? WHERE id = ? AND profile_id = ? AND node_config = ?`, string(raw), r.id, profile, r.nc)
}
