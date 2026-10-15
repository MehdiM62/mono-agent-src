package ref

import (
	"io"
	"os"
	"strings"
	"testing"
)

// `ref api` says an ownerless workflow belongs to nobody, not to 'default'.
func TestRefAPIOwnerlessWorkflowBelongsToNobody(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	refAPICmd().Run(nil, nil)
	os.Stdout = old
	w.Close()
	b, _ := io.ReadAll(r)
	text := strings.Join(strings.Fields(string(b)), " ")
	if strings.Contains(text, "belong to default") {
		t.Error("`ref api` still says legacy workflows belong to default")
	}
	if !strings.Contains(text, "belongs to nobody") {
		t.Error("`ref api` does not say an ownerless workflow belongs to nobody")
	}
}
