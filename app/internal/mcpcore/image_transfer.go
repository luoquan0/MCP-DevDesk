package mcpcore

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxImageRedirects = 5

func newImageDownloadClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ExpectContinueTimeout = 2 * time.Second

	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxImageRedirects {
				return errors.New("image download exceeded redirect limit")
			}
			return validateImageDownloadURL(req.URL)
		},
	}
}

func validateImageDownloadURL(value *url.URL) error {
	if value == nil || !strings.EqualFold(value.Scheme, "https") || strings.TrimSpace(value.Hostname()) == "" {
		return errors.New("ChatGPT file download URL must use HTTPS")
	}
	if value.User != nil {
		return errors.New("ChatGPT file download URL must not contain credentials")
	}
	return nil
}

func (s *Server) downloadOpenAIFile(file *openAIFileInput) ([]byte, string, error) {
	if file == nil {
		return nil, "", errors.New("image file parameter is required")
	}
	if strings.TrimSpace(file.FileID) == "" {
		return nil, "", errors.New("image.file_id is required")
	}
	downloadURL, err := url.Parse(strings.TrimSpace(file.DownloadURL))
	if err != nil || validateImageDownloadURL(downloadURL) != nil {
		return nil, "", errors.New("image.download_url must be a valid HTTPS URL")
	}

	request, err := http.NewRequest(http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return nil, "", errors.New("unable to create image download request")
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,application/octet-stream;q=0.8")
	request.Header.Set("User-Agent", "MCP-DevDesk/"+s.version)

	client := s.imageHTTPClient
	if client == nil {
		client = newImageDownloadClient()
	}
	response, err := client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) && urlError.Err != nil {
			return nil, "", fmt.Errorf("download ChatGPT image: %w", urlError.Err)
		}
		return nil, "", errors.New("download ChatGPT image failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download ChatGPT image returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxDownloadedImageBytes {
		return nil, "", fmt.Errorf("image exceeds %d bytes", maxDownloadedImageBytes)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, int64(maxDownloadedImageBytes)+1))
	if err != nil {
		return nil, "", fmt.Errorf("read ChatGPT image: %w", err)
	}
	if len(data) == 0 {
		return nil, "", errors.New("downloaded ChatGPT image is empty")
	}
	if len(data) > maxDownloadedImageBytes {
		return nil, "", fmt.Errorf("image exceeds %d bytes", maxDownloadedImageBytes)
	}

	declaredMIME := strings.TrimSpace(file.MIMEType)
	if declaredMIME == "" {
		responseMIME := normalizeImageMIME(response.Header.Get("Content-Type"))
		if supportedImageMIME(responseMIME) {
			declaredMIME = responseMIME
		}
	}
	return data, declaredMIME, nil
}
