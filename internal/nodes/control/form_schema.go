package control

// FormNodeSchema documents the config keys FormNode.Execute reads out of its
// map[string]interface{} config — see SetNodeSchema's doc comment for why
// this is a companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
type FormNodeSchema struct {
	Operation       string  `json:"operation" schema:"label=Page Type,type=select,options=page|completion,default=page,help=page shows the next form page to the person who submitted the Form trigger; completion ends the form with a final screen. Place it after a trigger.form."`
	FormTitle       string  `json:"formTitle" schema:"label=Form Title,type=text,depends_on_key=operation,depends_on_values=page"`
	FormDescription string  `json:"formDescription" schema:"label=Form Description,type=textarea,depends_on_key=operation,depends_on_values=page"`
	FormFields      string  `json:"formFields" schema:"label=Form Fields,type=code,language=json,rows=6,placeholder=[{\"fieldLabel\": \"Name\"， \"requiredField\": true}],help=A list of {fieldLabel， fieldName， fieldType (text|textarea|number|email|password|date|dropdown|radio|checkbox)， requiredField， placeholder， fieldOptions: {values: [{option}]}， multiselect}.,depends_on_key=operation,depends_on_values=page"`
	ButtonLabel     string  `json:"buttonLabel" schema:"label=Button Label,type=text,placeholder=Submit,depends_on_key=operation,depends_on_values=page"`
	LimitWaitTime   bool    `json:"limitWaitTime" schema:"label=Limit Wait Time,type=boolean,default=false,help=Give up waiting for the page after a while and continue with the input items.,depends_on_key=operation,depends_on_values=page"`
	LimitType       string  `json:"limitType" schema:"label=Limit Type,type=select,options=afterTimeInterval|atSpecifiedTime,default=afterTimeInterval,help=Used when Limit Wait Time is on.,depends_on_key=operation,depends_on_values=page"`
	ResumeAmount    float64 `json:"resumeAmount" schema:"label=Limit Amount,type=number,default=1,min=0,help=Used with limit type afterTimeInterval.,depends_on_key=operation,depends_on_values=page"`
	ResumeUnit      string  `json:"resumeUnit" schema:"label=Limit Unit,type=select,options=seconds|minutes|hours|days,default=hours,depends_on_key=operation,depends_on_values=page"`
	MaxDateAndTime  string  `json:"maxDateAndTime" schema:"label=Limit Date and Time,type=text,placeholder=2026-12-31T09:00:00Z,help=Used with limit type atSpecifiedTime.,depends_on_key=operation,depends_on_values=page"`

	RespondWith       string `json:"respondWith" schema:"label=On Submission,type=select,options=text|redirect|showText,default=text,help=text shows Completion Title and Message; redirect sends the browser to the URL; showText shows Text (HTML allowed) as the page.,depends_on_key=operation,depends_on_values=completion"`
	CompletionTitle   string `json:"completionTitle" schema:"label=Completion Title,type=text,depends_on_key=respondWith,depends_on_values=text"`
	CompletionMessage string `json:"completionMessage" schema:"label=Completion Message,type=textarea,depends_on_key=respondWith,depends_on_values=text"`
	RedirectURL       string `json:"redirectUrl" schema:"label=Redirect URL,type=text,placeholder=https://example.com/thanks,depends_on_key=respondWith,depends_on_values=redirect"`
	ResponseText      string `json:"responseText" schema:"label=Text,type=textarea,depends_on_key=respondWith,depends_on_values=showText"`
}
