package comm

import "github.com/monoes/mono-agent/internal/credhttp"

// commHTTP sends the comm nodes' REST requests (Twilio, WhatsApp): each
// carries the account's token, so a redirect elsewhere gets none of it
// (credhttp).
var commHTTP = credhttp.Client(nil)
