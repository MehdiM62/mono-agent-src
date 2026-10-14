package apipkg

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fastPolls drops every poll interval of p to 1 ms for tests.
func fastPolls(p *Package) {
	for i := range p.Operations {
		if p.Operations[i].Poll != nil {
			pl := *p.Operations[i].Poll
			pl.IntervalMs = 1
			p.Operations[i].Poll = &pl
		}
	}
}

// vendorServer records every request and answers it with fn, which may set
// response headers.
func vendorServer(t *testing.T, fn func(r seenReq, w http.ResponseWriter) string) *[]seenReq {
	t.Helper()
	var reqs []seenReq
	pagedServer(t, func(_ int, r *http.Request, w http.ResponseWriter) {
		b, _ := io.ReadAll(r.Body)
		s := seenReq{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Type: r.Header.Get("Content-Type"),
			Body: string(b), Header: r.Header.Clone()}
		reqs = append(reqs, s)
		fmt.Fprint(w, fn(s, w))
	})
	return &reqs
}

func reqLines(reqs []seenReq) string {
	var out []string
	for _, r := range reqs {
		line := r.Method + " " + r.Path
		if r.Query != "" {
			line += "?" + r.Query
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var gemCred = map[string]any{"api_key": "gk"}

// Gemini images: one item per generated image with the image data kept
// out of the JSON, and Imagen's :predict.
func TestGeminiImageItems(t *testing.T) {
	p := catalogPkg(t, "gemini_api")
	reqs := vendorServer(t, func(seenReq, http.ResponseWriter) string {
		return `{"candidates":[{"content":{"parts":[{"text":"here"},{"inlineData":{"mimeType":"image/jpeg","data":"` + b64("JPEG") + `"}}]},"finishReason":"STOP"}]}`
	})
	items, err := runItems(t, p, map[string]any{"credential": gemCred, "resource": "image", "operation": "generate", "prompt": "a cat"})
	if err != nil || len(items) != 1 || string(items[0].Binary["data"]) != "JPEG" {
		t.Fatalf("gemini image %v %+v", err, items)
	}
	j := items[0].JSON
	if strings.Contains(jsonOf(j), b64("JPEG")) || j["file_name"] != "image.jpeg" || j["mime_type"] != "image/jpeg" || j["finishReason"] != "STOP" {
		t.Errorf("gemini image json %v", j)
	}
	if (*reqs)[0].Path != "/v1beta/models/gemini-3.1-flash-image-preview:generateContent" || (*reqs)[0].Query != "key=gk" {
		t.Errorf("gemini image request %s?%s", (*reqs)[0].Path, (*reqs)[0].Query)
	}

	reqs = vendorServer(t, func(seenReq, http.ResponseWriter) string {
		return `{"predictions":[{"mimeType":"image/png","bytesBase64Encoded":"` + b64("P1") + `"},{"mimeType":"image/png","bytesBase64Encoded":"` + b64("P2") + `"}]}`
	})
	items, err = runItems(t, p, map[string]any{"credential": gemCred, "resource": "image", "operation": "generate", "prompt": "a cat",
		"modelId": "models/imagen-4.0-generate-001", "options": map[string]any{"sampleCount": 2, "binaryPropertyOutput": "pic"}})
	if err != nil || len(items) != 2 || string(items[1].Binary["pic"]) != "P2" {
		t.Fatalf("imagen %v %+v", err, items)
	}
	if r := (*reqs)[0]; r.Path != "/v1beta/models/imagen-4.0-generate-001:predict" || r.Body != `{"instances":[{"prompt":"a cat"}],"parameters":{"sampleCount":2}}` {
		t.Errorf("imagen request %s %s", r.Path, r.Body)
	}
}

// A file given by URL is fetched without the key, announced with a
// resumable start, sent to the upload URL from its headers and waited for.
func TestGeminiFileUploadFromURL(t *testing.T) {
	p := catalogPkg(t, "gemini_api")
	fastPolls(p)
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch {
		case r.Path == "/doc.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			return "%PDF"
		case r.Path == "/upload/v1beta/files":
			w.Header().Set("X-Goog-Upload-Url", "https://up.example.test/session?upload_id=9")
			return `{}`
		case r.Path == "/session":
			return `{"file":{"name":"files/f1","state":"PROCESSING"}}`
		}
		return `{"name":"files/f1","state":"ACTIVE","uri":"https://generativelanguage.googleapis.com/v1beta/files/f1","mimeType":"application/pdf"}`
	})
	items, err := runItems(t, p, map[string]any{"credential": gemCred, "resource": "file", "operation": "upload",
		"fileUrl": "https://files.example.test/doc.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "GET /doc.pdf\nPOST /upload/v1beta/files?key=gk\nPOST /session?key=gk&upload_id=9\nGET /v1beta/files/f1?key=gk"; reqLines(*reqs) != want {
		t.Errorf("requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	start := (*reqs)[1]
	if start.Header.Get("X-Goog-Upload-Header-Content-Length") != "4" || start.Header.Get("X-Goog-Upload-Header-Content-Type") != "application/pdf" ||
		start.Body != `{"file":{"display_name":"doc.pdf"}}` {
		t.Errorf("start %v %s", start.Header, start.Body)
	}
	if up := (*reqs)[2]; up.Body != "%PDF" || up.Header.Get("X-Goog-Upload-Command") != "upload, finalize" {
		t.Errorf("upload %v %q", up.Header, up.Body)
	}
	if len(items) != 1 || jsonOf(items[0].JSON) != `{"fileUri":"https://generativelanguage.googleapis.com/v1beta/files/f1","mimeType":"application/pdf"}` {
		t.Errorf("items %v", items)
	}
}

// Veo: the long-running operation is polled until done and every sample is
// downloaded with the key into the chosen binary property.
func TestGeminiVideoGenerateDownloads(t *testing.T) {
	p := catalogPkg(t, "gemini_api")
	fastPolls(p)
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch {
		case strings.HasSuffix(r.Path, ":predictLongRunning"):
			return `{"name":"models/veo-3.0-generate-001/operations/op1"}`
		case strings.HasSuffix(r.Path, ":download"):
			w.Header().Set("Content-Type", "video/mp4")
			return "MP4"
		}
		return `{"name":"models/veo-3.0-generate-001/operations/op1","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://generativelanguage.googleapis.com/v1beta/files/v1:download?alt=media"}}]}}}`
	})
	items, err := runItems(t, p, map[string]any{"credential": gemCred, "resource": "video", "operation": "generate",
		"modelId": "models/veo-3.0-generate-001", "prompt": "waves", "options": map[string]any{"binaryPropertyOutput": "clip"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "POST /v1beta/models/veo-3.0-generate-001:predictLongRunning?key=gk\nGET /v1beta/models/veo-3.0-generate-001/operations/op1?key=gk\nGET /v1beta/files/v1:download?alt=media&key=gk"; reqLines(*reqs) != want {
		t.Errorf("requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	if len(items) != 1 || string(items[0].Binary["clip"]) != "MP4" {
		t.Fatalf("items %+v", items)
	}
}

// Ollama image analysis fetches image URLs and sends them base64-encoded.
func TestOllamaImageURLs(t *testing.T) {
	p := catalogPkg(t, "ollama_api")
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "image/png")
			return "IMG" + r.Path
		}
		return `{"message":{"role":"assistant","content":"two cats"},"done":true}`
	})
	items, err := runItems(t, p, map[string]any{"resource": "image", "operation": "analyze", "modelId": "llava",
		"inputType": "url", "imageUrls": "https://img.test/a.png, https://img.test/b.png"})
	if err != nil || len(items) != 1 || jsonOf(items[0].JSON) != `{"content":"two cats"}` {
		t.Fatalf("items %v %v", err, items)
	}
	chat := (*reqs)[2]
	if chat.Path != "/api/chat" || chat.Header.Get("Authorization") != "" ||
		chat.Body != `{"messages":[{"content":"What's in this image?","images":["`+b64("IMG/a.png")+`","`+b64("IMG/b.png")+`"],"role":"user"}],"model":"llava","stream":false}` {
		t.Errorf("chat %s %s", chat.Path, chat.Body)
	}
}

// MiniMax video jobs: Hailuo polls v1, looks the file up and downloads it;
// MiniMax-H3 polls v2, whose answer carries the URL.
func TestMiniMaxVideoJobs(t *testing.T) {
	p := catalogPkg(t, "minimax_api")
	fastPolls(p)
	cred := map[string]any{"api_key": "mm"}
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch r.Path {
		case "/v1/video_generation":
			return `{"task_id":"t1","base_resp":{"status_code":0}}`
		case "/v1/query/video_generation":
			return `{"task_id":"t1","status":"Success","file_id":"f1","base_resp":{"status_code":0}}`
		case "/v1/files/retrieve":
			return `{"file":{"file_id":"f1","download_url":"https://cdn.test/v.mp4"},"base_resp":{"status_code":0}}`
		}
		w.Header().Set("Content-Type", "video/mp4")
		return "MP4"
	})
	items, err := runItems(t, p, map[string]any{"credential": cred, "resource": "video", "operation": "textToVideo",
		"modelId": "MiniMax-Hailuo-2.3", "prompt": "waves", "options": map[string]any{"promptOptimizer": false}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "POST /v1/video_generation\nGET /v1/query/video_generation?task_id=t1\nGET /v1/files/retrieve?file_id=f1\nGET /v.mp4"; reqLines(*reqs) != want {
		t.Errorf("v1 requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	if (*reqs)[3].Header.Get("Authorization") != "" {
		t.Error("the CDN download carried the API key")
	}
	if len(items) != 1 || string(items[0].Binary["data"]) != "MP4" || items[0].JSON["videoUrl"] != "https://cdn.test/v.mp4" ||
		items[0].JSON["taskId"] != "t1" || items[0].JSON["fileId"] != "f1" {
		t.Errorf("v1 items %+v", items)
	}
	if want := `{"duration":6,"model":"MiniMax-Hailuo-2.3","prompt":"waves","prompt_optimizer":false,"resolution":"768P"}`; (*reqs)[0].Body != want {
		t.Errorf("v1 body %s", (*reqs)[0].Body)
	}

	reqs = vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch r.Path {
		case "/v2/video_generation":
			return `{"task_id":"t2"}`
		case "/v2/query/video_generation/t2":
			return `{"task":{"status":"succeeded","content":{"url":"https://cdn.test/h3.mp4"}}}`
		}
		return "MP4"
	})
	items, err = runItems(t, p, map[string]any{"credential": cred, "resource": "video", "operation": "imageToVideo",
		"prompt": "zoom", "imageUrl": "https://img.test/first.png", "downloadVideo": false})
	if err != nil {
		t.Fatal(err)
	}
	if want := "POST /v2/video_generation\nGET /v2/query/video_generation/t2"; reqLines(*reqs) != want {
		t.Errorf("v2 requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	if want := `{"content":[{"text":"zoom","type":"text"},{"image_url":{"url":"https://img.test/first.png"},"role":"first_frame","type":"image_url"}],"duration":5,"model":"MiniMax-H3","ratio":"adaptive","resolution":"2K"}`; (*reqs)[0].Body != want {
		t.Errorf("v2 body %s", (*reqs)[0].Body)
	}
	if len(items) != 1 || items[0].JSON["videoUrl"] != "https://cdn.test/h3.mp4" || len(items[0].Binary) != 0 {
		t.Errorf("v2 items %+v", items)
	}
}

// Wan image models run as tasks on the endpoint their version needs; edit
// models get the reference images (fetched URLs and binaries) as data URIs.
func TestAlibabaWanImageTasks(t *testing.T) {
	p := catalogPkg(t, "alibaba_cloud_api")
	fastPolls(p)
	cred := map[string]any{"api_key": "ak", "region": "cn-beijing"}
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch {
		case r.Path == "/ref.png":
			w.Header().Set("Content-Type", "image/png")
			return "REF"
		case r.Path == "/out.png":
			w.Header().Set("Content-Type", "image/png")
			return "OUT"
		case r.Method == "POST":
			return `{"output":{"task_id":"t1","task_status":"PENDING"}}`
		}
		return `{"output":{"task_id":"t1","task_status":"SUCCEEDED","choices":[{"message":{"content":[{"image":"https://oss.test/out.png"}]}}]},"usage":{"image_count":1}}`
	})
	item := binItem(map[string][2]string{"photo": {"p.jpg", "image/jpeg"}})
	items, err := runItems(t, p, map[string]any{"credential": cred, "resource": "image", "operation": "generate",
		"modelId": "wan2.6-image", "prompt": "merge", "referenceImages": map[string]any{"values": []any{
			map[string]any{"inputType": "binary", "binaryPropertyName": "photo"},
			map[string]any{"inputType": "url", "imageUrl": "https://img.test/ref.png"}}}}, item)
	if err != nil {
		t.Fatal(err)
	}
	if want := "GET /ref.png\nPOST /api/v1/services/aigc/image-generation/generation\nGET /api/v1/tasks/t1\nGET /out.png"; reqLines(*reqs) != want {
		t.Errorf("requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	create := (*reqs)[1]
	if create.Header.Get("X-DashScope-Async") != "enable" ||
		create.Body != `{"input":{"messages":[{"content":[{"text":"merge"},{"image":"data:image/jpeg;base64,`+b64("BYTES-photo")+`"},{"image":"data:image/png;base64,`+b64("REF")+`"}],"role":"user"}]},"model":"wan2.6-image","parameters":{"n":1,"prompt_extend":false,"size":"1280*1280"}}` {
		t.Errorf("create %v %s", create.Header.Get("X-DashScope-Async"), create.Body)
	}
	if len(items) != 1 || string(items[0].Binary["data"]) != "OUT" || items[0].JSON["imageUrl"] != "https://oss.test/out.png" {
		t.Errorf("items %+v", items)
	}

	// Before 2.6 the text2image endpoint takes a prompt and 1024*1024 below 2.5.
	reqs = vendorServer(t, func(r seenReq, _ http.ResponseWriter) string {
		return `{"output":{"task_id":"t2","task_status":"SUCCEEDED","results":[{"url":"https://oss.test/r.png"}]}}`
	})
	items, err = runItems(t, p, map[string]any{"credential": cred, "resource": "image", "operation": "generate",
		"modelId": "wanx2.1-t2i-turbo", "prompt": "a hill", "downloadImage": false})
	if err != nil || len(items) != 1 || items[0].JSON["imageUrl"] != "https://oss.test/r.png" {
		t.Fatalf("t2i %v %+v", err, items)
	}
	if r := (*reqs)[0]; r.Path != "/api/v1/services/aigc/text2image/image-synthesis" ||
		r.Body != `{"input":{"prompt":"a hill"},"model":"wanx2.1-t2i-turbo","parameters":{"n":1,"prompt_extend":false,"size":"1024*1024"}}` {
		t.Errorf("t2i request %s %s", r.Path, r.Body)
	}

	// -i2i models send the references in input.images.
	reqs = vendorServer(t, func(r seenReq, _ http.ResponseWriter) string {
		return `{"output":{"task_id":"t3","task_status":"SUCCEEDED","results":[{"url":"https://oss.test/r.png"}]}}`
	})
	_, err = runItems(t, p, map[string]any{"credential": cred, "resource": "image", "operation": "generate",
		"modelId": "wan2.5-i2i-preview", "prompt": "style", "downloadImage": false, "referenceImages": map[string]any{"values": []any{
			map[string]any{"inputType": "url", "imageUrl": "data:image/png;base64,QQ=="}}}})
	if err != nil {
		t.Fatal(err)
	}
	if r := (*reqs)[0]; r.Path != "/api/v1/services/aigc/image2image/image-synthesis" ||
		r.Body != `{"input":{"images":["data:image/png;base64,QQ=="],"prompt":"style"},"model":"wan2.5-i2i-preview","parameters":{"n":1,"prompt_extend":false,"size":"1280*1280"}}` {
		t.Errorf("i2i request %s %s", r.Path, r.Body)
	}
}

// Wan video tasks are polled until they succeed; a failed task fails the item.
func TestAlibabaVideoTask(t *testing.T) {
	p := catalogPkg(t, "alibaba_cloud_api")
	fastPolls(p)
	cred := map[string]any{"api_key": "ak"}
	status := "SUCCEEDED"
	reqs := vendorServer(t, func(r seenReq, w http.ResponseWriter) string {
		switch {
		case r.Method == "POST":
			return `{"output":{"task_id":"v1","task_status":"PENDING"}}`
		case r.Path == "/v.mp4":
			return "MP4"
		}
		return `{"output":{"task_id":"v1","task_status":"` + status + `","video_url":"https://oss.test/v.mp4","code":"Bad","message":"no"},"usage":{"duration":5}}`
	})
	items, err := runItems(t, p, map[string]any{"credential": cred, "resource": "video", "operation": "textToVideo",
		"prompt": "waves", "simplify": false})
	if err != nil {
		t.Fatal(err)
	}
	if want := "POST /api/v1/services/aigc/video-generation/video-synthesis\nGET /api/v1/tasks/v1\nGET /v.mp4"; reqLines(*reqs) != want {
		t.Errorf("requests\n%s\nwant\n%s", reqLines(*reqs), want)
	}
	if len(items) != 1 || string(items[0].Binary["data"]) != "MP4" || items[0].JSON["taskId"] != "v1" || items[0].JSON["videoUrl"] != "https://oss.test/v.mp4" {
		t.Errorf("items %+v", items)
	}
	status = "FAILED"
	_, err = runItems(t, p, map[string]any{"credential": cred, "resource": "video", "operation": "textToVideo", "prompt": "waves"})
	if err == nil || !strings.Contains(err.Error(), "[Bad] no") {
		t.Errorf("failed task: %v", err)
	}
}
