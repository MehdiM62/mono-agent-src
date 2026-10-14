package chat

// chatFlags are the chat command's flag values (newChatCmd registers them).
type chatFlags struct {
	runtime        string
	model          string
	effort         string
	resume         string
	canvasID       string
	historyID      string
	timeoutS       string
	budget         float64
	tools          string
	noHistory      bool
	promptStdin    bool
	conversationID string
	turnID         string
	instanceID     string
	mode           string
	coderCwd       string
	newWorkspace   bool
	coderRoot      bool
	profileFold    bool
}
