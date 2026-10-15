import { useEffect, useState, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { FileText, Upload, Trash2 } from 'lucide-react'
import * as WailsApp from '../../wailsjs/go/main/App'
import { errorText, hasValue, isPdfDataUrl } from './infoModel.js'

// A photo/scan slot: thumbnail (a file chip for a PDF), Upload, Remove.
export default function InfoPhotoSlot({ field, entry, onChanged }) {
  const { t } = useTranslation()
  const id = useId()
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const stored = hasValue(entry)
  const version = entry ? `${entry.value}|${entry.updated_at}` : ''

  useEffect(() => {
    let live = true
    if (!stored) { setUrl(''); return undefined }
    WailsApp.GetProfileInfoPhoto(field.key).then(
      u => { if (live) { setUrl(u || ''); setError('') } },
      e => { if (live) setError(t('info.photoFailed', { message: errorText(e) })) },
    )
    return () => { live = false }
  }, [field.key, stored, version, t])

  const upload = async () => {
    setError('')
    try {
      const path = await WailsApp.OpenAnyFilePicker(t('info.filePicker'))
      if (!path) return
      if (path.startsWith('{')) { setError(errorText(JSON.parse(path).error)); return }
      setBusy(true)
      await WailsApp.SetProfileInfoPhoto(field.key, path)
      await onChanged()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    setError('')
    setBusy(true)
    try {
      await WailsApp.ClearProfileInfo(field.key)
      setUrl('')
      await onChanged()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="info-field">
      <div className="info-label-row"><span className="info-label" id={id}>{field.label}</span></div>
      <div className="info-photo" role="group" aria-labelledby={id}>
        <div className="info-photo-box">
          {!stored && t('info.noFile')}
          {stored && isPdfDataUrl(url) && <span title={entry.value} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2 }}><FileText size={18} />PDF</span>}
          {stored && url && !isPdfDataUrl(url) && <img src={url} alt={field.label} />}
        </div>
        <button type="button" className="info-btn" disabled={busy} onClick={upload} aria-label={`${stored ? t('info.replace') : t('info.upload')}: ${field.label}`}>
          <Upload size={12} /> {stored ? t('info.replace') : t('info.upload')}
        </button>
        {stored && (
          <button type="button" className="info-btn" disabled={busy} onClick={remove} aria-label={`${t('info.remove')}: ${field.label}`}>
            <Trash2 size={12} /> {t('info.remove')}
          </button>
        )}
      </div>
      <div className="info-error-text" role={error ? 'alert' : undefined}>{error}</div>
    </div>
  )
}
