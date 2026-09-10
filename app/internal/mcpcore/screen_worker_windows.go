//go:build windows

package mcpcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const screenWorkerArgument = "--internal-screen-capture-worker"
const screenWorkerTimeout = 6 * time.Second
const screenWorkerMaxOutput = 96 << 20

var (
	screenCreateMutex  = screenKernel32.NewProc("CreateMutexW")
	screenWaitMutex    = screenKernel32.NewProc("WaitForSingleObject")
	screenReleaseMutex = screenKernel32.NewProc("ReleaseMutex")
	screenSetThreadDPI = screenUser32.NewProc("SetThreadDpiAwarenessContext")
)

type screenWorkerRequest struct {
	Mode      string     `json:"mode"`
	Handle    uint64     `json:"handle"`
	ProcessID uint32     `json:"processId"`
	Bounds    screenRect `json:"bounds"`
}

type screenWorkerResponse struct {
	PNG    []byte     `json:"png,omitempty"`
	Bounds screenRect `json:"bounds"`
	Method string     `json:"method,omitempty"`
	Error  string     `json:"error,omitempty"`
}

// RunScreenCaptureWorker must run before startup cleanup, single-instance
// signaling, flags, OAuth or HTTP listeners in both executables. This is a local
// pipe-only helper, not a new remote tool or an alternative permission policy.
func RunScreenCaptureWorker(args []string, input io.Reader, output io.Writer) (bool, int) {
	if len(args) != 1 || args[0] != screenWorkerArgument {
		return false, 0
	}
	// Even if the parent exits, a stuck native call cannot leave a permanent
	// capture helper behind. This exits only this helper, never the target app.
	watchdog := time.AfterFunc(screenWorkerTimeout+2*time.Second, func() { os.Exit(124) })
	defer watchdog.Stop()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if screenSetThreadDPI.Find() == nil {
		screenSetThreadDPI.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2, helper thread only
	}
	respond := func(result screenWorkerResponse) (bool, int) {
		if err := json.NewEncoder(output).Encode(result); err != nil {
			return true, 1
		}
		return true, 0
	}
	decoder := json.NewDecoder(io.LimitReader(input, 4096))
	decoder.DisallowUnknownFields()
	var request screenWorkerRequest
	if err := decoder.Decode(&request); err != nil {
		return respond(screenWorkerResponse{Error: "invalid capture worker request"})
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return respond(screenWorkerResponse{Error: "unexpected trailing capture worker input"})
	}
	if err := screenValidateWorkerRequest(request); err != nil {
		return respond(screenWorkerResponse{Error: err.Error()})
	}
	hwnd := uintptr(request.Handle)
	bounds := request.Bounds
	if hwnd != 0 {
		if err := screenValidateWindowIdentity(hwnd, request.ProcessID); err != nil {
			return respond(screenWorkerResponse{Error: err.Error()})
		}
		// PrintWindow captures the full native rectangle, not the narrower DWM
		// visible-frame bounds. Resolve it in the DPI-aware worker, not the UI.
		iconic, _, _ := procIsIconic.Call(hwnd)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if iconic != 0 || visible == 0 {
			placement, ok := screenGetWindowPlacement(hwnd)
			if normal, valid := screenPlacementNormalBounds(placement, ok); valid {
				bounds = normal
			}
		} else {
			var r winRect
			if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok != 0 {
				bounds = screenRect{X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}
			}
		}
	}
	frame, err := captureScreenRect(bounds, hwnd)
	if err != nil {
		return respond(screenWorkerResponse{Error: err.Error()})
	}
	if hwnd != 0 {
		if err := screenValidateWindowIdentity(hwnd, request.ProcessID); err != nil {
			return respond(screenWorkerResponse{Error: err.Error()})
		}
	}
	if frame.Image == nil {
		return respond(screenWorkerResponse{Error: "capture produced no image"})
	}
	encoded := screenBoundedBuffer{limit: 64 << 20}
	if err := png.Encode(&encoded, frame.Image); err != nil {
		return respond(screenWorkerResponse{Error: fmt.Sprintf("encode capture: %v", err)})
	}
	return respond(screenWorkerResponse{PNG: encoded.Bytes(), Bounds: frame.Bounds, Method: frame.Method})
}

func screenValidateWorkerRequest(request screenWorkerRequest) error {
	if err := validateScreenRect(request.Bounds); err != nil {
		return err
	}
	switch request.Mode {
	case "window":
		if request.Handle == 0 || uint64(uintptr(request.Handle)) != request.Handle || request.ProcessID == 0 {
			return errors.New("window capture requires the exact HWND and PID")
		}
	case "desktop":
		if request.Handle != 0 || request.ProcessID != 0 {
			return errors.New("desktop capture cannot contain a window target")
		}
	default:
		return errors.New("unsupported capture worker mode")
	}
	return nil
}

func screenValidateWindowIdentity(hwnd uintptr, pid uint32) error {
	if hwnd == 0 || pid == 0 {
		return errors.New("window identity is incomplete")
	}
	valid, _, _ := procIsWindow.Call(hwnd)
	var actualPID uint32
	thread, _, _ := procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&actualPID)))
	if valid == 0 || thread == 0 || actualPID != pid {
		return errors.New("selected window was closed or its HWND/PID identity changed")
	}
	return nil
}

// The mutex spans the *entire* request, including worker exit. It serializes
// updated MCP instances in this login session; it never locks a target UI thread.
// Callers must remain on the same OS thread until the release function returns.
func screenAcquireCaptureLease() (func(), error) {
	name, _ := windows.UTF16PtrFromString("Local\\MCPDevDesk.ScreenCapture.StateNeutral")
	handle, _, err := screenCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("create capture mutex: %v", err)
	}
	state, _, err := screenWaitMutex.Call(handle, 0)
	if state != 0 && state != 0x80 { // WAIT_OBJECT_0 or WAIT_ABANDONED
		procCloseHandle.Call(handle)
		if state == 0x102 {
			return nil, errors.New("another capture is in progress; wait for it to finish")
		}
		return nil, fmt.Errorf("acquire capture mutex: %v", err)
	}
	return func() { screenReleaseMutex.Call(handle); procCloseHandle.Call(handle) }, nil
}

func screenCaptureIsolated(request screenWorkerRequest) (screenCaptureFrame, error) {
	if err := screenValidateWorkerRequest(request); err != nil {
		return screenCaptureFrame{}, err
	}
	if request.Handle != 0 {
		if err := screenValidateWindowIdentity(uintptr(request.Handle), request.ProcessID); err != nil {
			return screenCaptureFrame{}, err
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, err := screenAcquireCaptureLease()
	if err != nil {
		return screenCaptureFrame{}, err
	}
	defer release()
	executable, err := os.Executable()
	if err != nil {
		return screenCaptureFrame{}, err
	}
	input, err := json.Marshal(request)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), screenWorkerTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, screenWorkerArgument)
	command.Stdin = bytes.NewReader(input)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	// No project credentials, OAuth secrets or network tokens go to the helper.
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA"} {
		if value, ok := os.LookupEnv(key); ok {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	raw, err := screenRunCaptureCommand(ctx, command)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	var response screenWorkerResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("invalid capture worker response: %w", err)
	}
	if response.Error != "" {
		return screenCaptureFrame{}, errors.New(response.Error)
	}
	if response.Method == "" || len(response.PNG) == 0 {
		return screenCaptureFrame{}, errors.New("capture worker returned an incomplete frame")
	}
	config, err := png.DecodeConfig(bytes.NewReader(response.PNG))
	if err != nil {
		return screenCaptureFrame{}, fmt.Errorf("invalid capture PNG: %w", err)
	}
	if err := validateScreenRect(screenRect{Width: config.Width, Height: config.Height}); err != nil {
		return screenCaptureFrame{}, err
	}
	if config.Width != response.Bounds.Width || config.Height != response.Bounds.Height {
		return screenCaptureFrame{}, errors.New("capture dimensions do not match worker metadata")
	}
	decoded, err := png.Decode(bytes.NewReader(response.PNG))
	if err != nil {
		return screenCaptureFrame{}, err
	}
	pixels, ok := decoded.(*image.NRGBA)
	if !ok {
		pixels = image.NewNRGBA(decoded.Bounds())
		draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	}
	if request.Handle != 0 {
		if err := screenValidateWindowIdentity(uintptr(request.Handle), request.ProcessID); err != nil {
			return screenCaptureFrame{}, err
		}
	}
	return screenCaptureFrame{Image: pixels, Bounds: response.Bounds, Method: response.Method}, nil
}

func screenRunCaptureCommand(ctx context.Context, command *exec.Cmd) ([]byte, error) {
	output := screenBoundedBuffer{limit: screenWorkerMaxOutput}
	stderr := screenBoundedBuffer{limit: 8192}
	command.Stdout = &output
	command.Stderr = &stderr
	command.WaitDelay = 500 * time.Millisecond
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("capture worker timed out and was terminated; target window was not modified: %w", ctx.Err())
		}
		return nil, fmt.Errorf("capture worker failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return output.Bytes(), nil
}

type screenBoundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *screenBoundedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *screenBoundedBuffer) String() string { return b.buffer.String() }
func (b *screenBoundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("capture output exceeds memory limit")
	}
	return b.buffer.Write(p)
}
