import SendToCoderButton from './SendToCoderButton.jsx'
import { aitaskApi } from './SendToCoderApi.js'

// ApplyWithCoderButton has the coder fill in an application's form in the
// person's browser (`application fill start`). It never submits: the task
// header says "filled, waiting for you to submit" when it is done. With coder
// mode off it says why and the regular Apply beside it still works.
export default function ApplyWithCoderButton({ applicationId, ...rest }) {
  return (
    <SendToCoderButton
      label="Apply with coder"
      kind="application-apply"
      applicationId={applicationId}
      start={() => aitaskApi.startFill(applicationId)}
      doneLabel="filled, waiting for you to submit"
      origin={{ page: 'applications', label: 'Applications' }}
      {...rest}
    />
  )
}
