package control

// RespondChatNodeSchema documents the config keys RespondChatNode.Execute
// reads out of its map[string]interface{} config — see SetNodeSchema's doc
// comment for why this is a companion struct rather than the runtime config,
// and internal/tools/schemagen for the tag grammar.
type RespondChatNodeSchema struct {
	Operation       string `json:"operation" schema:"label=Operation,type=select,options=send|sendAndWait,default=send,help=send posts the message into the chat of the trigger.chat that started the run; sendAndWait also waits for the person's answer there."`
	Message         string `json:"message" schema:"label=Message,type=textarea,required,rows=4"`
	ResponseType    string `json:"responseType" schema:"label=Response Type,type=select,options=freeTextChat|approval,default=freeTextChat,help=freeTextChat: the next message typed is the answer. approval: Approve/Disapprove buttons in the chat (a typed message counts as disapproval).,depends_on_key=operation,depends_on_values=sendAndWait"`
	ApprovalOptions string `json:"approvalOptions" schema:"label=Approval Options (JSON),type=code,language=json,rows=3,placeholder={\"values\": {\"approvalType\": \"double\"， \"approveLabel\": \"Yes\"， \"disapproveLabel\": \"No\"}},depends_on_key=responseType,depends_on_values=approval"`
	BlockUserInput  bool   `json:"blockUserInput" schema:"label=Block User Input,type=boolean,default=false,help=No typing until a button is pressed.,depends_on_key=responseType,depends_on_values=approval"`
	Options         string `json:"options" schema:"label=Options (JSON),type=code,language=json,rows=3,placeholder={\"limitWaitTime\": {\"values\": {\"resumeAmount\": 1， \"resumeUnit\": \"hours\"}}},help=limitWaitTime gives up waiting and passes the input items on.,depends_on_key=operation,depends_on_values=sendAndWait"`
}
