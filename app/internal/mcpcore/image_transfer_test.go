package mcpcore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if !ok || len(params) != 1 || params[0] != "image" {
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
	server.imageHTTPClient = downloadServer.Client()
	result, err := server.executeTool("save_chatgpt_image", map[string]any{
		"path":          "images/original.png",
		"createParents": true,
		"image": map[string]any{
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

func TestSaveChatGPTImageRejectsUnsafeOrAmbiguousSources(t *testing.T) {
	server := mustNewServer(t, Options{Workspace: t.TempDir(), PermissionMode: "trusted"})
	_, err := server.executeTool("save_chatgpt_image", map[string]any{
		"path": "image.png",
		"image": map[string]any{
			"download_url": "http://example.com/image.png",
			"file_id":      "file_test",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}

	_, err = server.executeTool("save_chatgpt_image", map[string]any{
		"path": "image.png",
		"image": map[string]any{
			"download_url": "https://example.com/image.png",
			"file_id":      "file_test",
		},
		"data": "AAAA",
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one image source") {
		t.Fatalf("expected ambiguous source error, got %v", err)
	}
}

func TestChatGPTImageDownloadURLIsRedactedFromAudit(t *testing.T) {
	redacted := redactAuditArguments(map[string]any{
		"path": "image.png",
		"image": map[string]any{
			"download_url": "https://files.example.test/private?signature=secret",
			"file_id":      "file_test",
		},
	})
	image, ok := redacted["image"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected redacted image value: %#v", redacted["image"])
	}
	download, ok := image["download_url"].(map[string]any)
	if !ok || download["redacted"] != true {
		t.Fatalf("signed download URL was not redacted: %#v", image["download_url"])
	}
}
