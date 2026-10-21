package extension

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	testPNG  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR....")
	testJPEG = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01")
)

func imageParam(mime string, data []byte) map[string]any {
	return map[string]any{"mime": mime, "data": base64.StdEncoding.EncodeToString(data)}
}

func sendWithImages(t *testing.T, f *fakeChat, images any) error {
	t.Helper()
	srv := chatSrv(f)
	_, err := callChat(t, srv, MethodChatSend, map[string]any{"profile": "default", "runtime": "claude", "message": "what is drawn?", "images": images}, nil)
	return err
}

// The images of a turn are written 0600 in a private folder for the runner,
// named by what they are (not by anything the page or the panel said), shown to
// it as --attach, fenced as untrusted in the message, and gone when the turn is.
func TestChatSendWritesImagesForTheTurnAndRemovesThem(t *testing.T) {
	var paths []string
	var contents []string
	var modes []os.FileMode
	var dirMode os.FileMode
	var message string
	f := &fakeChat{}
	f.onRun = func(s ChatTurnSpec) {
		message = s.Message
		for _, p := range s.Attach {
			paths = append(paths, p)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("an attached image is not on disk during the turn: %v", err)
			}
			contents = append(contents, string(b))
			fi, _ := os.Stat(p)
			modes = append(modes, fi.Mode().Perm())
			di, _ := os.Stat(filepath.Dir(p))
			dirMode = di.Mode().Perm()
		}
	}
	if err := sendWithImages(t, f, []any{imageParam("image/jpeg", testJPEG), imageParam("image/png", testPNG)}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || contents[0] != string(testJPEG) || contents[1] != string(testPNG) {
		t.Fatalf("paths %v contents %q", paths, contents)
	}
	if !strings.HasSuffix(paths[0], ".jpg") || !strings.HasSuffix(paths[1], ".png") {
		t.Errorf("names must come from the bytes: %v", paths)
	}
	for _, m := range modes {
		if m != 0o600 {
			t.Errorf("image mode %v, want 0600", m)
		}
	}
	if dirMode != 0o700 {
		t.Errorf("folder mode %v, want 0700", dirMode)
	}
	if !strings.Contains(message, "2 image") || !strings.Contains(message, "untrusted") {
		t.Errorf("the message does not fence the images: %q", message)
	}
	for _, p := range paths {
		if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
			t.Errorf("the turn's image folder outlived the turn: %s", filepath.Dir(p))
		}
	}
}

func TestChatSendRemovesImagesWhenTheTurnFails(t *testing.T) {
	var dir string
	f := &fakeChat{runErr: errBusy()}
	f.onRun = func(s ChatTurnSpec) { dir = filepath.Dir(s.Attach[0]) }
	if err := sendWithImages(t, f, []any{imageParam("image/png", testPNG)}); err == nil {
		t.Fatal("expected the turn's error")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a failed turn left its images: %s", dir)
	}
}

func TestChatSendRefusesBadImagesBeforeAnyTurn(t *testing.T) {
	big := append(append([]byte{}, testPNG...), make([]byte, chatMaxImageBytes)...)
	cases := map[string]any{
		"not a list":           "x",
		"entry not an object":  []any{"x"},
		"not base64":           []any{map[string]any{"mime": "image/png", "data": "!!!"}},
		"magic bytes disagree": []any{imageParam("image/png", testJPEG)},
		"not an image at all":  []any{imageParam("image/png", []byte("#!/bin/sh\nrm\n"))},
		"svg":                  []any{imageParam("image/svg+xml", []byte("<svg onload=x>"))},
		"too big":              []any{imageParam("image/png", big)},
		"too many":             []any{imageParam("image/png", testPNG), imageParam("image/png", testPNG), imageParam("image/png", testPNG), imageParam("image/png", testPNG)},
		"empty":                []any{imageParam("image/png", nil)},
	}
	for name, images := range cases {
		f := &fakeChat{}
		err := sendWithImages(t, f, images)
		if errCode(err) != CodeInvalidInput {
			t.Errorf("%s: err = %v, want invalid input", name, err)
		}
		if len(f.specs) != 0 || len(f.created) != 0 {
			t.Errorf("%s: a turn started anyway", name)
		}
	}
}

func TestChatTurnArgvPassesEachImage(t *testing.T) {
	argv := chatTurnArgv(ChatTurnSpec{Profile: "p", Conversation: "c", Turn: "t", Instance: "i", Attach: []string{"/x/a.png", "/x/b.jpg"}})
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--attach=/x/a.png") || !strings.Contains(joined, "--attach=/x/b.jpg") {
		t.Errorf("argv = %v", argv)
	}
	if strings.Contains(strings.Join(chatTurnArgv(ChatTurnSpec{Profile: "p", Conversation: "c", Turn: "t"}), " "), "--attach") {
		t.Error("a turn without images got --attach")
	}
}
