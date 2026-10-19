package control

import (
	"context"
	"database/sql"
	"os"
	"regexp"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// tagChars is a tag's shape: an id's characters, at most 128 of them.
var tagChars = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// askTags is the org and the coder chat an agent's run of this node is
// for, as hilsend tags the agent's send items (Detect: MONOMIND_ORG_NAME,
// MONOAGENT_CONVERSATION_ID), so its items reach that bubble. Like a send
// item's, it is the agent's own word, and only says where the item shows;
// it is kept only with an id's shape (tagChars) and when it names an org
// or a chat the profile has, so an agent can't file an item under a bubble
// that isn't there or store a long value with every row. A run that is no
// agent's (a schedule, the user's own) is untagged and stays on the Human
// in Loop page, and so is an org-started execution's, whose items are the
// org's decisions (orgdecide) until it hands them on.
func askTags(ctx context.Context, db *sql.DB, profileID, executionID string) map[string]interface{} {
	who, _ := hilsend.Detect() // no agent's: no tags
	if orgStarted(ctx, db, executionID) {
		return nil
	}
	tags := map[string]interface{}{}
	if tagChars.MatchString(who.Org) && orgExists(db, profileID, who.Org) {
		tags["org"] = who.Org
	}
	if tagChars.MatchString(who.ConversationID) && chatExists(ctx, db, profileID, who.ConversationID) {
		tags["conversationId"] = who.ConversationID
	}
	return tags
}

// orgExists reports whether the profile has an org named org: its file
// under the profile's .monomind/orgs.
func orgExists(db *sql.DB, profileID, org string) bool {
	path, err := orgdesign.ConfigPath(profiledir.Root(db, profileID), org)
	if err != nil {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// chatExists reports whether the profile has the chat conversation id.
func chatExists(ctx context.Context, db *sql.DB, profileID, id string) bool {
	var one int
	return db.QueryRowContext(ctx, `SELECT 1 FROM ai_chat_conversations WHERE id = ? AND profile_id = ?`, id, profileID).Scan(&one) == nil
}
