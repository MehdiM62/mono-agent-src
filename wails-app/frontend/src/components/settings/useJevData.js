import { useState, useEffect, useCallback } from 'react'
import { JevStatus, JevUsage } from '../../wailsjs/go/main/App'
import { errMsg } from './jevShared.jsx'

// The Jev status and (when a profile half is shown) the 7-day usage, read once
// for however many JevSection parts share them. Settings calls this once and
// hands the result to both parts as `jev`; a JevSection without that prop calls
// it itself. `load` reads both again, after a save/test/remove/adopt.
export function useJevData({ withUsage }) {
  const [status, setStatus] = useState(null)
  const [loadErr, setLoadErr] = useState('')
  const [usage, setUsage] = useState(null)
  const [usageErr, setUsageErr] = useState('')

  const load = useCallback(async () => {
    const [st, us] = await Promise.allSettled([
      (async () => JevStatus())(),
      withUsage ? (async () => JevUsage('7d'))() : Promise.resolve(null),
    ])
    if (st.status === 'fulfilled') { setStatus(st.value); setLoadErr('') } else setLoadErr(errMsg(st.reason))
    if (!withUsage) return
    if (us.status === 'fulfilled') { setUsage(us.value); setUsageErr('') } else setUsageErr(errMsg(us.reason))
  }, [withUsage])

  useEffect(() => { load() }, [load])

  return { status, loadErr, usage, usageErr, load }
}
