//go:build darwin

package devmaintenance

import (
	"errors"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/buildidentity"
)

var framework struct {
	sync.Once
	err error
}

func loadFrameworks() error {
	framework.Do(func() {
		for _, name := range []string{"Foundation", "ServiceManagement"} {
			if _, err := purego.Dlopen("/System/Library/Frameworks/"+name+".framework/"+name, purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
				framework.err = err
				return
			}
		}
	})
	return framework.err
}
func send(object objc.ID, selector string, arguments ...any) objc.ID {
	return object.Send(objc.RegisterName(selector), arguments...)
}
func nsString(value string) objc.ID {
	return send(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", value)
}
func goString(value objc.ID) string {
	if value == 0 {
		return ""
	}
	return objc.Send[string](value, objc.RegisterName("UTF8String"))
}

func verifyOwnBundle(executable string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := loadFrameworks(); err != nil {
		return err
	}
	pool := send(objc.ID(objc.GetClass("NSAutoreleasePool")), "new")
	defer send(pool, "drain")
	bundle := send(objc.ID(objc.GetClass("NSBundle")), "mainBundle")
	app, err := filepath.EvalSymlinks(goString(send(bundle, "bundlePath")))
	if err != nil {
		return err
	}
	expected := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if app != expected || filepath.Base(app) != "Picup Dev.app" || filepath.Base(executable) != "Picup Dev" ||
		goString(send(bundle, "bundleIdentifier")) != buildidentity.DevelopmentIdentifier ||
		goString(send(bundle, "objectForInfoDictionaryKey:", nsString("PicupDevMaintenanceProtocol"))) != "1" {
		return errors.New("maintenance target is not this exact packaged Picup Dev identity")
	}
	return nil
}

func loginStatus() (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := loadFrameworks(); err != nil {
		return 0, err
	}
	pool := send(objc.ID(objc.GetClass("NSAutoreleasePool")), "new")
	defer send(pool, "drain")
	class := objc.GetClass("SMAppService")
	if class == 0 {
		return 0, errors.New("Dev maintenance requires macOS 13 SMAppService")
	}
	service := send(objc.ID(class), "mainAppService")
	if service == 0 {
		return 0, errors.New("cannot inspect this application's login item")
	}
	return objc.Send[int](service, objc.RegisterName("status")), nil
}

func unregisterLogin() error {
	var operationErr error
	mygo.App.SetName(buildidentity.Name())
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyProhibited)
	mygo.App.WhenReady(func() { operationErr = mygo.App.SetOpenAtLogin(false); mygo.App.Quit() })
	runErr := mygo.App.Run()
	return errors.Join(runErr, operationErr)
}
