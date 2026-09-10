//go:build windows

package mcpcore

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	screenWGCCaptureTimeout               = 1500 * time.Millisecond
	screenWGCPollInterval                 = 12 * time.Millisecond
	screenWGCROInitMultithreaded          = 1
	screenWGCD3DDriverTypeHardware        = 1
	screenWGCD3DDriverTypeWarp            = 5
	screenWGCD3D11CreateDeviceBGRASupport = 0x20
	screenWGCD3D11SDKVersion              = 7
	screenWGCDirectXPixelFormatBGRA8UNorm = 87
	screenWGCD3D11UsageStaging            = 3
	screenWGCD3D11CPUAccessRead           = 0x20000
	screenWGCD3D11MapRead                 = 1
	screenWGCTextureGetDescVTableIndex    = 10
	screenWGCDeviceCreateTexture2DVTable  = 5
	screenWGCContextMapVTableIndex        = 14
	screenWGCContextUnmapVTableIndex      = 15
	screenWGCContextCopyResourceVTable    = 47
	screenWGCIInspectableMethodBase       = 6
	screenWGCCaptureItemSizeVTableIndex   = 7
	screenWGCFramePoolTryNextVTableIndex  = 7
	screenWGCFramePoolCreateSessionVTable = 10
	screenWGCSessionStartVTableIndex      = 6
	screenWGCSessionSetterVTableIndex     = 7
	screenWGCFrameSurfaceVTableIndex      = 6
	screenWGCIClosableCloseVTableIndex    = 6
	screenWGCInteropCreateWindowVTable    = 3
	screenWGCDXGIAccessGetInterfaceVTable = 3
)

var (
	screenCombase = windows.NewLazySystemDLL("combase.dll")
	screenD3D11   = windows.NewLazySystemDLL("d3d11.dll")

	procRoInitialize                         = screenCombase.NewProc("RoInitialize")
	procRoUninitialize                       = screenCombase.NewProc("RoUninitialize")
	procRoGetActivationFactory               = screenCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString                  = screenCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString                  = screenCombase.NewProc("WindowsDeleteString")
	procD3D11CreateDevice                    = screenD3D11.NewProc("D3D11CreateDevice")
	procCreateDirect3D11DeviceFromDXGIDevice = screenD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
)

var (
	screenWGCGraphicsCaptureItemIID = windows.GUID{
		Data1: 0x79C3F95B, Data2: 0x31F7, Data3: 0x4EC2,
		Data4: [8]byte{0xA4, 0x64, 0x63, 0x2E, 0xF5, 0xD3, 0x07, 0x60},
	}
	screenWGCGraphicsCaptureItemInteropIID = windows.GUID{
		Data1: 0x3628E81B, Data2: 0x3CAC, Data3: 0x4C60,
		Data4: [8]byte{0xB7, 0xF4, 0x23, 0xCE, 0x0E, 0x0C, 0x33, 0x56},
	}
	screenWGCFramePoolStatics2IID = windows.GUID{
		Data1: 0x589B103F, Data2: 0x6BBC, Data3: 0x5DF5,
		Data4: [8]byte{0xA9, 0x91, 0x02, 0xE2, 0x8B, 0x3B, 0x66, 0xD5},
	}
	screenWGCSession2IID = windows.GUID{
		Data1: 0x2C39AE40, Data2: 0x7D2E, Data3: 0x5044,
		Data4: [8]byte{0x80, 0x4E, 0x8B, 0x67, 0x99, 0xD4, 0xCF, 0x9E},
	}
	screenWGCSession3IID = windows.GUID{
		Data1: 0xF2CDD966, Data2: 0x22AE, Data3: 0x5EA1,
		Data4: [8]byte{0x95, 0x96, 0x3A, 0x28, 0x93, 0x44, 0xC3, 0xBE},
	}
	screenWGCIDirect3DDeviceIID = windows.GUID{
		Data1: 0xA37624AB, Data2: 0x8D5F, Data3: 0x4650,
		Data4: [8]byte{0x9D, 0x3E, 0x9E, 0xAE, 0x3D, 0x9B, 0xC6, 0x70},
	}
	screenWGCIDXGIDeviceIID = windows.GUID{
		Data1: 0x54EC77FA, Data2: 0x1377, Data3: 0x44E6,
		Data4: [8]byte{0x8C, 0x32, 0x88, 0xFD, 0x5F, 0x44, 0xC8, 0x4C},
	}
	screenWGCIDirect3DDXGIAccessIID = windows.GUID{
		Data1: 0xA9B3D012, Data2: 0x3DF2, Data3: 0x4EE3,
		Data4: [8]byte{0xB8, 0xD1, 0x86, 0x95, 0xF4, 0x57, 0xD3, 0xC1},
	}
	screenWGCID3D11Texture2DIID = windows.GUID{
		Data1: 0x6F15AAF2, Data2: 0xD208, Data3: 0x4E89,
		Data4: [8]byte{0x9A, 0xB4, 0x48, 0x95, 0x35, 0xD3, 0x4F, 0x9C},
	}
	screenWGCIClosableIID = windows.GUID{
		Data1: 0x30D5A829, Data2: 0x7FA4, Data3: 0x4026,
		Data4: [8]byte{0x83, 0xBB, 0xD7, 0x5B, 0xAE, 0x4E, 0xA9, 0x9E},
	}
)

type screenWGCSizeInt32 struct {
	Width  int32
	Height int32
}

type screenWGCSampleDesc struct {
	Count   uint32
	Quality uint32
}

type screenWGCTexture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     screenWGCSampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type screenWGCMappedSubresource struct {
	Data       unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

type screenWGCResult struct {
	frame screenCaptureFrame
	err   error
}

// captureScreenWindowWGC captures a single HWND through Windows.Graphics.Capture
// without moving, activating, showing, minimizing, or otherwise changing the
// target window. It is intentionally one-shot so the capture session exists only
// for the duration of the MCP tool call.
func captureScreenWindowWGC(hwnd uintptr, fallbackBounds screenRect) (screenCaptureFrame, error) {
	if hwnd == 0 {
		return screenCaptureFrame{}, errors.New("Windows Graphics Capture requires a valid window handle")
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return screenCaptureFrame{}, errors.New("Windows Graphics Capture is supported only by the 64-bit MCP DevDesk build")
	}

	result := make(chan screenWGCResult, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		frame, err := captureScreenWindowWGCOnThread(hwnd, fallbackBounds)
		result <- screenWGCResult{frame: frame, err: err}
	}()
	got := <-result
	return got.frame, got.err
}

func captureScreenWindowWGCOnThread(hwnd uintptr, fallbackBounds screenRect) (screenCaptureFrame, error) {
	hr, _, _ := procRoInitialize.Call(screenWGCROInitMultithreaded)
	if screenHRESULTFailed(hr) {
		return screenCaptureFrame{}, screenHRESULTError("RoInitialize", hr)
	}
	defer procRoUninitialize.Call()

	captureItem, err := screenWGCCreateCaptureItem(hwnd)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	defer screenCOMRelease(captureItem)

	var size screenWGCSizeInt32
	hr = screenCOMCall(captureItem, screenWGCCaptureItemSizeVTableIndex, uintptr(unsafe.Pointer(&size)))
	if screenHRESULTFailed(hr) {
		return screenCaptureFrame{}, screenHRESULTError("GraphicsCaptureItem.Size", hr)
	}
	if size.Width <= 0 || size.Height <= 0 {
		return screenCaptureFrame{}, fmt.Errorf("Windows Graphics Capture reported invalid window size %dx%d", size.Width, size.Height)
	}
	if err := validateScreenRect(screenRect{Width: int(size.Width), Height: int(size.Height)}); err != nil {
		return screenCaptureFrame{}, fmt.Errorf("Windows Graphics Capture window size: %w", err)
	}

	d3dDevice, d3dContext, winRTDevice, err := screenWGCCreateD3DDevice()
	if err != nil {
		return screenCaptureFrame{}, err
	}
	defer screenCOMRelease(winRTDevice)
	defer screenCOMRelease(d3dContext)
	defer screenCOMRelease(d3dDevice)

	framePool, err := screenWGCCreateFramePool(winRTDevice, size)
	if err != nil {
		return screenCaptureFrame{}, err
	}
	defer func() {
		screenWGCClose(framePool)
		screenCOMRelease(framePool)
	}()

	var session uintptr
	hr = screenCOMCall(framePool, screenWGCFramePoolCreateSessionVTable, captureItem, uintptr(unsafe.Pointer(&session)))
	if screenHRESULTFailed(hr) || session == 0 {
		if screenHRESULTFailed(hr) {
			return screenCaptureFrame{}, screenHRESULTError("CreateCaptureSession", hr)
		}
		return screenCaptureFrame{}, errors.New("CreateCaptureSession returned no session")
	}
	defer func() {
		screenWGCClose(session)
		screenCOMRelease(session)
	}()

	// These are best-effort presentation controls. Programmatic window capture
	// itself must never depend on them: Windows may deny border suppression on
	// systems where the app has not been granted the borderless capture capability.
	if session2, queryErr := screenCOMQueryInterface(session, screenWGCSession2IID); queryErr == nil {
		_ = screenCOMCall(session2, screenWGCSessionSetterVTableIndex, 0) // IsCursorCaptureEnabled = false
		screenCOMRelease(session2)
	}
	if session3, queryErr := screenCOMQueryInterface(session, screenWGCSession3IID); queryErr == nil {
		_ = screenCOMCall(session3, screenWGCSessionSetterVTableIndex, 0) // IsBorderRequired = false when permitted
		screenCOMRelease(session3)
	}

	hr = screenCOMCall(session, screenWGCSessionStartVTableIndex)
	if screenHRESULTFailed(hr) {
		return screenCaptureFrame{}, screenHRESULTError("GraphicsCaptureSession.StartCapture", hr)
	}

	deadline := time.Now().Add(screenWGCCaptureTimeout)
	var lastHRESULT uintptr
	blankFrames := 0
	for time.Now().Before(deadline) {
		var captureFrame uintptr
		hr = screenCOMCall(framePool, screenWGCFramePoolTryNextVTableIndex, uintptr(unsafe.Pointer(&captureFrame)))
		if !screenHRESULTFailed(hr) && captureFrame != 0 {
			capturedImage, convertErr := screenWGCFrameToNRGBA(captureFrame, d3dDevice, d3dContext)
			screenWGCClose(captureFrame)
			screenCOMRelease(captureFrame)
			if convertErr != nil {
				return screenCaptureFrame{}, convertErr
			}
			if screenImageLikelyPrintWindowArtifact(capturedImage) {
				// A resumed compositor may first produce an empty frame. Release
				// it and wait within the same bounded session; never show a window.
				blankFrames++
				time.Sleep(screenWGCPollInterval)
				continue
			}
			bounds := fallbackBounds
			bounds.Width = capturedImage.Bounds().Dx()
			bounds.Height = capturedImage.Bounds().Dy()
			return screenCaptureFrame{Image: capturedImage, Bounds: bounds, Method: "windows-graphics-capture"}, nil
		}
		if screenHRESULTFailed(hr) {
			lastHRESULT = hr
		}
		time.Sleep(screenWGCPollInterval)
	}
	if lastHRESULT != 0 {
		return screenCaptureFrame{}, fmt.Errorf("Windows Graphics Capture timed out waiting for a frame after HRESULT 0x%08X", uint32(lastHRESULT))
	}
	return screenCaptureFrame{}, fmt.Errorf("Windows Graphics Capture timed out waiting for a usable frame (blank frames: %d)", blankFrames)
}

func screenWGCCreateCaptureItem(hwnd uintptr) (uintptr, error) {
	factory, err := screenWGCRoGetFactory("Windows.Graphics.Capture.GraphicsCaptureItem", screenWGCGraphicsCaptureItemInteropIID)
	if err != nil {
		return 0, fmt.Errorf("get GraphicsCaptureItem interop factory: %w", err)
	}
	defer screenCOMRelease(factory)

	var item uintptr
	hr := screenCOMCall(factory, screenWGCInteropCreateWindowVTable, hwnd, uintptr(unsafe.Pointer(&screenWGCGraphicsCaptureItemIID)), uintptr(unsafe.Pointer(&item)))
	if screenHRESULTFailed(hr) {
		return 0, screenHRESULTError("IGraphicsCaptureItemInterop.CreateForWindow", hr)
	}
	if item == 0 {
		return 0, errors.New("IGraphicsCaptureItemInterop.CreateForWindow returned no capture item")
	}
	return item, nil
}

func screenWGCCreateD3DDevice() (device, context, winRTDevice uintptr, err error) {
	create := func(driverType uintptr) (uintptr, uintptr, uintptr) {
		var rawDevice uintptr
		var rawContext uintptr
		hr, _, _ := procD3D11CreateDevice.Call(
			0,
			driverType,
			0,
			screenWGCD3D11CreateDeviceBGRASupport,
			0,
			0,
			screenWGCD3D11SDKVersion,
			uintptr(unsafe.Pointer(&rawDevice)),
			0,
			uintptr(unsafe.Pointer(&rawContext)),
		)
		return rawDevice, rawContext, hr
	}

	device, context, hr := create(screenWGCD3DDriverTypeHardware)
	if screenHRESULTFailed(hr) || device == 0 || context == 0 {
		screenCOMRelease(context)
		screenCOMRelease(device)
		device, context, hr = create(screenWGCD3DDriverTypeWarp)
	}
	if screenHRESULTFailed(hr) || device == 0 || context == 0 {
		screenCOMRelease(context)
		screenCOMRelease(device)
		if screenHRESULTFailed(hr) {
			return 0, 0, 0, screenHRESULTError("D3D11CreateDevice", hr)
		}
		return 0, 0, 0, errors.New("D3D11CreateDevice returned incomplete device objects")
	}

	dxgiDevice, queryErr := screenCOMQueryInterface(device, screenWGCIDXGIDeviceIID)
	if queryErr != nil {
		screenCOMRelease(context)
		screenCOMRelease(device)
		return 0, 0, 0, fmt.Errorf("query IDXGIDevice: %w", queryErr)
	}
	defer screenCOMRelease(dxgiDevice)

	var inspectable uintptr
	hr, _, _ = procCreateDirect3D11DeviceFromDXGIDevice.Call(dxgiDevice, uintptr(unsafe.Pointer(&inspectable)))
	if screenHRESULTFailed(hr) || inspectable == 0 {
		screenCOMRelease(context)
		screenCOMRelease(device)
		if screenHRESULTFailed(hr) {
			return 0, 0, 0, screenHRESULTError("CreateDirect3D11DeviceFromDXGIDevice", hr)
		}
		return 0, 0, 0, errors.New("CreateDirect3D11DeviceFromDXGIDevice returned no WinRT device")
	}

	winRTDevice, queryErr = screenCOMQueryInterface(inspectable, screenWGCIDirect3DDeviceIID)
	screenCOMRelease(inspectable)
	if queryErr != nil {
		screenCOMRelease(context)
		screenCOMRelease(device)
		return 0, 0, 0, fmt.Errorf("query WinRT IDirect3DDevice: %w", queryErr)
	}
	return device, context, winRTDevice, nil
}

func screenWGCCreateFramePool(winRTDevice uintptr, size screenWGCSizeInt32) (uintptr, error) {
	factory, err := screenWGCRoGetFactory("Windows.Graphics.Capture.Direct3D11CaptureFramePool", screenWGCFramePoolStatics2IID)
	if err != nil {
		return 0, fmt.Errorf("get Direct3D11CaptureFramePool factory: %w", err)
	}
	defer screenCOMRelease(factory)

	var framePool uintptr
	sizeArg := screenWGCSizeArg(size.Width, size.Height)
	hr := screenCOMCall(
		factory,
		screenWGCIInspectableMethodBase,
		winRTDevice,
		screenWGCDirectXPixelFormatBGRA8UNorm,
		1,
		sizeArg,
		uintptr(unsafe.Pointer(&framePool)),
	)
	if screenHRESULTFailed(hr) {
		return 0, screenHRESULTError("Direct3D11CaptureFramePool.CreateFreeThreaded", hr)
	}
	if framePool == 0 {
		return 0, errors.New("Direct3D11CaptureFramePool.CreateFreeThreaded returned no frame pool")
	}
	return framePool, nil
}

func screenWGCFrameToNRGBA(frame, d3dDevice, d3dContext uintptr) (*image.NRGBA, error) {
	var surface uintptr
	hr := screenCOMCall(frame, screenWGCFrameSurfaceVTableIndex, uintptr(unsafe.Pointer(&surface)))
	if screenHRESULTFailed(hr) || surface == 0 {
		if screenHRESULTFailed(hr) {
			return nil, screenHRESULTError("Direct3D11CaptureFrame.Surface", hr)
		}
		return nil, errors.New("Direct3D11CaptureFrame.Surface returned no surface")
	}
	defer screenCOMRelease(surface)

	dxgiAccess, err := screenCOMQueryInterface(surface, screenWGCIDirect3DDXGIAccessIID)
	if err != nil {
		return nil, fmt.Errorf("query IDirect3DDxgiInterfaceAccess: %w", err)
	}
	defer screenCOMRelease(dxgiAccess)

	var sourceTexture uintptr
	hr = screenCOMCall(dxgiAccess, screenWGCDXGIAccessGetInterfaceVTable, uintptr(unsafe.Pointer(&screenWGCID3D11Texture2DIID)), uintptr(unsafe.Pointer(&sourceTexture)))
	if screenHRESULTFailed(hr) || sourceTexture == 0 {
		if screenHRESULTFailed(hr) {
			return nil, screenHRESULTError("IDirect3DDxgiInterfaceAccess.GetInterface", hr)
		}
		return nil, errors.New("IDirect3DDxgiInterfaceAccess returned no D3D11 texture")
	}
	defer screenCOMRelease(sourceTexture)

	var desc screenWGCTexture2DDesc
	_ = screenCOMCall(sourceTexture, screenWGCTextureGetDescVTableIndex, uintptr(unsafe.Pointer(&desc)))
	if desc.Width == 0 || desc.Height == 0 {
		return nil, errors.New("Windows Graphics Capture returned an empty D3D11 texture")
	}
	if err := validateScreenRect(screenRect{Width: int(desc.Width), Height: int(desc.Height)}); err != nil {
		return nil, err
	}

	stagingDesc := desc
	stagingDesc.MipLevels = 1
	stagingDesc.ArraySize = 1
	stagingDesc.SampleDesc.Count = 1
	stagingDesc.SampleDesc.Quality = 0
	stagingDesc.Usage = screenWGCD3D11UsageStaging
	stagingDesc.BindFlags = 0
	stagingDesc.CPUAccessFlags = screenWGCD3D11CPUAccessRead
	stagingDesc.MiscFlags = 0
	var stagingTexture uintptr
	hr = screenCOMCall(d3dDevice, screenWGCDeviceCreateTexture2DVTable, uintptr(unsafe.Pointer(&stagingDesc)), 0, uintptr(unsafe.Pointer(&stagingTexture)))
	if screenHRESULTFailed(hr) || stagingTexture == 0 {
		if screenHRESULTFailed(hr) {
			return nil, screenHRESULTError("ID3D11Device.CreateTexture2D(staging)", hr)
		}
		return nil, errors.New("ID3D11Device.CreateTexture2D returned no staging texture")
	}
	defer screenCOMRelease(stagingTexture)

	_ = screenCOMCall(d3dContext, screenWGCContextCopyResourceVTable, stagingTexture, sourceTexture)
	var mapped screenWGCMappedSubresource
	hr = screenCOMCall(
		d3dContext,
		screenWGCContextMapVTableIndex,
		stagingTexture,
		0,
		screenWGCD3D11MapRead,
		0,
		uintptr(unsafe.Pointer(&mapped)),
	)
	if screenHRESULTFailed(hr) {
		return nil, screenHRESULTError("ID3D11DeviceContext.Map", hr)
	}
	defer screenCOMCall(d3dContext, screenWGCContextUnmapVTableIndex, stagingTexture, 0)
	if mapped.Data == nil || mapped.RowPitch < desc.Width*4 {
		return nil, errors.New("ID3D11DeviceContext.Map returned invalid pixel memory")
	}

	width := int(desc.Width)
	height := int(desc.Height)
	rowPitch := int(mapped.RowPitch)
	source := unsafe.Slice((*byte)(mapped.Data), rowPitch*height)
	captured := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sourceRow := y * rowPitch
		destinationRow := y * captured.Stride
		for x := 0; x < width; x++ {
			s := sourceRow + x*4
			d := destinationRow + x*4
			captured.Pix[d] = source[s+2]
			captured.Pix[d+1] = source[s+1]
			captured.Pix[d+2] = source[s]
			captured.Pix[d+3] = source[s+3]
		}
	}
	return captured, nil
}

func screenWGCRoGetFactory(className string, iid windows.GUID) (uintptr, error) {
	hstring, err := screenWGCCreateHString(className)
	if err != nil {
		return 0, err
	}
	defer procWindowsDeleteString.Call(hstring)

	var factory uintptr
	hr, _, _ := procRoGetActivationFactory.Call(hstring, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory)))
	if screenHRESULTFailed(hr) {
		return 0, screenHRESULTError("RoGetActivationFactory("+className+")", hr)
	}
	if factory == 0 {
		return 0, fmt.Errorf("RoGetActivationFactory(%s) returned no interface", className)
	}
	return factory, nil
}

func screenWGCCreateHString(value string) (uintptr, error) {
	utf16, err := windows.UTF16FromString(value)
	if err != nil {
		return 0, err
	}
	if len(utf16) == 0 {
		return 0, errors.New("cannot create an empty WinRT class name")
	}
	var hstring uintptr
	hr, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&utf16[0])),
		uintptr(len(utf16)-1),
		uintptr(unsafe.Pointer(&hstring)),
	)
	if screenHRESULTFailed(hr) {
		return 0, screenHRESULTError("WindowsCreateString", hr)
	}
	return hstring, nil
}

func screenWGCSizeArg(width, height int32) uintptr {
	return uintptr(uint64(uint32(width)) | uint64(uint32(height))<<32)
}

// screenCOMCall forwards Go pointers encoded as uintptr to native COM methods.
// Retain them on the heap until the syscall completes; otherwise stack growth
// while constructing callArgs can leave native code with stale output pointers.
//
//go:uintptrescapes
func screenCOMCall(object uintptr, index int, args ...uintptr) uintptr {
	if object == 0 {
		return ^uintptr(0)
	}
	function := screenCOMVTableFunction(object, index)
	if function == 0 {
		return ^uintptr(0)
	}
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, object)
	callArgs = append(callArgs, args...)
	result, _, _ := syscall.SyscallN(function, callArgs...)
	return result
}

func screenCOMVTableFunction(object uintptr, index int) uintptr {
	if object == 0 || index < 0 {
		return 0
	}
	vtable := *(*uintptr)(unsafe.Pointer(object))
	if vtable == 0 {
		return 0
	}
	return *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

func screenCOMQueryInterface(object uintptr, iid windows.GUID) (uintptr, error) {
	var result uintptr
	hr := screenCOMCall(object, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&result)))
	if screenHRESULTFailed(hr) {
		return 0, screenHRESULTError("QueryInterface", hr)
	}
	if result == 0 {
		return 0, errors.New("QueryInterface returned no interface")
	}
	return result, nil
}

func screenCOMRelease(object uintptr) {
	if object != 0 {
		_ = screenCOMCall(object, 2)
	}
}

func screenWGCClose(object uintptr) {
	if object == 0 {
		return
	}
	closable, err := screenCOMQueryInterface(object, screenWGCIClosableIID)
	if err != nil {
		return
	}
	_ = screenCOMCall(closable, screenWGCIClosableCloseVTableIndex)
	screenCOMRelease(closable)
}

func screenHRESULTFailed(value uintptr) bool {
	return int32(uint32(value)) < 0
}

func screenHRESULTError(operation string, value uintptr) error {
	return fmt.Errorf("%s failed with HRESULT 0x%08X", operation, uint32(value))
}
