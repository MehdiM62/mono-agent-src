// quoteClaim shows what an agent says about itself (a name, a role) as a
// quoted string a reader can't mistake for the card's own words: JSON
// quoting escapes quotes and newlines, the characters that change how text
// reads without being seen (Unicode format and control characters, line and
// paragraph separators and every space but the ASCII one: U+202E
// right-to-left override, U+200B zero-width space, U+0085, U+2028, U+3000)
// are shown as \uXXXX, or \u{XXXXX} above U+FFFF (a tag character such as
// U+E0041). So are variation selectors (U+FE0F), private-use characters
// (U+E000) and unassigned code points and noncharacters (U+10FFFF), which
// can carry text a reader doesn't see; other combining marks stay, since
// scripts such as Devanagari and Thai need them. A claim longer than max
// characters is cut with "…".
export function quoteClaim(s, max = 80) {
  let text = String(s ?? '')
  const chars = Array.from(text)
  if (chars.length > max) text = chars.slice(0, max).join('') + '…'
  return JSON.stringify(text).replace(/[\p{Cf}\p{Cc}\p{Co}\p{Cn}\p{Zl}\p{Zp}\p{Variation_Selector}]|(?! )\p{Zs}/gu,
    (c) => {
      const hex = c.codePointAt(0).toString(16).toUpperCase()
      return hex.length > 4 ? '\\u{' + hex + '}' : '\\u' + hex.padStart(4, '0')
    })
}
