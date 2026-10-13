import { describe, it, expect } from 'vitest'
import { templateBlurb } from './templateText.js'

describe('templateBlurb', () => {
  it('prefers the summary', () => {
    expect(templateBlurb({ summary: 'Make one image from a prompt.', description: 'Run with: x' })).toBe('Make one image from a prompt.')
  })

  it('drops the CLI sentences from a description', () => {
    const description = "Manually trigger with a prompt; generates a single image via Gemini and saves it to the image vault automatically. Run with: workflow run <id> --input '{\"prompt\":\"a red bicycle\"}'. Requires `monoagentcli login gemini` once beforehand."
    expect(templateBlurb({ description })).toBe('Manually trigger with a prompt; generates a single image via Gemini and saves it to the image vault automatically.')
  })

  it('keeps a description with nothing CLI-specific as it is', () => {
    expect(templateBlurb({ description: 'Every hour: fetch new Outlook emails. Set the credential first.' }))
      .toBe('Every hour: fetch new Outlook emails. Set the credential first.')
    expect(templateBlurb({})).toBe('')
  })
})

describe('templateBlurb sentence ends', () => {
  it('does not split inside "e.g." or a file name', () => {
    const description = "Saves every match. Run with: workflow run <id> --input '{\"keywords\":\"go\"}'. One source failing (e.g. a site tightening robots.txt) doesn't block the others."
    expect(templateBlurb({ description })).toBe("Saves every match. One source failing (e.g. a site tightening robots.txt) doesn't block the others.")
  })
})
