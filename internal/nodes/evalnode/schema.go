package evalnode

// EvaluationNodeSchema documents the config keys Node.Execute reads; see
// control.SetNodeSchema's doc comment for why this is a companion struct.
type EvaluationNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,options=setInputs|setOutputs|setMetrics|checkIfEvaluating,default=setOutputs,help=setInputs / setOutputs record the case's inputs or outputs (setOutputs also writes them back to the dataset row); setMetrics scores the run; checkIfEvaluating routes items to evaluation or normal. Only a run of monoagentcli workflow evaluate records anything."`

	Inputs string `json:"inputs" schema:"label=Inputs,type=code,language=json,rows=4,placeholder=[{\"name\": \"question\"， \"value\": \"{{ $json.question }}\"}],help=The values to show as the case's inputs.,depends_on_key=operation,depends_on_values=setInputs"`

	Source string `json:"source" schema:"label=Source,type=select,options=dataTable|googleSheets,default=dataTable,help=Where the dataset row is: a data table or a Google Sheet.,depends_on_key=operation,depends_on_values=setOutputs"`

	Table string `json:"table" schema:"label=Data Table,type=text,help=The dataset's data table (name or id). Empty: the table the evaluation trigger read.,depends_on_key=source,depends_on_values=dataTable"`

	CredentialID string `json:"credential_id" schema:"label=Google Sheets Account,type=text,depends_on_key=source,depends_on_values=googleSheets"`

	SpreadsheetID string `json:"spreadsheet_id" schema:"label=Spreadsheet ID,type=text,depends_on_key=source,depends_on_values=googleSheets"`

	SheetName string `json:"sheet_name" schema:"label=Sheet,type=text,default=Sheet1,depends_on_key=source,depends_on_values=googleSheets"`

	Outputs string `json:"outputs" schema:"label=Outputs,type=code,language=json,rows=4,placeholder=[{\"name\": \"answer\"， \"value\": \"{{ $json.output }}\"}],help=The values written into the dataset row (a column it lacks is added) and shown as the case's outputs.,depends_on_key=operation,depends_on_values=setOutputs"`

	Metric string `json:"metric" schema:"label=Metric,type=select,options=correctness|helpfulness|stringSimilarity|categorization|toolsUsed|customMetrics,default=correctness,help=correctness / helpfulness: a model scores 1-5. stringSimilarity: 0-1 by edit distance. categorization: 1 on an exact match. toolsUsed: share of the expected tools the agent called. customMetrics: your own numbers.,depends_on_key=operation,depends_on_values=setMetrics"`

	ExpectedAnswer string `json:"expected_answer" schema:"label=Expected Answer,type=text,help=The reference answer from the dataset (correctness， stringSimilarity， categorization).,depends_on_key=metric,depends_on_values=correctness|stringSimilarity|categorization"`

	ActualAnswer string `json:"actual_answer" schema:"label=Actual Answer,type=text,help=The answer the workflow gave.,depends_on_key=metric,depends_on_values=correctness|helpfulness|stringSimilarity|categorization"`

	UserQuery string `json:"user_query" schema:"label=User Query,type=text,help=The question the response answers.,depends_on_key=metric,depends_on_values=helpfulness"`

	ExpectedTools string `json:"expected_tools" schema:"label=Expected Tools,type=text,placeholder=Get Events， Send Email,help=Comma-separated names of the tools the agent should call.,depends_on_key=metric,depends_on_values=toolsUsed"`

	IntermediateSteps string `json:"intermediate_steps" schema:"label=Intermediate Steps,type=text,placeholder={{ $json.intermediateSteps }},help=The agent's steps: a list of {action: {tool}}.,depends_on_key=metric,depends_on_values=toolsUsed"`

	Metrics string `json:"metrics" schema:"label=Metrics to Return,type=code,language=json,rows=4,placeholder=[{\"name\": \"length\"， \"value\": \"{{ $json.output.length }}\"}],help=Each metric's name and numeric value.,depends_on_key=metric,depends_on_values=customMetrics"`

	MetricName string `json:"metric_name" schema:"label=Metric Name,type=text,help=The name the metric is recorded under (default: the metric's own name).,depends_on_key=metric,depends_on_values=correctness|helpfulness|stringSimilarity|categorization|toolsUsed"`

	Prompt string `json:"prompt" schema:"label=Prompt,type=text,help=How the model scores (default: a built-in grading guide).,depends_on_key=metric,depends_on_values=correctness|helpfulness"`

	InputPrompt string `json:"input_prompt" schema:"label=Input Prompt,type=text,help=The case as the model sees it: {expected_answer}， {actual_answer} and {user_query} are filled in.,depends_on_key=metric,depends_on_values=correctness|helpfulness"`

	Runtime string `json:"runtime" schema:"label=Runtime,type=text,placeholder=auto,help=The local agent runtime that scores (an attached chat model sets it; auto picks one).,depends_on_key=metric,depends_on_values=correctness|helpfulness"`

	Model string `json:"model" schema:"label=Model,type=text,depends_on_key=metric,depends_on_values=correctness|helpfulness"`

	Timeout float64 `json:"timeout" schema:"label=Timeout (seconds),type=number,default=120,min=1,depends_on_key=metric,depends_on_values=correctness|helpfulness"`
}
