import { createContext, useContext } from 'react'

// CoderBubbleContext lets any page open a coder bubble without prop plumbing:
// App provides { openDraft, navigate } from the bubble store (#957). Without a
// provider (a page rendered alone) both are null and a Send-to-coder button
// still submits, it just cannot open the bubble.
export const CoderBubbleContext = createContext({ openDraft: null, navigate: null })

// useSendToCoder is { openDraft(seed), navigate(page) } for the current app.
export function useSendToCoder() {
  return useContext(CoderBubbleContext)
}
