// wails-app/app_application_assets.go
package main

// ApplicationAssets is everything around one application: `application show
// <id> --json --assets`'s "assets" object, field for field. This file shells
// out through runMonoCLI and imports no internal package -- the CLI's JSON is
// the contract.
type ApplicationAssets struct {
	Posting   AssetPosting    `json:"posting"`
	Documents []AssetDocument `json:"documents"`
	Fill      AssetFill       `json:"fill"`
	People    []AssetPerson   `json:"people"`
	Messages  []AssetMessage  `json:"messages"`
	Tasks     []AssetTask     `json:"tasks"`
	Task      *AssetAITask    `json:"task"`
	Links     []AssetLink     `json:"links"`
}

type AssetPosting struct {
	URL          string `json:"url"`
	CapturedText string `json:"capturedText"`
	CapturedAt   string `json:"capturedAt"`
	Deadline     string `json:"deadline"`
}

type AssetDocument struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	CreatedAt string `json:"createdAt"`
	Attached  bool   `json:"attached"`
	Path      string `json:"path"`
}

type AssetFillField struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	Generated bool   `json:"generated"`
}

type AssetFill struct {
	Status     string           `json:"status"`
	Screenshot string           `json:"screenshot"`
	UpdatedAt  string           `json:"updatedAt"`
	Fields     []AssetFillField `json:"fields"`
	Note       string           `json:"note"`
}

type AssetPerson struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Email string `json:"email"`
}

type AssetMessage struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	Direction string `json:"direction"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet"`
	At        string `json:"at"`
}

type AssetTask struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type AssetAITask struct {
	TaskID         string `json:"taskId"`
	ConversationID string `json:"conversationId"`
	Status         string `json:"status"`
}

type AssetLink struct {
	Kind  string `json:"kind"`
	RefID string `json:"refId"`
}

// GetApplicationAssets returns the application's posting, documents, form
// fill, linked people, messages and tasks, and its running aitask (nil when
// none).
func (a *App) GetApplicationAssets(id string) (ApplicationAssets, error) {
	var out ApplicationAssets
	if err := a.signedOut(); err != nil {
		return out, err
	}
	var result struct {
		Assets ApplicationAssets `json:"assets"`
	}
	if err := a.runMonoCLI("", &result, "application", "show", id, "--assets"); err != nil {
		return out, err
	}
	out = result.Assets
	if out.Documents == nil {
		out.Documents = []AssetDocument{}
	}
	if out.Fill.Fields == nil {
		out.Fill.Fields = []AssetFillField{}
	}
	if out.People == nil {
		out.People = []AssetPerson{}
	}
	if out.Messages == nil {
		out.Messages = []AssetMessage{}
	}
	if out.Tasks == nil {
		out.Tasks = []AssetTask{}
	}
	if out.Links == nil {
		out.Links = []AssetLink{}
	}
	return out, nil
}
