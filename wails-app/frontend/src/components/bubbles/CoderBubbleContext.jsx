import { createContext, useContext } from 'react'

// CoderBubbleContext lets any page open a coder bubble without prop plumbing:
// App provides { openDraft, openNewChat, navigate } from the bubble store
// (#957). openNewChat(seed?) opens a blank chat (seed: { prompt, setup }).
// Without a provider (a page rendered alone) they are null and a Send-to-coder button
// still submits, it just cannot open the bubble.
export const CoderBubbleContext = createContext({ openDraft: null, openNewChat: null, navigate: null })

// useSendToCoder is { openDraft(seed), openNewChat(seed), navigate(page) } for the current app.
export function useSendToCoder() {
  return useContext(CoderBubbleContext)
}
