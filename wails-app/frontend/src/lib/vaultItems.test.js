import { describe, it, expect } from 'vitest'
import { isVaultImage, vaultFileKind } from './vaultItems.js'

describe('vaultItems', () => {
  it('tells images from the files the extension saves', () => {
    for (const f of ['img-001.png', 'img-002.JPG', 'img-003.jpeg', 'a.gif', 'a.webp', 'a.svg', 'a.bmp', 'a.ico']) {
      expect(isVaultImage({ filename: f })).toBe(true)
    }
    for (const f of ['img-004.pdf', 'img-005.md', 'img-006.txt', 'img-007.csv', 'noext', '']) {
      expect(isVaultImage({ filename: f })).toBe(false)
    }
    expect(isVaultImage(null)).toBe(false)
    expect(vaultFileKind({ filename: 'img-004.pdf' })).toBe('PDF')
    expect(vaultFileKind({ filename: 'noext' })).toBe('FILE')
  })
})
