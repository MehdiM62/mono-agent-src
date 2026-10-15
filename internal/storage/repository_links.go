package storage

import (
	"database/sql"
	"fmt"
)

// ---------------------------------------------------------------------------
// Person links (cross-platform "same human?" links; never row merges)
// ---------------------------------------------------------------------------

// Person link relations and statuses (the person_links CHECK constraints).
const (
	PersonLinkSame    = "same"
	PersonLinkNotSame = "not_same"

	PersonLinkSuggested = "suggested"
	PersonLinkConfirmed = "confirmed"
	PersonLinkDismissed = "dismissed"
)

// ErrPersonLinkNotFound is returned when a link id does not exist in the profile.
var ErrPersonLinkNotFound = fmt.Errorf("person link not found")

// PersonLink is a row in person_links. PersonA < PersonB always.
type PersonLink struct {
	ID         string  `json:"id"`
	ProfileID  string  `json:"profile_id"`
	PersonA    string  `json:"person_a"`
	PersonB    string  `json:"person_b"`
	Relation   string  `json:"relation"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source,omitempty"`
	Model      string  `json:"model,omitempty"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

// Other returns the id of the link's person that is not personID.
func (l *PersonLink) Other(personID string) string {
	if l.PersonA == personID {
		return l.PersonB
	}
	return l.PersonA
}

func validPersonLinkStatus(s string) bool {
	return s == PersonLinkSuggested || s == PersonLinkConfirmed || s == PersonLinkDismissed
}

const personLinkColumns = `id, profile_id, person_a, person_b, relation, status,
	COALESCE(confidence, 0), source, model, created_at, updated_at`

func scanPersonLinks(rows *sql.Rows) ([]*PersonLink, error) {
	defer rows.Close()
	var out []*PersonLink
	for rows.Next() {
		l := &PersonLink{}
		if err := rows.Scan(&l.ID, &l.ProfileID, &l.PersonA, &l.PersonB, &l.Relation, &l.Status,
			&l.Confidence, &l.Source, &l.Model, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// InsertPersonLink records a link between two people, ordering the pair so
// PersonA < PersonB and generating an id when empty. An existing link for the
// pair (in either order, any status) is left untouched: inserted is false.
func (d *Database) InsertPersonLink(l *PersonLink) (inserted bool, err error) {
	if l.PersonA == "" || l.PersonB == "" || l.PersonA == l.PersonB {
		return false, fmt.Errorf("a person link needs two different people")
	}
	if l.Relation != PersonLinkSame && l.Relation != PersonLinkNotSame {
		return false, fmt.Errorf("invalid person link relation %q", l.Relation)
	}
	if !validPersonLinkStatus(l.Status) {
		return false, fmt.Errorf("invalid person link status %q", l.Status)
	}
	if err := requireProfile(l.ProfileID); err != nil {
		return false, err
	}
	if l.PersonA > l.PersonB {
		l.PersonA, l.PersonB = l.PersonB, l.PersonA
	}
	if l.ID == "" {
		l.ID = NewID()
	}
	res, err := d.DB.Exec(`INSERT INTO person_links
		(id, profile_id, person_a, person_b, relation, status, confidence, source, model)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(profile_id, person_a, person_b) DO NOTHING`,
		l.ID, l.ProfileID, l.PersonA, l.PersonB, l.Relation, l.Status, l.Confidence, l.Source, l.Model)
	if err != nil {
		return false, fmt.Errorf("inserting person link: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListPersonLinks returns the profile's links, newest first; an empty status
// returns every status.
func (d *Database) ListPersonLinks(profileID, status string) ([]*PersonLink, error) {
	if err := requireProfile(profileID); err != nil {
		return nil, err
	}
	q := `SELECT ` + personLinkColumns + ` FROM person_links WHERE profile_id = ?`
	args := []interface{}{profileID}
	if status != "" {
		if !validPersonLinkStatus(status) {
			return nil, fmt.Errorf("invalid person link status %q", status)
		}
		q += ` AND status = ?`
		args = append(args, status)
	}
	rows, err := d.DB.Query(q+` ORDER BY created_at DESC, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("listing person links: %w", err)
	}
	return scanPersonLinks(rows)
}

// SetPersonLinkStatus moves a link to status. Confirming sets the relation
// to "same", dismissing to "not_same". Unknown ids (or ids of another
// profile) return ErrPersonLinkNotFound.
func (d *Database) SetPersonLinkStatus(profileID, id, status string) error {
	if err := requireProfile(profileID); err != nil {
		return err
	}
	if !validPersonLinkStatus(status) {
		return fmt.Errorf("invalid person link status %q", status)
	}
	relation := ""
	switch status {
	case PersonLinkConfirmed:
		relation = PersonLinkSame
	case PersonLinkDismissed:
		relation = PersonLinkNotSame
	}
	res, err := d.DB.Exec(`UPDATE person_links SET status = ?,
		relation = CASE WHEN ? = '' THEN relation ELSE ? END,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND profile_id = ?`, status, relation, relation, id, profileID)
	if err != nil {
		return fmt.Errorf("updating person link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s", ErrPersonLinkNotFound, id)
	}
	return nil
}

// PersonLinksFor returns every link (any status) that involves personID.
func (d *Database) PersonLinksFor(profileID, personID string) ([]*PersonLink, error) {
	if err := requireProfile(profileID); err != nil {
		return nil, err
	}
	rows, err := d.DB.Query(`SELECT `+personLinkColumns+` FROM person_links
		WHERE profile_id = ? AND (person_a = ? OR person_b = ?)
		ORDER BY created_at DESC, id`, profileID, personID, personID)
	if err != nil {
		return nil, fmt.Errorf("listing person links: %w", err)
	}
	return scanPersonLinks(rows)
}
