// quoteClaim shows what an agent says about itself (a name, a role) as a
// quoted string a reader can't mistake for the card's own words: JSON
// quoting escapes quotes and newlines, the characters that change how text
// reads without being seen (Unicode format and control characters, line and
// paragraph separators and every space but the ASCII one: U+202E
// right-to-left override, U+200B zero-width space, U+0085, U+2028, U+3000)
// are shown as \uXXXX, and a claim longer than max characters is cut with
// "…".
export function quoteClaim(s, max = 80) {
  let text = String(s ?? '')
  const chars = Array.from(text)
  if (chars.length > max) text = chars.slice(0, max).join('') + '…'
  return JSON.stringify(text).replace(/[\p{Cf}\p{Cc}\p{Zl}\p{Zp}]|(?! )\p{Zs}/gu,
    (c) => '\\u' + c.codePointAt(0).toString(16).toUpperCase().padStart(4, '0'))
}
