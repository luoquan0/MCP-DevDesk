from pathlib import Path
import re

path = Path("app/internal/mcpcore/screen_minimized_windows.go")
text = path.read_text(encoding="utf-8")
old = "\tNormalPosition winRect\n}"
new = "\tNormalPosition winRect\n\tDevicePosition winRect\n}"
if text.count(old) != 1:
    raise RuntimeError("WINDOWPLACEMENT struct matcher failed")
text = text.replace(old, new, 1)
pattern = re.compile(r"func screenRepairRestoredBounds\(hwnd uintptr, placement screenWindowPlacement, placementOK bool\) error \{.*?\n\}\n\nfunc screenRestoreDormantWindow", re.S)
replacement = r'''func screenRepairRestoredBounds(hwnd uintptr, placement screenWindowPlacement, placementOK bool) error {
	normal, normalOK := screenPlacementNormalBounds(placement, placementOK)
	if !normalOK {
		return nil
	}
	current, currentErr := screenWindowRect(hwnd)
	if currentErr == nil && !screenVisionBoundsNeedRepair(current, normal) {
		return nil
	}

	// WINDOWPLACEMENT coordinates are workspace coordinates for normal top-level
	// app windows. Restore them with SetWindowPlacement itself; Microsoft warns
	// against feeding rcNormalPosition directly into SetWindowPos.
	if placementOK {
		repairedPlacement := placement
		repairedPlacement.Length = uint32(unsafe.Sizeof(screenWindowPlacement{}))
		repairedPlacement.ShowCmd = swShowNoActivate
		ok, _, callErr := procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&repairedPlacement)))
		if ok == 0 {
			if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
				return callErr
			}
			return errors.New("SetWindowPlacement failed while restoring normal placement")
		}
		screenFlushDWM()
		time.Sleep(screenRestorePoll)
		current, currentErr = screenWindowRect(hwnd)
		if currentErr == nil && !screenVisionBoundsNeedRepair(current, normal) {
			return nil
		}
	}

	// If an application keeps an abnormal icon-sized surface even after its
	// placement is restored, repair only width/height. SWP_NOMOVE deliberately
	// avoids interpreting workspace coordinates as screen coordinates.
	flags := uintptr(swpNoMove | screenSWPNoZOrder | swpNoActivate | swpNoOwnerZOrder | swpNoSendChanging)
	ok, _, callErr := procSetWindowPos.Call(
		hwnd,
		0,
		0,
		0,
		uintptr(normal.Width),
		uintptr(normal.Height),
		flags,
	)
	if ok == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return callErr
		}
		return errors.New("SetWindowPos failed while restoring normal size")
	}
	screenFlushDWM()
	return nil
}

func screenRestoreDormantWindow'''
text, count = pattern.subn(replacement, text, count=1)
if count != 1:
    raise RuntimeError("screenRepairRestoredBounds matcher failed")
path.write_text(text, encoding="utf-8", newline="\n")

test_path = Path("app/internal/mcpcore/screen_minimized_windows_test.go")
test = test_path.read_text(encoding="utf-8")
if 'import "testing"' not in test:
    raise RuntimeError("test import matcher failed")
test = test.replace('import "testing"', 'import (\n\t"testing"\n\t"unsafe"\n)', 1)
marker = 'func TestScreenVisionPreferredBoundsUsesNormalPlacementForDormantWindow(t *testing.T) {'
abi_test = '''func TestScreenWindowPlacementMatchesWin32Layout(t *testing.T) {\n\tif got := unsafe.Sizeof(screenWindowPlacement{}); got != 60 {\n\t\tt.Fatalf("WINDOWPLACEMENT size = %d, want 60", got)\n\t}\n}\n\n'''
if marker not in test:
    raise RuntimeError("test marker failed")
test = test.replace(marker, abi_test + marker, 1)
test_path.write_text(test, encoding="utf-8", newline="\n")
