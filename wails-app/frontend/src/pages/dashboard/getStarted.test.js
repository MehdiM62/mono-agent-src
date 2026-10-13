import { describe, it, expect } from 'vitest'
import { bridgeState, getStartedSteps, getStartedComplete, loadDismissed, okRunCount, saveDismissed } from './getStarted.js'

describe('bridgeState', () => {
  const portOpen = { status: 'connected', addr: '127.0.0.1:9222' }
  it('says not paired when the bridge turns this client away', () => {
    expect(bridgeState(portOpen, { running: true, bridge: { status: 'connected' }, clientPairing: 'mismatch' })).toBe('unpaired')
    expect(bridgeState(portOpen, { running: true, bridge: { status: 'unpaired' }, clientPairing: 'ok' })).toBe('unpaired')
  })
  it('says not running when no bridge answers', () => {
    expect(bridgeState(portOpen, { running: false })).toBe('off')
    expect(bridgeState(null, null)).toBe('off')
  })
  it('says connected only when paired and attached', () => {
    expect(bridgeState(portOpen, { running: true, bridge: { status: 'connected' }, clientPairing: 'ok' })).toBe('connected')
    expect(bridgeState(portOpen, { running: true, bridge: { status: 'waiting' }, clientPairing: 'ok' })).toBe('waiting')
  })
  it('falls back to the summary probe when extension status is unknown', () => {
    expect(bridgeState(portOpen, null)).toBe('connected')
  })
})

describe('getStartedSteps', () => {
  it('tracks each step and treats the daemon as optional', () => {
    const fresh = getStartedSteps({ signedIn: false, okRuns: 0, bridge: 'off', daemonRunning: false })
    expect(fresh.map(s => [s.id, s.done])).toEqual([['signIn', false], ['firstRun', false], ['extension', false], ['daemon', false]])
    expect(getStartedComplete(fresh)).toBe(false)
    const done = getStartedSteps({ signedIn: true, okRuns: 2, bridge: 'connected', daemonRunning: false })
    expect(getStartedComplete(done)).toBe(true)
  })
})

describe('dismissal', () => {
  it('is remembered and survives a throwing storage', () => {
    const store = new Map()
    const storage = { getItem: k => store.get(k) ?? null, setItem: (k, v) => store.set(k, v) }
    expect(loadDismissed(storage)).toBe(false)
    saveDismissed(storage)
    expect(loadDismissed(storage)).toBe(true)
    const broken = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } }
    expect(loadDismissed(broken)).toBe(false)
    expect(() => saveDismissed(broken)).not.toThrow()
  })
})

describe('first run step', () => {
  const step = (executions) => getStartedSteps({ signedIn: true, okRuns: okRunCount(executions), bridge: 'connected' }).find(s => s.id === 'firstRun')
  it('is not done when every run failed', () => {
    const failedOnly = [
      { id: 'e1', status: 'FAILED' }, { id: 'e2', status: 'FAILED' }, { id: 'e3', status: 'failed' },
      { id: 'e4', status: 'SUCCESS_WITH_ERRORS' }, { id: 'e5', status: 'CANCELLED' }, { id: 'e6', status: 'RUNNING' },
    ]
    expect(okRunCount(failedOnly)).toBe(0)
    expect(step(failedOnly).done).toBe(false)
    expect(step([]).done).toBe(false)
  })
  it('is done once a run succeeded', () => {
    expect(step([{ status: 'FAILED' }, { status: 'SUCCESS' }]).done).toBe(true)
    expect(step([{ status: 'COMPLETED' }]).done).toBe(true)
  })
})
