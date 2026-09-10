//go:build windows

package mcpcore

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--screen-test-worker-sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if handled, code := RunScreenCaptureWorker(os.Args[1:], os.Stdin, os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestScreenCaptureProductionCannotMutateTargetWindows(t *testing.T) {
	forbidden := map[string]bool{}
	for _, name := range []string{"SetWindowRgn", "DwmSetWindowAttribute", "ShowWindow", "ShowWindowAsync", "SetWindowPos", "SetWindowPlacement", "SetForegroundWindow", "SetFocus", "SetActiveWindow", "MoveWindow", "SetWindowLongW", "SetWindowLongPtrW", "SetParent", "BringWindowToTop"} {
		forbidden[name] = true
	}
	paths, err := filepath.Glob("screen*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || fn.Sel.Name != "NewProc" {
				return true
			}
			for _, arg := range call.Args {
				if value, ok := arg.(*ast.BasicLit); ok && value.Kind == token.STRING {
					name, _ := strconv.Unquote(value.Value)
					if forbidden[name] {
						t.Errorf("%s still loads forbidden target mutation %s", path, name)
					}
				}
			}
			return true
		})
	}
}

func TestScreenWorkerRequestValidation(t *testing.T) {
	good := screenWorkerRequest{Mode: "window", Handle: 1, ProcessID: 1, Bounds: screenRect{Width: 320, Height: 200}}
	if err := screenValidateWorkerRequest(good); err != nil {
		t.Fatal(err)
	}
	for _, request := range []screenWorkerRequest{
		{Mode: "window", Bounds: good.Bounds},
		{Mode: "desktop", Handle: 1, Bounds: good.Bounds},
		{Mode: "other", Bounds: good.Bounds},
		{Mode: "desktop", Bounds: screenRect{Width: 1000000000, Height: 1000000000}},
	} {
		if screenValidateWorkerRequest(request) == nil {
			t.Errorf("unsafe request accepted: %+v", request)
		}
	}
}

func TestScreenCaptureWorkerTimeoutIsReaped(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--screen-test-worker-sleep")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	started := time.Now()
	if _, err := screenRunCaptureCommand(ctx, command); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected bounded failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("worker cleanup took %s", elapsed)
	}
	if command.ProcessState == nil {
		t.Fatal("worker was not waited/reaped")
	}
}

func TestScreenCaptureBufferLimit(t *testing.T) {
	b := screenBoundedBuffer{limit: 8}
	if _, err := b.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("9")); err == nil {
		t.Fatal("output limit bypassed")
	}
	b = screenBoundedBuffer{limit: 8}
	if _, err := io.Copy(&b, io.LimitReader(strings.NewReader("123456789"), 9)); err == nil {
		t.Fatal("io.Copy bypassed capture output limit")
	}
}

func TestScreenCaptureLeaseSerializesThreads(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, err := screenAcquireCaptureLease()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, e := screenAcquireCaptureLease()
		if e == nil {
			r()
		}
		result <- e
	}()
	if err := <-result; err == nil {
		t.Fatal("concurrent capture lease was accepted")
	}
}

// These fixtures are our own temporary HWNDs, never a user's application. They
// are tool windows with NOACTIVATE and stay outside the virtual desktop. The
// WM_PRINT handler draws a known pattern; Windows may still clip dormant HWNDs.
var fixtureOnce sync.Once
var fixtureClass *uint16
var fixtureClassError error
var fixtureCallback uintptr
var (
	fixtureRegister     = screenUser32.NewProc("RegisterClassExW")
	fixtureCreate       = screenUser32.NewProc("CreateWindowExW")
	fixtureDef          = screenUser32.NewProc("DefWindowProcW")
	fixtureDestroy      = screenUser32.NewProc("DestroyWindow")
	fixturePost         = screenUser32.NewProc("PostMessageW")
	fixtureQuit         = screenUser32.NewProc("PostQuitMessage")
	fixtureGetMessage   = screenUser32.NewProc("GetMessageW")
	fixtureTranslate    = screenUser32.NewProc("TranslateMessage")
	fixtureDispatch     = screenUser32.NewProc("DispatchMessageW")
	fixtureShow         = screenUser32.NewProc("ShowWindow")
	fixtureCreateRegion = screenGDI32.NewProc("CreateRectRgn")
	fixtureSetRegion    = screenUser32.NewProc("SetWindowRgn")
	fixtureGetRegion    = screenUser32.NewProc("GetWindowRgn")
	fixtureRegionData   = screenGDI32.NewProc("GetRegionData")
	fixtureBrush        = screenGDI32.NewProc("CreateSolidBrush")
	fixtureFill         = screenUser32.NewProc("FillRect")
	fixtureSetPlacement = screenUser32.NewProc("SetWindowPlacement")
)

type fixtureWNDClass struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type fixtureMSG struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          screenPlacementPoint
	Private        uint32
}

func fixturePaint(dc uintptr) {
	r := winRect{0, 0, 320, 200}
	brush, _, _ := fixtureBrush.Call(0x00AA7733)
	fixtureFill.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	procDeleteObject.Call(brush)
	r = winRect{16, 16, 112, 80}
	brush, _, _ = fixtureBrush.Call(0x0000CCFF)
	fixtureFill.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	procDeleteObject.Call(brush)
}
func fixtureRegisterClass() {
	fixtureClass, _ = windows.UTF16PtrFromString("MCPDevDeskCaptureIsolatedFixture")
	fixtureCallback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
		switch msg {
		case 0x317, 0x318, 0x14: // WM_PRINT, WM_PRINTCLIENT, WM_ERASEBKGND
			fixturePaint(wparam)
			return 1
		case 0x10:
			fixtureDestroy.Call(hwnd)
			return 0
		case 0x2:
			fixtureQuit.Call(0)
			return 0
		}
		value, _, _ := fixtureDef.Call(hwnd, uintptr(msg), wparam, lparam)
		return value
	})
	wc := fixtureWNDClass{WndProc: fixtureCallback, ClassName: fixtureClass}
	wc.Size = uint32(unsafe.Sizeof(wc))
	atom, _, err := fixtureRegister.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		fixtureClassError = err
	}
}
func newScreenFixture(t *testing.T, state string) screenWindow {
	t.Helper()
	type result struct {
		hwnd uintptr
		err  error
	}
	ready := make(chan result, 1)
	stopped := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(stopped)
		if screenSetThreadDPI.Find() == nil {
			old, _, _ := screenSetThreadDPI.Call(^uintptr(3))
			if old != 0 {
				defer screenSetThreadDPI.Call(old)
			}
		}
		fixtureOnce.Do(fixtureRegisterClass)
		if fixtureClassError != nil {
			ready <- result{err: fixtureClassError}
			return
		}
		title, _ := windows.UTF16PtrFromString("MCP capture synthetic test")
		x, y := int32(-30000), int32(-30000)
		hwnd, _, err := fixtureCreate.Call(0x08000080, uintptr(unsafe.Pointer(fixtureClass)), uintptr(unsafe.Pointer(title)), 0x80000000, uintptr(x), uintptr(y), 320, 200, 0, 0, 0, 0)
		if hwnd == 0 {
			ready <- result{err: err}
			return
		}
		region, _, _ := fixtureCreateRegion.Call(0, 0, 320, 200)
		if ok, _, _ := fixtureSetRegion.Call(hwnd, region, 0); ok == 0 {
			procDeleteObject.Call(region)
		}
		if state == "minimized" {
			fixtureShow.Call(hwnd, 7)
		}
		if state == "offscreen-normal" {
			fixtureShow.Call(hwnd, 4)
		}
		ready <- result{hwnd: hwnd}
		var msg fixtureMSG
		for {
			code, _, _ := fixtureGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(code) <= 0 {
				break
			}
			fixtureTranslate.Call(uintptr(unsafe.Pointer(&msg)))
			fixtureDispatch.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	got := <-ready
	if got.err != nil || got.hwnd == 0 {
		t.Fatalf("create independent window fixture: %v", got.err)
	}
	t.Cleanup(func() {
		fixturePost.Call(got.hwnd, 0x10, 0, 0)
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("independent fixture did not close")
		}
	})
	return screenWindow{Handle: got.hwnd, ProcessID: uint32(os.Getpid()), Bounds: screenRect{X: -30000, Y: -30000, Width: 320, Height: 200}}
}

type fixtureSnapshot struct {
	Placement                                 screenWindowPlacement
	Rect                                      winRect
	Visible, Iconic, Above, Style, Foreground uintptr
	Region                                    []byte
}

func snapshotScreenFixture(t *testing.T, hwnd uintptr) fixtureSnapshot {
	t.Helper()
	var s fixtureSnapshot
	var ok bool
	s.Placement, ok = screenGetWindowPlacement(hwnd)
	if !ok {
		t.Fatal("fixture GetWindowPlacement failed")
	}
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&s.Rect))); ok == 0 {
		t.Fatal("fixture GetWindowRect failed")
	}
	s.Visible, _, _ = procIsWindowVisible.Call(hwnd)
	s.Iconic, _, _ = procIsIconic.Call(hwnd)
	s.Above, _, _ = procGetWindow.Call(hwnd, gwHwndPrev)
	s.Style, _, _ = procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
	s.Foreground, _, _ = procGetForegroundWindow.Call()
	r, _, _ := fixtureCreateRegion.Call(0, 0, 0, 0)
	defer procDeleteObject.Call(r)
	typ, _, _ := fixtureGetRegion.Call(hwnd, r)
	if typ != 0 {
		n, _, _ := fixtureRegionData.Call(r, 0, 0)
		if n > 4096 {
			t.Fatal("fixture region unexpectedly large")
		}
		if n > 0 {
			s.Region = make([]byte, n)
			fixtureRegionData.Call(r, n, uintptr(unsafe.Pointer(&s.Region[0])))
		}
	}
	return s
}

func TestScreenCaptureFixtureStatePreserved(t *testing.T) {
	for _, state := range []string{"hidden", "minimized", "offscreen-normal"} {
		t.Run(state, func(t *testing.T) {
			target := newScreenFixture(t, state)
			before := snapshotScreenFixture(t, target.Handle)
			for i := 0; i < 3; i++ {
				frame, err := platformCaptureScreenWindowForVision(target)
				after := snapshotScreenFixture(t, target.Handle)
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("capture changed fixture state: before=%+v after=%+v", before, after)
				}
				// Availability and safety are separate assertions. Windows can
				// withhold a dormant surface; that must not trigger a restore.
				if err != nil {
					if state == "offscreen-normal" {
						t.Fatalf("independent normal fixture must be readable: %v", err)
					}
					t.Logf("%s iteration %d: explicit unavailable result; placement/region/state/Z-order/focus unchanged: %v", state, i+1, err)
				} else {
					if frame.Image.Bounds().Dx() != 320 || frame.Image.Bounds().Dy() != 200 {
						t.Fatalf("wrong fixture dimensions: %v", frame.Image.Bounds())
					}
					color := frame.Image.NRGBAAt(200, 100)
					if color.R != 0x33 || color.G != 0x77 || color.B != 0xAA {
						t.Fatalf("fixture pixels are wrong/blank: %v; method=%s", color, frame.Method)
					}
					t.Logf("%s iteration %d: %s; pixels verified; placement/region/state/Z-order/focus unchanged", state, i+1, frame.Method)
				}
				var response uintptr
				// A read must not leave the target UI thread unresponsive.
				ok, _, callErr := screenUser32.NewProc("SendMessageTimeoutW").Call(target.Handle, 0, 0, 0, 0x23, 250, uintptr(unsafe.Pointer(&response)))
				if ok == 0 {
					t.Fatalf("fixture no longer responds after capture: %v", callErr)
				}
			}
		})
	}
}

func TestScreenWindowPlacementRoundTripsOnRealWin32(t *testing.T) {
	target := newScreenFixture(t, "hidden")
	placement, ok := screenGetWindowPlacement(target.Handle)
	if !ok {
		t.Fatal("GetWindowPlacement failed")
	}
	if placement.Length != 44 {
		t.Fatalf("Windows returned length=%d", placement.Length)
	}
	if ok, _, err := fixtureSetPlacement.Call(target.Handle, uintptr(unsafe.Pointer(&placement))); ok == 0 {
		t.Fatalf("44-byte placement roundtrip failed: %v", err)
	}
}

func TestScreenCaptureRejectsWrongPIDWithoutStateChanges(t *testing.T) {
	target := newScreenFixture(t, "hidden")
	before := snapshotScreenFixture(t, target.Handle)
	target.ProcessID++
	if _, err := platformCaptureScreenWindowForVision(target); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	after := snapshotScreenFixture(t, target.Handle)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected capture changed target state")
	}
}

func TestScreenBlankImageDetection(t *testing.T) {
	if !screenImageLikelyPrintWindowArtifact(nil) {
		t.Fatal("nil image accepted")
	}
	var out bytes.Buffer
	handled, code := RunScreenCaptureWorker([]string{"--not-a-worker"}, strings.NewReader(""), &out)
	if handled || code != 0 || out.Len() != 0 {
		t.Fatal("normal invocation entered capture worker")
	}
}
