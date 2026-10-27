package winhello

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinRT from pure Go: activation and strings come from combase.dll, and
// methods are called through each object's vtable. Slots 0-2 are IUnknown's
// (QueryInterface, AddRef, Release) and 3-5 IInspectable's, so an interface's
// own methods start at 6.

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoUninitialize         = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
	procIsWindow               = windows.NewLazySystemDLL("user32.dll").NewProc("IsWindow")
)

const (
	roInitMultithreaded = 1
	rpcEChangedMode     = 0x80010106

	slotQueryInterface = 0
	slotRelease        = 2
	slotFirstMethod    = 6

	// IAsyncInfo.get_Status, get_ErrorCode and Cancel; IAsyncOperation<T>.GetResults.
	slotAsyncStatus     = 7
	slotAsyncErrorCode  = 8
	slotAsyncCancel     = 9
	slotAsyncGetResults = 8

	asyncStarted   = 0
	asyncCompleted = 1
	asyncCanceled  = 2
)

// How long a prompt may stay open: the user may be away from the keyboard.
const (
	checkTimeout  = 10 * time.Second
	promptTimeout = 3 * time.Minute
)

type object struct {
	vtbl *[64]uintptr
}

// call calls method slot of o. Callers pass out-pointers converted to uintptr
// in the call itself; uintptrescapes keeps what they point to on the heap and
// alive for the call, as for LazyProc.Call (a moved stack would leave Windows
// writing to memory the variable no longer has).
//
//go:uintptrescapes
func (o *object) call(slot int, args ...uintptr) error {
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return hresult("WinRT call", hr)
}

func (o *object) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[slotRelease], uintptr(unsafe.Pointer(o)))
	}
}

func hresult(what string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s: HRESULT %#08x", what, uint32(hr))
	}
	return nil
}

func guid(s string) *windows.GUID {
	g, err := windows.GUIDFromString("{" + s + "}")
	if err != nil {
		panic(err)
	}
	return &g
}

type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, hresult("WindowsCreateString", r)
}

func (h hstring) delete() {
	procWindowsDeleteString.Call(uintptr(h))
}

// onWinRTThread runs f on one OS thread in the multithreaded apartment, as
// WinRT calls need.
func onWinRTThread(f func() error) error {
	if err := combase.Load(); err != nil {
		return ErrUnsupported
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procRoInitialize.Call(roInitMultithreaded)
	if int32(r) >= 0 {
		defer procRoUninitialize.Call()
	} else if uint32(r) != rpcEChangedMode {
		return hresult("RoInitialize", r)
	}
	return f()
}

// factory is the UserConsentVerifier activation factory's interface iid.
func factory(iid string) (*object, error) {
	class, err := newHString(className)
	if err != nil {
		return nil, err
	}
	defer class.delete()
	var f *object
	r, _, _ := procRoGetActivationFactory.Call(uintptr(class), uintptr(unsafe.Pointer(guid(iid))), uintptr(unsafe.Pointer(&f)))
	if err := hresult("RoGetActivationFactory", r); err != nil {
		// Interop for windowed apps came with Windows 11.
		return nil, fmt.Errorf("%w (%v)", ErrUnsupported, err)
	}
	return f, nil
}

// await waits for the async operation op to finish within limit, or until
// ctx is done, and stores its enum result in out. A prompt still open when
// its caller has gone or the server stops is cancelled.
func await(ctx context.Context, op *object, out *int32, limit time.Duration) error {
	var info *object
	if err := op.call(slotQueryInterface, uintptr(unsafe.Pointer(guid(iidAsyncInfo))), uintptr(unsafe.Pointer(&info))); err != nil {
		return err
	}
	defer info.release()
	deadline := time.Now().Add(limit)
	for {
		var status int32
		if err := info.call(slotAsyncStatus, uintptr(unsafe.Pointer(&status))); err != nil {
			return err
		}
		switch status {
		case asyncStarted:
			if ctx.Err() != nil {
				info.call(slotAsyncCancel)
				return ctx.Err()
			}
			if time.Now().After(deadline) {
				info.call(slotAsyncCancel)
				return errors.New("Windows Hello didn't answer in time")
			}
			time.Sleep(50 * time.Millisecond)
		case asyncCompleted:
			return op.call(slotAsyncGetResults, uintptr(unsafe.Pointer(out)))
		case asyncCanceled:
			return errors.New("the Windows Hello request was cancelled")
		default:
			var code int32
			info.call(slotAsyncErrorCode, uintptr(unsafe.Pointer(&code)))
			return hresult("Windows Hello", uintptr(uint32(code)))
		}
	}
}

// Available reports why Windows Hello can't approve a request now, or nil.
func (Hello) Available() error {
	return onWinRTThread(func() error {
		// The prompt over a window needs the interop interface, which came
		// with Windows 11; Windows 10 has only the statics.
		interop, err := factory(iidInterop)
		if err != nil {
			return err
		}
		interop.release()
		statics, err := factory(iidStatics)
		if err != nil {
			return err
		}
		defer statics.release()
		var op *object
		if err := statics.call(slotFirstMethod, uintptr(unsafe.Pointer(&op))); err != nil { // CheckAvailabilityAsync
			return err
		}
		defer op.release()
		availability := int32(-1) // unknown, should Windows not write it
		if err := await(context.Background(), op, &availability, checkTimeout); err != nil {
			return err
		}
		return availabilityError(availability)
	})
}

// Approve shows message in a Windows Hello prompt over window (a window
// handle; anything that isn't a window counts as none) and reports whether
// the user approved it. When ctx is done first, the prompt is cancelled.
func (Hello) Approve(ctx context.Context, window uint64, message string) (approved bool, err error) {
	if r, _, _ := procIsWindow.Call(uintptr(window)); r == 0 {
		window = 0
	}
	err = onWinRTThread(func() error {
		interop, err := factory(iidInterop)
		if err != nil {
			return err
		}
		defer interop.release()
		msg, err := newHString(message)
		if err != nil {
			return err
		}
		defer msg.delete()
		var op *object
		// RequestVerificationForWindowAsync(HWND, HSTRING, REFIID, void**)
		if err := interop.call(slotFirstMethod, uintptr(window), uintptr(msg),
			uintptr(unsafe.Pointer(guid(iidAsyncResult))), uintptr(unsafe.Pointer(&op))); err != nil {
			return err
		}
		defer op.release()
		// Not verified (0) unless Windows says so: an answer that never
		// arrives must not read as approval.
		result := int32(-1)
		if err := await(ctx, op, &result, promptTimeout); err != nil {
			return err
		}
		approved, err = resultOf(result)
		return err
	})
	return approved, err
}
