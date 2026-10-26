//go:build darwin

package r2oauth

import (
	"reflect"
	"testing"
)

// Resolve the real framework ABI without querying, creating or deleting any
// credential. This also exercises the backend used by MyGo's no-cgo packages.
func TestLoadKeychainFrameworkSymbols(t *testing.T) {
	a, err := loadKeychain()
	if err != nil {
		t.Fatal("load macOS credential frameworks:", err)
	}
	functions := map[string]any{
		"CFDictionaryCreateMutable": a.dictCreate,
		"CFDictionarySetValue":      a.dictSet,
		"CFStringCreateWithCString": a.stringCreate,
		"CFDataCreate":              a.dataCreate,
		"CFDataGetLength":           a.dataLength,
		"CFDataGetBytePtr":          a.dataBytes,
		"CFRelease":                 a.release,
		"SecItemCopyMatching":       a.copyMatching,
		"SecItemUpdate":             a.update,
		"SecItemAdd":                a.add,
		"SecItemDelete":             a.delete,
	}
	for name, function := range functions {
		if reflect.ValueOf(function).IsNil() {
			t.Errorf("missing function %s", name)
		}
	}
	constants := map[string]uintptr{
		"kSecClass":                a.class,
		"kSecClassGenericPassword": a.generic,
		"kSecAttrService":          a.service,
		"kSecAttrAccount":          a.account,
		"kSecReturnData":           a.returnData,
		"kSecMatchLimit":           a.matchLimit,
		"kSecMatchLimitOne":        a.matchOne,
		"kSecValueData":            a.valueData,
		"kSecAttrAccessible":       a.accessible,
		"kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly": a.localUnlocked,
		"kCFBooleanTrue":                  a.booleanTrue,
		"kCFTypeDictionaryKeyCallBacks":   a.keyCallbacks,
		"kCFTypeDictionaryValueCallBacks": a.valueCallbacks,
	}
	for name, value := range constants {
		if value == 0 {
			t.Errorf("missing constant %s", name)
		}
	}
}
