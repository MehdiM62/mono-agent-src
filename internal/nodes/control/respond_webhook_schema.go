package control

// RespondWebhookNodeSchema documents the config keys RespondWebhookNode.Execute
// reads out of its map[string]interface{} config — see SetNodeSchema's doc
// comment for why this is a companion struct rather than the runtime config,
// and internal/tools/schemagen for the tag grammar.
type RespondWebhookNodeSchema struct {
	RespondWith string `json:"respond_with" schema:"label=Respond With,type=select,options=firstIncomingItem|allIncomingItems|json|text|noData,default=firstIncomingItem,help=What the webhook caller gets back. Used when the webhook trigger's Response Mode is responseNode; elsewhere the node only passes items through."`

	ResponseCode float64 `json:"response_code" schema:"label=Response Code,type=number,default=200,min=100,max=599"`

	ResponseHeaders string `json:"response_headers" schema:"label=Response Headers (JSON),type=textarea,rows=3,placeholder={ \"X-Request-Id\": \"{{ $json.id }}\" }"`

	ResponseBody string `json:"response_body" schema:"label=Response Body,type=textarea,rows=5,depends_on_key=respond_with,depends_on_values=json|text,help=For json: JSON text (expressions allowed). For text: the body as is."`
}
