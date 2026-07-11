package mcpcore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveChatGPTImageDeclaresOpenAIFileParam(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted"})
	var descriptor Tool
	found := false
	for _, tool := range server.tools {
		if tool.Name == "save_chatgpt_image" {
			descriptor = tool
			found = true
			break
		}
	}
	if !found {
		t.Fatal("save_chatgpt_image descriptor is missing")
	}
	params, ok := descriptor.Meta["openai/fileParams"].([]string)
	if !ok || len(params) != 1 || params[0] != "source_image" {
		t.Fatalf("unexpected OpenAI file params metadata: %#v", descriptor.Meta)
	}
	raw, err := json.Marshal(descriptor.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"download_url", "file_id", "mime_type", "file_name"} {
		if !bytes.Contains(raw, []byte(required)) {
			t.Fatalf("file schema is missing %s: %s", required, raw)
		}
	}
	for _, forbidden := range []string{`"data"`, `"dataUrl"`, `"image"`, `"mimeType"`, `"createParents"`} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("save_chatgpt_image must not expose legacy inline field %s: %s", forbidden, raw)
		}
	}
	requiredFields, ok := descriptor.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("unexpected required field declaration: %#v", descriptor.InputSchema["required"])
	}
	foundPath := false
	foundSourceImage := false
	for _, required := range requiredFields {
		foundPath = foundPath || required == "path"
		foundSourceImage = foundSourceImage || required == "source_image"
	}
	if !foundPath || !foundSourceImage {
		t.Fatalf("path and source_image must be required: %#v", requiredFields)
	}
	if descriptor.Meta["openai/toolInvocation/invoking"] == "" || descriptor.Meta["openai/toolInvocation/invoked"] == "" {
		t.Fatalf("tool invocation metadata is incomplete: %#v", descriptor.Meta)
	}
}

func TestSaveChatGPTImageDownloadsOriginalFile(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	downloadServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("download method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer downloadServer.Close()

	workspace := t.TempDir()
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	server.imageURLValidator = func(value *url.URL) error {
		if value == nil || value.Scheme != "https" {
			return errors.New("test URL must use HTTPS")
		}
		return nil
	}
	server.imageHTTPClient = downloadServer.Client()
	result, err := server.executeTool("save_chatgpt_image", map[string]any{
		"path":           "images/original.png",
		"create_parents": true,
		"source_image": map[string]any{
			"download_url": downloadServer.URL + "/generated.png",
			"file_id":      "file_generated_test",
			"mime_type":    "image/png",
			"file_name":    "generated.png",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["sourceFileId"] != "file_generated_test" || result["mimeType"] != "image/png" {
		t.Fatalf("unexpected save result: %#v", result)
	}
	saved, err := os.ReadFile(filepath.Join(workspace, "images", "original.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, png) {
		t.Fatal("saved image bytes differ from the downloaded ChatGPT file")
	}
}

func TestSaveChatGPTImageStreamsFourKOriginalFile(t *testing.T) {
	image4K := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	random := rand.New(rand.NewSource(20260711))
	if _, err := random.Read(image4K.Pix); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image4K, &jpeg.Options{Quality: 88}); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	if len(original) < 2*1024*1024 {
		t.Fatalf("4K fixture is unexpectedly small: %d bytes", len(original))
	}

	downloadServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", fmt.Sprint(len(original)))
		_, _ = w.Write(original)
	}))
	defer downloadServer.Close()

	workspace := t.TempDir()
	server := mustNewServer(t, Options{Workspace: workspace, PermissionMode: "trusted"})
	server.imageURLValidator = func(value *url.URL) error {
		if value == nil || value.Scheme != "https" {
			return errors.New("test URL must use HTTPS")
		}
		return nil
	}
	server.imageHTTPClient = downloadServer.Client()
	result, err := server.executeTool("save_chatgpt_image", map[string]any{
		"path": "wallpapers/four-k.jpg",
		"source_image": map[string]any{
			"download_url": downloadServer.URL + "/four-k.jpg",
			"file_id":      "file_four_k",
			"mime_type":    "image/jpeg",
			"file_name":    "four-k.jpg",
		},
		"create_parents": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["sizeBytes"] != int64(len(original)) {
		t.Fatalf("saved size mismatch: %#v", result)
	}
	saved, err := os.ReadFile(filepath.Join(workspace, "wallpapers", "four-k.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, original) {
		t.Fatal("4K image bytes changed during file-parameter transfer")
	}
}

func TestSaveChatGPTImageRejectsUnsafeAndInlineSources(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted"})
	_, err := server.executeTool("save_chatgpt_image", map[string]any{
		"path": "image.png",
		"source_image": map[string]any{
			"download_url": "http://example.com/image.png",
			"file_id":      "file_test",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}

	_, err = server.executeTool("save_chatgpt_image", map[string]any{
		"path": "image.png",
		"source_image": map[string]any{
			"download_url": "https://example.com/image.png",
			"file_id":      "file_test",
		},
		"data": "AAAA",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected inline base64 field rejection, got %v", err)
	}
}

func TestImageDownloadURLPolicyRejectsPrivateAndNonstandardPorts(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/image.png",
		"https://127.0.0.1/image.png",
		"https://10.0.0.1/image.png",
		"https://example.com:8443/image.png",
		"https://user:pass@example.com/image.png",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateImageDownloadURL(parsed); err == nil {
			t.Fatalf("unsafe image URL was accepted: %s", raw)
		}
	}
}

func TestChatGPTImageDownloadURLIsRedactedFromAudit(t *testing.T) {
	redacted := redactAuditArguments(map[string]any{
		"path": "image.png",
		"source_image": map[string]any{
			"download_url": "https://files.example.test/private?signature=secret",
			"file_id":      "file_test",
		},
	})
	image, ok := redacted["source_image"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected redacted image value: %#v", redacted["source_image"])
	}
	download, ok := image["download_url"].(map[string]any)
	if !ok || download["redacted"] != true {
		t.Fatalf("signed download URL was not redacted: %#v", image["download_url"])
	}
}
