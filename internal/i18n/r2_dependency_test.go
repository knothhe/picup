package i18n

import "testing"

func TestR2DependencyPurposeTranslation(t *testing.T) {
	const source = "R2 授权与连接管理"
	if Text(English, source) != "R2 authorization and connection management" || Text(Chinese, source) != source {
		t.Fatal("R2 SDK purpose is not localized")
	}
}
