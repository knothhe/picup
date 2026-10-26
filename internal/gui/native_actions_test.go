package gui

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/config"
)

type fakeNativeHotkeys struct {
	callbacks map[string]func()
	taken     map[string]bool
	removed   []string
}

func newFakeNativeHotkeys() *fakeNativeHotkeys {
	return &fakeNativeHotkeys{callbacks: map[string]func(){}, taken: map[string]bool{}}
}

func (f *fakeNativeHotkeys) controller(invoke func(string)) nativeHotkeys {
	return nativeHotkeys{
		register: func(key string, fn func()) error {
			if f.taken[key] || f.callbacks[key] != nil {
				return errors.New("shortcut is already registered")
			}
			f.callbacks[key] = fn
			return nil
		},
		unregister: func(key string) {
			delete(f.callbacks, key)
			f.removed = append(f.removed, key)
		},
		invoke: invoke,
	}
}

func TestNativeHotkeyConflictRetainsExistingBindings(t *testing.T) {
	f := newFakeNativeHotkeys()
	var invoked []string
	h := f.controller(func(command string) { invoked = append(invoked, command) })
	initial := map[string]string{"CmdOrCtrl+Shift+U": "quick-upload", "CmdOrCtrl+Shift+V": "quick-paste"}
	finish, err := h.prepare(initial)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	f.taken["CmdOrCtrl+Shift+P"] = true
	if _, err := h.prepare(map[string]string{"CmdOrCtrl+Shift+P": "quick-upload", "CmdOrCtrl+Shift+V": "quick-paste"}); err == nil {
		t.Fatal("accepted a hotkey owned by another app")
	}
	if !reflect.DeepEqual(h.routes, initial) {
		t.Fatal("conflict changed existing routes", h.routes)
	}
	f.callbacks["CmdOrCtrl+Shift+U"]()
	if !reflect.DeepEqual(invoked, []string{"quick-upload"}) {
		t.Fatal("old effective hotkey stopped working", invoked)
	}
}

func TestNativeHotkeySaveFailureRollsBackNewRegistrations(t *testing.T) {
	f := newFakeNativeHotkeys()
	h := f.controller(nil)
	finish, err := h.prepare(map[string]string{"CmdOrCtrl+Shift+U": "quick-upload"})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	finish, err = h.prepare(map[string]string{"CmdOrCtrl+Alt+U": "quick-upload"})
	if err != nil {
		t.Fatal(err)
	}
	if f.callbacks["CmdOrCtrl+Shift+U"] == nil || f.callbacks["CmdOrCtrl+Alt+U"] == nil {
		t.Fatal("preparation released the effective hotkey")
	}
	finish(false)
	finish(true) // A late duplicate completion cannot commit a rolled back change.
	if f.callbacks["CmdOrCtrl+Shift+U"] == nil || f.callbacks["CmdOrCtrl+Alt+U"] != nil || h.routes["CmdOrCtrl+Shift+U"] != "quick-upload" {
		t.Fatal("failed save did not restore exactly the old hotkey set", h.routes)
	}
}

func TestNativeHotkeyPartialRegistrationFailureCompensates(t *testing.T) {
	f := newFakeNativeHotkeys()
	h := f.controller(nil)
	f.taken["CmdOrCtrl+Alt+V"] = true
	if _, err := h.prepare(map[string]string{"CmdOrCtrl+Alt+U": "quick-upload", "CmdOrCtrl+Alt+V": "quick-paste"}); err == nil {
		t.Fatal("expected second registration to fail")
	}
	if len(f.callbacks) != 0 || len(h.routes) != 0 || len(h.registered) != 0 {
		t.Fatal("failed batch leaked a registered hotkey")
	}
}

func TestNativeHotkeySwapClearAndShutdown(t *testing.T) {
	f := newFakeNativeHotkeys()
	var invoked []string
	h := f.controller(func(command string) { invoked = append(invoked, command) })
	first, second := "CmdOrCtrl+Shift+U", "CmdOrCtrl+Shift+V"
	finish, err := h.prepare(map[string]string{first: "quick-upload", second: "quick-paste"})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	finish, err = h.prepare(map[string]string{first: "quick-paste", second: "quick-upload"})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	f.callbacks[first]()
	if !reflect.DeepEqual(invoked, []string{"quick-paste"}) {
		t.Fatal("reused registration kept an old callback target", invoked)
	}
	finish, err = h.prepare(map[string]string{second: "quick-upload"})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if f.callbacks[first] != nil || f.callbacks[second] == nil {
		t.Fatal("clearing one binding affected the other")
	}
	late := f.callbacks[second]
	h.stop()
	late()
	if len(f.callbacks) != 0 || len(invoked) != 1 {
		t.Fatal("shutdown leaked a registration or dispatched a late event")
	}
}

func TestNativeHotkeyCanBeCapturedWithoutUnregistering(t *testing.T) {
	f := newFakeNativeHotkeys()
	invoked, captured := 0, ""
	h := f.controller(func(string) { invoked++ })
	h.intercept = func(key string) bool { captured = key; return true }
	finish, err := h.prepare(map[string]string{"CmdOrCtrl+Shift+U": "quick-upload"})
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	f.callbacks["CmdOrCtrl+Shift+U"]()
	if invoked != 0 || captured != "CmdOrCtrl+Shift+U" || len(f.removed) != 0 {
		t.Fatal("recording triggered an upload or released its OS binding")
	}
}

func TestExistingGlobalConflictDoesNotBlockUnrelatedSettings(t *testing.T) {
	f := newFakeNativeHotkeys()
	a := &application{config: config.Default()}
	a.nativeActions = nativeActionsState{
		initialized:      true,
		hotkeys:          f.controller(nil),
		requestedHotkeys: nativeHotkeyBindings(a.config),
		hotkeyErr:        errors.New("another Picup owns the shortcut"),
	}
	for key := range a.nativeActions.requestedHotkeys {
		f.taken[key] = true
	}
	c := a.config
	c.Appearance = "dark"
	finish, err := a.prepareNativeActions(c)
	if err != nil {
		t.Fatal("unchanged global conflict blocked an unrelated preference", err)
	}
	finish(true)
	if a.nativeActions.hotkeyErr == nil || len(f.callbacks) != 0 {
		t.Fatal("unrelated save hid the conflict or registered a taken shortcut")
	}
	a.nativeActions.retryHotkeys = true
	if _, err = a.prepareNativeActions(c); err == nil {
		t.Fatal("an explicit retry skipped the OS registration attempt")
	}
}

func notificationTestApplication() (*application, *[]mygo.NotificationOptions) {
	var shown []mygo.NotificationOptions
	a := &application{config: config.Default()}
	a.nativeActions = nativeActionsState{
		initialized:         true,
		notificationSupport: func() bool { return true },
		showNotification: func(options mygo.NotificationOptions) error {
			shown = append(shown, options)
			return nil
		},
		closeNotification: func() {},
		windowFocused:     func() bool { return false },
		runWorker:         func(fn func()) { fn() },
		runOnMain:         func(fn func()) { fn() },
	}
	return a, &shown
}

func TestBackgroundNotificationsRespectPreferenceAndForeground(t *testing.T) {
	a, shown := notificationTestApplication()
	a.config.NotifyOnComplete = false
	a.backgroundUploadFinished(2, 0, false)
	a.config.NotifyOnComplete = true
	a.nativeActions.windowFocused = func() bool { return true }
	a.backgroundUploadFinished(2, 0, false)
	a.nativeActions.windowFocused = func() bool { return false }
	a.backgroundUploadFinished(0, 0, true)
	if len(*shown) != 0 {
		t.Fatal("notified for disabled, foreground, or empty results")
	}
	a.backgroundUploadFinished(2, 1, false)
	if len(*shown) != 1 || (*shown)[0].ID != uploadNotificationID || (*shown)[0].Body == "" {
		t.Fatal("background completion did not produce an actionable notification", *shown)
	}
}

func TestNotificationDenialStopsAutomaticRequestsUntilUserRetries(t *testing.T) {
	a, _ := notificationTestApplication()
	calls := 0
	a.nativeActions.showNotification = func(mygo.NotificationOptions) error {
		calls++
		return mygo.ErrNotificationsDenied
	}
	a.backgroundUploadFinished(1, 0, false)
	a.backgroundUploadFinished(1, 0, false)
	if calls != 1 || !a.nativeActions.notificationDenied || a.nativeActions.notificationErr == nil {
		t.Fatal("denial did not stop subsequent automatic requests")
	}
	a.nativeActions.showNotification = func(mygo.NotificationOptions) error { calls++; return nil }
	a.retrySystemNotification()
	if calls != 2 || a.nativeActions.notificationDenied || a.nativeActions.notificationErr != nil {
		t.Fatal("explicit retry did not recover notification delivery")
	}
	a.backgroundUploadFinished(1, 0, false)
	if calls != 3 {
		t.Fatal("successful user retry did not restore later completion notifications")
	}
}

func TestDisableNotificationsWhilePermissionIsPending(t *testing.T) {
	a, _ := notificationTestApplication()
	var completion func()
	closed := 0
	a.nativeActions.runOnMain = func(fn func()) { completion = fn }
	a.nativeActions.closeNotification = func() { closed++ }
	a.backgroundUploadFinished(1, 0, false)
	if !a.nativeActions.notificationBusy || completion == nil {
		t.Fatal("notification did not wait for the controlled permission completion")
	}
	a.config.NotifyOnComplete = false
	completion()
	if closed != 1 || a.nativeActions.notificationBusy {
		t.Fatal("late permission result survived a disabled preference")
	}
}

func TestUnsupportedNativeNotificationsDoNotAttemptPermission(t *testing.T) {
	a, shown := notificationTestApplication()
	a.nativeActions.notificationSupport = func() bool { return false }
	a.backgroundUploadFinished(1, 0, false)
	if len(*shown) != 0 || a.nativeActions.notificationBusy {
		t.Fatal("unpackaged execution attempted to show a system notification")
	}
}
