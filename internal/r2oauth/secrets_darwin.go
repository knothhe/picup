//go:build darwin

package r2oauth

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The app's MyGo packaging deliberately builds without cgo. Resolve Security
// and CoreFoundation through the same purego runtime already used by the UI.
type keychainFunctions struct {
	dictCreate                                                                                                            func(uintptr, int, uintptr, uintptr) uintptr
	dictSet                                                                                                               func(uintptr, uintptr, uintptr)
	stringCreate                                                                                                          func(uintptr, string, uint32) uintptr
	dataCreate                                                                                                            func(uintptr, unsafe.Pointer, int) uintptr
	dataLength                                                                                                            func(uintptr) int
	dataBytes                                                                                                             func(uintptr) unsafe.Pointer
	release                                                                                                               func(uintptr)
	copyMatching                                                                                                          func(uintptr, *uintptr) int32
	update                                                                                                                func(uintptr, uintptr) int32
	add                                                                                                                   func(uintptr, *uintptr) int32
	delete                                                                                                                func(uintptr) int32
	class, generic, service, account, returnData, matchLimit, matchOne, valueData, accessible, localUnlocked, booleanTrue uintptr
	keyCallbacks, valueCallbacks                                                                                          uintptr
}

var keychain struct {
	sync.Once
	api keychainFunctions
	err error
}

func systemStoreAvailable() bool { return true }
func loadKeychain() (*keychainFunctions, error) {
	keychain.Do(func() {
		cf, e := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if e != nil {
			keychain.err = ErrSecretUnavailable
			return
		}
		sec, e := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if e != nil {
			keychain.err = ErrSecretUnavailable
			return
		}
		bind := func(lib uintptr, target any, name string) {
			if keychain.err != nil {
				return
			}
			addr, e := purego.Dlsym(lib, name)
			if e != nil {
				keychain.err = ErrSecretUnavailable
				return
			}
			purego.RegisterFunc(target, addr)
		}
		var copyPointer func(*uintptr, uintptr, uintptr) uintptr
		bind(purego.RTLD_DEFAULT, &copyPointer, "memcpy")
		constant := func(lib uintptr, name string) uintptr {
			if keychain.err != nil {
				return 0
			}
			addr, e := purego.Dlsym(lib, name)
			if e != nil {
				keychain.err = ErrSecretUnavailable
				return 0
			}
			var value uintptr
			copyPointer(&value, addr, unsafe.Sizeof(value))
			return value
		}
		address := func(lib uintptr, name string) uintptr {
			p, e := purego.Dlsym(lib, name)
			if e != nil {
				keychain.err = ErrSecretUnavailable
			}
			return p
		}
		a := &keychain.api
		bind(cf, &a.dictCreate, "CFDictionaryCreateMutable")
		bind(cf, &a.dictSet, "CFDictionarySetValue")
		bind(cf, &a.stringCreate, "CFStringCreateWithCString")
		bind(cf, &a.dataCreate, "CFDataCreate")
		bind(cf, &a.dataLength, "CFDataGetLength")
		bind(cf, &a.dataBytes, "CFDataGetBytePtr")
		bind(cf, &a.release, "CFRelease")
		bind(sec, &a.copyMatching, "SecItemCopyMatching")
		bind(sec, &a.update, "SecItemUpdate")
		bind(sec, &a.add, "SecItemAdd")
		bind(sec, &a.delete, "SecItemDelete")
		a.class = constant(sec, "kSecClass")
		a.generic = constant(sec, "kSecClassGenericPassword")
		a.service = constant(sec, "kSecAttrService")
		a.account = constant(sec, "kSecAttrAccount")
		a.returnData = constant(sec, "kSecReturnData")
		a.matchLimit = constant(sec, "kSecMatchLimit")
		a.matchOne = constant(sec, "kSecMatchLimitOne")
		a.valueData = constant(sec, "kSecValueData")
		a.accessible = constant(sec, "kSecAttrAccessible")
		a.localUnlocked = constant(sec, "kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly")
		a.booleanTrue = constant(cf, "kCFBooleanTrue")
		a.keyCallbacks = address(cf, "kCFTypeDictionaryKeyCallBacks")
		a.valueCallbacks = address(cf, "kCFTypeDictionaryValueCallBacks")
	})
	if keychain.err != nil {
		return nil, keychain.err
	}
	return &keychain.api, nil
}

type systemStore struct{ service string }

func newSystemStore(service string) secretStore { return systemStore{service} }
func (a *keychainFunctions) dictionary() uintptr {
	return a.dictCreate(0, 0, a.keyCallbacks, a.valueCallbacks)
}
func (a *keychainFunctions) query(service, account string) uintptr {
	q := a.dictionary()
	s := a.stringCreate(0, service, 0x08000100)
	v := a.stringCreate(0, account, 0x08000100)
	a.dictSet(q, a.class, a.generic)
	a.dictSet(q, a.service, s)
	a.dictSet(q, a.account, v)
	a.release(s)
	a.release(v)
	return q
}
func statusError(status int32) error {
	switch status {
	case 0:
		return nil
	case -25300:
		return errSecretMissing
	case -128, -25293, -25308, -25291:
		return ErrSecretDenied
	}
	return fmt.Errorf("system credential operation failed (%d)", status)
}
func (s systemStore) Get(key string) ([]byte, error) {
	a, e := loadKeychain()
	if e != nil {
		return nil, e
	}
	q := a.query(s.service, key)
	defer a.release(q)
	a.dictSet(q, a.returnData, a.booleanTrue)
	a.dictSet(q, a.matchLimit, a.matchOne)
	var result uintptr
	if e = statusError(a.copyMatching(q, &result)); e != nil {
		return nil, e
	}
	if result == 0 {
		return nil, ErrSecretUnavailable
	}
	defer a.release(result)
	n := a.dataLength(result)
	if n < 0 || n > maxBlobSize {
		return nil, fmt.Errorf("invalid system credential size")
	}
	if n == 0 {
		return []byte{}, nil
	}
	p := a.dataBytes(result)
	if p == nil {
		return nil, ErrSecretUnavailable
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), n)...), nil
}
func (s systemStore) Set(key string, b []byte) error {
	a, e := loadKeychain()
	if e != nil {
		return e
	}
	q := a.query(s.service, key)
	defer a.release(q)
	values := a.dictionary()
	defer a.release(values)
	var p unsafe.Pointer
	if len(b) > 0 {
		p = unsafe.Pointer(&b[0])
	}
	data := a.dataCreate(0, p, len(b))
	runtime.KeepAlive(b)
	if data == 0 {
		return ErrSecretUnavailable
	}
	defer a.release(data)
	a.dictSet(values, a.valueData, data)
	status := a.update(q, values)
	if status == -25300 {
		a.dictSet(q, a.valueData, data)
		a.dictSet(q, a.accessible, a.localUnlocked)
		status = a.add(q, nil)
	}
	return statusError(status)
}
func (s systemStore) Delete(key string) error {
	a, e := loadKeychain()
	if e != nil {
		return e
	}
	q := a.query(s.service, key)
	defer a.release(q)
	e = statusError(a.delete(q))
	if e == errSecretMissing {
		return nil
	}
	return e
}
