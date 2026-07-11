package mcpcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxImageRedirects     = 5
	imageDNSLookupTimeout = 10 * time.Second
	imageDownloadTimeout  = 2 * time.Minute
	imageResponseTimeout  = 30 * time.Second
)

type imageURLValidator func(*url.URL) error

func newImageDownloadClient(validate imageURLValidator) *http.Client {
	validator := validate
	if validator == nil {
		validator = validateImageDownloadURL
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = imageResponseTimeout
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ExpectContinueTimeout = 2 * time.Second

	return &http.Client{
		Transport: transport,
		Timeout:   imageDownloadTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxImageRedirects {
				return errors.New("image download exceeded redirect limit")
			}
			return validator(req.URL)
		},
	}
}

func validateImageDownloadURL(value *url.URL) error {
	if value == nil || !strings.EqualFold(value.Scheme, "https") {
		return errors.New("ChatGPT file download URL must use HTTPS")
	}
	if value.User != nil {
		return errors.New("ChatGPT file download URL must not contain credentials")
	}
	hostname := strings.TrimSpace(strings.TrimSuffix(value.Hostname(), "."))
	if hostname == "" {
		return errors.New("ChatGPT file download URL must include a hostname")
	}
	if port := value.Port(); port != "" && port != "443" {
		return errors.New("ChatGPT file download URL must use port 443")
	}
	if !imageDownloadHostAllowed(hostname) {
		return errors.New("ChatGPT file download hostname is not allowlisted")
	}

	if literal := net.ParseIP(hostname); literal != nil {
		if !isPublicImageDownloadIP(literal) {
			return errors.New("ChatGPT file download URL resolves to a non-public address")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), imageDNSLookupTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return errors.New("ChatGPT file download hostname could not be resolved")
	}
	if len(addresses) == 0 {
		return errors.New("ChatGPT file download hostname returned no addresses")
	}
	for _, address := range addresses {
		if !isPublicImageDownloadIP(address.IP) {
			return errors.New("ChatGPT file download URL resolves to a non-public address")
		}
	}
	return nil
}

func imageDownloadHostAllowed(hostname string) bool {
	raw := strings.TrimSpace(os.Getenv("MCP_DEV_DESK_IMAGE_DOWNLOAD_HOSTS"))
	if raw == "" {
		return true
	}
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, item := range strings.Split(raw, ",") {
		pattern := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(item, ".")))
		switch {
		case pattern == "*":
			return true
		case strings.HasPrefix(pattern, "*."):
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(hostname, suffix) && hostname != strings.TrimPrefix(suffix, ".") {
				return true
			}
		case pattern == hostname:
			return true
		}
	}
	return false
}

func isPublicImageDownloadIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !ip.IsUnspecified() &&
		!ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsInterfaceLocalMulticast() &&
		!ip.IsMulticast()
}

func (s *Server) saveChatGPTImage(args saveChatGPTImageArgs) (result map[string]any, err error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, errors.New("path is required")
	}
	if args.SourceImage == nil {
		return nil, errors.New("source_image is required")
	}
	if strings.TrimSpace(args.SourceImage.FileID) == "" {
		return nil, errors.New("source_image.file_id is required")
	}
	downloadURL, parseErr := url.Parse(strings.TrimSpace(args.SourceImage.DownloadURL))
	if parseErr != nil || downloadURL == nil {
		return nil, errors.New("source_image.download_url must be a valid HTTPS URL")
	}
	validator := s.imageURLValidator
	if validator == nil {
		validator = validateImageDownloadURL
	}
	if validateErr := validator(downloadURL); validateErr != nil {
		return nil, validateErr
	}

	maxBytes := args.MaxBytes
	if maxBytes == 0 {
		maxBytes = maxDownloadedImageBytes
	}
	if maxBytes < 1024 || maxBytes > maxDownloadedImageBytes {
		return nil, fmt.Errorf("max_bytes must be between 1024 and %d", maxDownloadedImageBytes)
	}
	_, target, display, err := s.prepareImageTarget(args.Path, args.Overwrite, args.CreateParents)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest(http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return nil, errors.New("unable to create image download request")
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,application/octet-stream;q=0.8")
	request.Header.Set("User-Agent", "MCP-DevDesk/"+s.version)
	client := s.imageHTTPClient
	if client == nil {
		client = newImageDownloadClient(validator)
	}
	response, err := client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) && urlError.Err != nil {
			return nil, fmt.Errorf("download ChatGPT image: %w", urlError.Err)
		}
		return nil, errors.New("download ChatGPT image failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download ChatGPT image returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("remote image exceeds max_bytes %d", maxBytes)
	}

	temp, err := os.CreateTemp(filepath.Dir(target), ".mcp-devdesk-image-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create image temporary file: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("secure image temporary file: %w", err)
	}

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ChatGPT image: %w", err)
	}
	if written == 0 {
		return nil, errors.New("downloaded ChatGPT image is empty")
	}
	if written > maxBytes {
		return nil, fmt.Errorf("remote image exceeds max_bytes %d", maxBytes)
	}
	if err := temp.Sync(); err != nil {
		return nil, fmt.Errorf("sync image temporary file: %w", err)
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind image temporary file: %w", err)
	}
	header := make([]byte, 512)
	headerSize, readErr := io.ReadFull(temp, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("inspect downloaded image: %w", readErr)
	}
	header = header[:headerSize]
	detectedMIME := normalizeImageMIME(http.DetectContentType(header))
	if !supportedImageMIME(detectedMIME) {
		return nil, fmt.Errorf("downloaded file is not a supported PNG, JPEG, GIF, or WebP image: %s", detectedMIME)
	}
	if err := validateDeclaredImageMIME(args.SourceImage.MIMEType, detectedMIME, "source_image.mime_type"); err != nil {
		return nil, err
	}
	responseMIME := normalizeImageMIME(response.Header.Get("Content-Type"))
	if supportedImageMIME(responseMIME) {
		if err := validateDeclaredImageMIME(responseMIME, detectedMIME, "HTTP Content-Type"); err != nil {
			return nil, err
		}
	}
	if err := validateImageExtension(target, detectedMIME); err != nil {
		return nil, err
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind image for validation: %w", err)
	}
	if err := validateImageFile(temp, detectedMIME); err != nil {
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("close image temporary file: %w", err)
	}
	if err := replaceFile(tempPath, target); err != nil {
		return nil, fmt.Errorf("replace image file: %w", err)
	}
	committed = true

	result = map[string]any{
		"path":         display,
		"mimeType":     detectedMIME,
		"sizeBytes":    written,
		"sha256":       hex.EncodeToString(hash.Sum(nil)),
		"saved":        true,
		"sourceFileId": args.SourceImage.FileID,
	}
	if fileName := strings.TrimSpace(args.SourceImage.FileName); fileName != "" {
		result["sourceFileName"] = fileName
	}
	return result, nil
}

func validateDeclaredImageMIME(declared, detected, source string) error {
	declared = normalizeImageMIME(declared)
	if declared == "" || declared == "application/octet-stream" {
		return nil
	}
	if !supportedImageMIME(declared) {
		return fmt.Errorf("%s declares an unsupported image MIME type %s", source, declared)
	}
	if declared != normalizeImageMIME(detected) {
		return fmt.Errorf("%s %s does not match downloaded image bytes %s", source, declared, detected)
	}
	return nil
}

func validateImageFile(file *os.File, mimeType string) error {
	if file == nil {
		return errors.New("image validation file is missing")
	}
	if normalizeImageMIME(mimeType) == "image/webp" {
		header := make([]byte, 16)
		n, err := io.ReadFull(file, header)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return errors.New("WebP image validation failed")
		}
		if n < 16 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
			return errors.New("WebP image validation failed")
		}
		chunk := string(header[12:16])
		if chunk != "VP8 " && chunk != "VP8L" && chunk != "VP8X" {
			return errors.New("WebP image validation failed")
		}
		return nil
	}
	if _, _, err := image.DecodeConfig(file); err != nil {
		return fmt.Errorf("image validation failed: %w", err)
	}
	return nil
}
