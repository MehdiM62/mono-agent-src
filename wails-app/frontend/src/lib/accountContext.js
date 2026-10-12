import { createContext } from 'react'

// The account gate (lib/accountGate.js) for the app below it, so a component can
// show the machine's monoes.me sign-in and ask the gate again. null outside the
// gate, so a component that wears it renders nothing there.
export const AccountContext = createContext(null)
