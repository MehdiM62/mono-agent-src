import { useTranslation } from 'react-i18next'
import { ArrowUpRight } from 'lucide-react'

// ToolLink is a tool card's "Open in <tab>" (#473): link is {page} from
// toolLinks.js, onNavigate the app's navigate(pageId). Nothing without both.
export function ToolLink({ link, onNavigate, style }) {
  const { t } = useTranslation()
  if (!link?.page || !onNavigate) return null
  const tab = t(`toolLinks.page.${link.page}`)
  return (
    <button type="button" data-testid="tool-link" data-page={link.page} onClick={() => onNavigate(link.page)}
      title={t('toolLinks.open', { tab })}
      style={{
        display: 'inline-flex', alignItems: 'center', gap: 3, background: 'transparent', border: 'none', padding: 0,
        cursor: 'pointer', color: '#00b4d8', fontFamily: 'var(--font-mono)', fontSize: 9.5, ...style,
      }}>
      {t('toolLinks.open', { tab })} <ArrowUpRight size={10} aria-hidden="true" />
    </button>
  )
}
