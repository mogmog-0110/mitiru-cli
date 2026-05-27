package commands

import (
	"runtime"
	"testing"
)

func TestPickAsset(t *testing.T) {
	// 現在の OS/arch 向けに goreleaser が出す名前を組み立てる。
	name := "mitiru_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	assets := []ghAsset{
		{Name: "checksums.txt", URL: "u-checksums"},
		{Name: "installer_windows_amd64.exe", URL: "u-installer"},
		{Name: name, URL: "u-mitiru"},
		{Name: "mitiru_freebsd_riscv64", URL: "u-other"},
	}
	if got := pickAsset(assets); got != "u-mitiru" {
		t.Errorf("pickAsset picked %q; want u-mitiru (asset %q)", got, name)
	}

	// mitiru asset が無ければ "" を返す (installer は拾わない)。
	none := []ghAsset{{Name: "installer_windows_amd64.exe", URL: "x"}}
	if got := pickAsset(none); got != "" {
		t.Errorf("pickAsset with no mitiru asset = %q; want empty", got)
	}
}

func TestPickAssetArchAlias(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("alias test is amd64-specific")
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	// x86_64 別名でもマッチすること。
	assets := []ghAsset{{Name: "mitiru_" + runtime.GOOS + "_x86_64" + ext, URL: "u"}}
	if got := pickAsset(assets); got != "u" {
		t.Errorf("pickAsset did not match x86_64 alias; got %q", got)
	}
}
