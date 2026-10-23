package budget

import (
	"fmt"

	"github.com/monoes/mono-agent/internal/aitask"
)

// CodeNoCapToLower is the refusal for a conversation that no cap applies to:
// a chat can't create spend authority from nothing.
const CodeNoCapToLower = "no_cap_to_lower"

// LowerConversationCap creates or lowers the conversation-scope cap of a chat
// to usd, and only when usd is strictly below the cap that applies to the
// conversation now (Resolve: the lowest of the global, conversation and
// operator caps; the kind and role caps are not known here). With no cap
// applying it refuses no_cap_to_lower, and for usd at or above the effective
// cap cap_raise_refused, so it can never be used to raise or to set a cap.
func (l *Ledger) LowerConversationCap(conversationID string, usd float64, by string) error {
	if err := validScope(ScopeConversation, conversationID); err != nil {
		return err
	}
	if err := validUSD(usd); err != nil {
		return err
	}
	eff, err := l.Resolve("", conversationID, "")
	if err != nil {
		return err
	}
	if !eff.Set {
		return &aitask.Error{Code: CodeNoCapToLower, Message: "no cap applies to this chat yet, so there is nothing to lower; set a global cap in Settings > AI task budgets"}
	}
	if usd > eff.USD-epsilon {
		return &aitask.Error{Code: CodeCapRaise,
			Message: fmt.Sprintf("a cap can only be lowered here: %v USD is not below the current %v USD, and only the operator raises a cap", usd, eff.USD)}
	}
	return l.SetCapMin(ScopeConversation, conversationID, usd, by)
}

// SetCapMin stores usd as the scope's cap, keeping the lower of it and a
// stored one.
func (l *Ledger) SetCapMin(scope, key string, usd float64, by string) error {
	_, err := l.DB.Exec(`INSERT INTO ai_budgets (scope, key, cap_usd, updated_by, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(scope, key) DO UPDATE SET cap_usd = MIN(cap_usd, excluded.cap_usd), updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		scope, key, usd, by, l.now())
	return err
}
