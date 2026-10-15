package profiledir

import "errors"

// ErrNoProfile is returned when a profile-scoped call names no profile. There
// is no implicit default. It lives in this leaf package so storage, workflow
// and recording share one value (errors.Is matches any of their aliases)
// without importing each other; callers and the UI test it to say "pass
// --profile".
var ErrNoProfile = errors.New("no profile named: pass --profile <id|name>")
