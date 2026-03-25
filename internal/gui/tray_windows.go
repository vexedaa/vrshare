//go:build windows

package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"syscall"
	"unsafe"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/vexedaa/vrshare/assets"
)

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_USER          = 0x0400
	WM_TRAYICON      = WM_USER + 1
	WM_COMMAND       = 0x0111
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205
	WM_DESTROY       = 0x0002

	TPM_BOTTOMALIGN = 0x0020
	TPM_LEFTALIGN   = 0x0000

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800

	IDM_SHOW = 1000
	IDM_QUIT = 1001

	CS_HREDRAW = 0x0002
	CS_VREDRAW = 0x0001

	DIB_RGB_COLORS = 0
	SM_CXSMICON    = 49
	SM_CYSMICON    = 50
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")

	trayUser32               = syscall.NewLazyDLL("user32.dll")
	procRegisterClassEx      = trayUser32.NewProc("RegisterClassExW")
	procCreateWindowEx       = trayUser32.NewProc("CreateWindowExW")
	procDefWindowProc        = trayUser32.NewProc("DefWindowProcW")
	procGetMessage           = trayUser32.NewProc("GetMessageW")
	procTranslateMessage     = trayUser32.NewProc("TranslateMessage")
	procDispatchMessage      = trayUser32.NewProc("DispatchMessageW")
	procPostQuitMessage      = trayUser32.NewProc("PostQuitMessage")
	procCreatePopupMenu      = trayUser32.NewProc("CreatePopupMenu")
	procAppendMenu           = trayUser32.NewProc("AppendMenuW")
	procTrackPopupMenu       = trayUser32.NewProc("TrackPopupMenu")
	procDestroyMenu          = trayUser32.NewProc("DestroyMenu")
	procGetCursorPos         = trayUser32.NewProc("GetCursorPos")
	procSetForegroundWindow  = trayUser32.NewProc("SetForegroundWindow")
	procFindWindow           = trayUser32.NewProc("FindWindowW")
	procSendMessage          = trayUser32.NewProc("SendMessageW")
	procLoadImage            = trayUser32.NewProc("LoadImageW")
	procGetModuleHandle      = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procDestroyIcon          = trayUser32.NewProc("DestroyIcon")
	procCreateIconIndirect   = trayUser32.NewProc("CreateIconIndirect")
	procGetSystemMetricsTray = trayUser32.NewProc("GetSystemMetrics")

	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
)

type NOTIFYICONDATA struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
}

type WNDCLASSEX struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type MSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type POINT struct {
	x, y int32
}

type BITMAPINFOHEADER struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type BITMAPINFO struct {
	bmiHeader BITMAPINFOHEADER
	bmiColors [1]uint32
}

type ICONINFO struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

var trayApp *App
var trayHwnd uintptr
var idleIcon uintptr
var streamingIcon uintptr

// loadIconImage decodes the embedded PNG icon.
func loadIconImage() image.Image {
	img, err := png.Decode(bytes.NewReader(assets.IconPNG))
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, 16, 16))
	}
	return img
}

// tintIcon creates an HICON from the icon image with white pixels tinted.
// For idle: white stays white. For streaming: white becomes tintColor.
func tintIcon(img image.Image, tintR, tintG, tintB uint8, tintWhite bool) uintptr {
	size, _, _ := procGetSystemMetricsTray.Call(SM_CXSMICON)
	if size == 0 {
		size = 16
	}
	s := int(size)

	// Scale and tint the image
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	hdc, _, _ := procCreateCompatibleDC.Call(0)

	bmi := BITMAPINFO{
		bmiHeader: BITMAPINFOHEADER{
			biSize:     uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			biWidth:    int32(s),
			biHeight:   -int32(s), // top-down
			biPlanes:   1,
			biBitCount: 32,
		},
	}

	var colorBits uintptr
	hbmColor, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&colorBits)), 0, 0)

	var maskBits uintptr
	hbmMask, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&maskBits)), 0, 0)

	colorPixels := unsafe.Slice((*[4]byte)(unsafe.Pointer(colorBits)), s*s)
	maskPixels := unsafe.Slice((*uint32)(unsafe.Pointer(maskBits)), s*s)

	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			// Sample from source with nearest-neighbor scaling
			sx := x * srcW / s + bounds.Min.X
			sy := y * srcH / s + bounds.Min.Y
			r, g, b, a := img.At(sx, sy).RGBA()
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)
			a8 := uint8(a >> 8)

			// Tint white pixels: if pixel is bright (close to white), apply tint
			if tintWhite && a8 > 128 {
				brightness := (int(r8) + int(g8) + int(b8)) / 3
				if brightness > 180 {
					// Scale tint by brightness ratio
					scale := float64(brightness) / 255.0
					r8 = uint8(float64(tintR) * scale)
					g8 = uint8(float64(tintG) * scale)
					b8 = uint8(float64(tintB) * scale)
				}
			}

			idx := y*s + x
			// BGRA format for Windows DIB
			colorPixels[idx] = [4]byte{b8, g8, r8, a8}

			if a8 > 128 {
				maskPixels[idx] = 0x00000000 // opaque
			} else {
				maskPixels[idx] = 0xFFFFFFFF // transparent
			}
		}
	}

	procDeleteDC.Call(hdc)

	ii := ICONINFO{
		fIcon:    1,
		hbmMask:  hbmMask,
		hbmColor: hbmColor,
	}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	procDeleteObject.Call(hbmColor)
	procDeleteObject.Call(hbmMask)

	return icon
}

// setWindowIcon sets the Wails window icon from the exe's embedded resource.
func (a *App) setWindowIcon() {
	const WM_SETICON = 0x0080
	const ICON_SMALL = 0
	const ICON_BIG = 1
	const IMAGE_ICON = 1
	const LR_DEFAULTSIZE = 0x00000040

	// Load icon from the exe's embedded resource (put there by .syso)
	hModule, _, _ := procGetModuleHandle.Call(0)
	// Resource ID "APP" maps to ordinal 1 in go-winres
	smallIcon, _, _ := procLoadImage.Call(hModule, 1, IMAGE_ICON, 16, 16, LR_DEFAULTSIZE)
	bigIcon, _, _ := procLoadImage.Call(hModule, 1, IMAGE_ICON, 32, 32, LR_DEFAULTSIZE)

	// Find the Wails window by title
	title, _ := syscall.UTF16PtrFromString("VRShare")
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		if smallIcon != 0 {
			procSendMessage.Call(hwnd, WM_SETICON, ICON_SMALL, smallIcon)
		}
		if bigIcon != 0 {
			procSendMessage.Call(hwnd, WM_SETICON, ICON_BIG, bigIcon)
		}
	}
}

func (a *App) setupTray() {
	trayApp = a

	// Set the Wails window icon from the exe resource
	a.setWindowIcon()

	// Pre-create tinted icons
	img := loadIconImage()
	idleIcon = tintIcon(img, 255, 255, 255, false)     // no tint - original colors
	streamingIcon = tintIcon(img, 255, 60, 60, true)    // red tint on white pixels

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		className, _ := syscall.UTF16PtrFromString("VRShareTray")
		windowName, _ := syscall.UTF16PtrFromString("VRShare Tray")

		wc := WNDCLASSEX{
			cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
			style:         CS_HREDRAW | CS_VREDRAW,
			lpfnWndProc:   syscall.NewCallback(trayWndProc),
			lpszClassName: className,
		}
		procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

		hwnd, _, _ := procCreateWindowEx.Call(
			0,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(windowName)),
			0, 0, 0, 0, 0,
			0, 0, 0, 0,
		)
		trayHwnd = hwnd

		nid := NOTIFYICONDATA{
			cbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
			hWnd:             hwnd,
			uID:              1,
			uFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
			uCallbackMessage: WM_TRAYICON,
			hIcon:            idleIcon,
		}
		tip, _ := syscall.UTF16FromString("VRShare")
		copy(nid.szTip[:], tip)
		procShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))

		var msg MSG
		for {
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if ret == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
}

// UpdateTrayIcon switches the tray icon based on streaming state.
func (a *App) UpdateTrayIcon(streaming bool) {
	if trayHwnd == 0 {
		return
	}
	icon := idleIcon
	tipText := "VRShare"
	if streaming {
		icon = streamingIcon
		tipText = "VRShare - Streaming"
	}
	nid := NOTIFYICONDATA{
		cbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
		hWnd:   trayHwnd,
		uID:    1,
		uFlags: NIF_ICON | NIF_TIP,
		hIcon:  icon,
	}
	tip, _ := syscall.UTF16FromString(tipText)
	copy(nid.szTip[:], tip)
	procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
}

func (a *App) removeTray() {
	if trayHwnd == 0 {
		return
	}
	nid := NOTIFYICONDATA{
		cbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
		hWnd:   trayHwnd,
		uID:    1,
	}
	procShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	if idleIcon != 0 {
		procDestroyIcon.Call(idleIcon)
	}
	if streamingIcon != 0 {
		procDestroyIcon.Call(streamingIcon)
	}
}

func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONDBLCLK:
			if trayApp != nil {
				trayApp.ShowWindow()
			}
		case WM_RBUTTONUP:
			showTrayMenu(hwnd)
		}
		return 0
	case WM_COMMAND:
		switch wParam {
		case IDM_SHOW:
			if trayApp != nil {
				trayApp.ShowWindow()
			}
		case IDM_QUIT:
			if trayApp != nil {
				trayApp.quitting = true
				trayApp.removeTray()
				if trayApp.srv != nil {
					trayApp.srv.Stop()
				}
				wailsRuntime.Quit(trayApp.ctx)
			}
		}
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	showStr, _ := syscall.UTF16PtrFromString("Show VRShare")
	quitStr, _ := syscall.UTF16PtrFromString("Quit")

	procAppendMenu.Call(menu, MF_STRING, IDM_SHOW, uintptr(unsafe.Pointer(showStr)))
	procAppendMenu.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenu.Call(menu, MF_STRING, IDM_QUIT, uintptr(unsafe.Pointer(quitStr)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	procTrackPopupMenu.Call(menu, TPM_LEFTALIGN|TPM_BOTTOMALIGN, uintptr(pt.x), uintptr(pt.y), 0, hwnd, 0)
	procDestroyMenu.Call(menu)
}

// Ensure color import is used
var _ color.Color
