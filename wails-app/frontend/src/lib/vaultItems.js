// The image/file vault holds images and, since vault.save, files the
// extension saved (PDF, text, Markdown, CSV). Only images have a thumbnail or
// can be picked as an image; the rest show as files.

// The image extensions the vault treats as images (internal/imagescan
// AllowedExtensions).
const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'])

// isVaultImage reports whether a vault item is an image, by its stored file name.
export function isVaultImage(item) {
  const name = String(item?.filename || item?.path || '')
  const dot = name.lastIndexOf('.')
  return dot >= 0 && IMAGE_EXTENSIONS.has(name.slice(dot + 1).toLowerCase())
}

// vaultFileKind is a short label for a non-image item ("PDF", "MD"), or "FILE".
export function vaultFileKind(item) {
  const name = String(item?.filename || '')
  const dot = name.lastIndexOf('.')
  const ext = dot >= 0 ? name.slice(dot + 1).toUpperCase() : ''
  return ext && ext.length <= 4 ? ext : 'FILE'
}
