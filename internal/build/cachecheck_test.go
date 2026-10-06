package build

import "testing"

func TestShowIncludesPrefix(t *testing.T) {
	dir := `C:\Temp\p`
	out := "probe.cpp\r\nメモ: インクルード ファイル:  " + dir + `\probe.h` + "\r\n"
	got, ok := showIncludesPrefix(out, dir)
	if !ok || got != "メモ: インクルード ファイル:" {
		t.Fatalf("prefix = %q, %v", got, ok)
	}
	if _, ok := showIncludesPrefix("probe.cpp\r\n", dir); ok {
		t.Error("include の行が無いのに見つかった")
	}
}

func TestCacheServerMatchesCompiler(t *testing.T) {
	dir := `C:\Temp\p`
	utf8 := "メモ: インクルード ファイル:  " + dir + `\probe.h` + "\r\n"
	garbled := ": CN[h t@C:  " + dir + `\probe.h` + "\r\n"
	if !cacheServerMatchesCompiler(utf8, utf8, dir) {
		t.Error("同じ出力が合わない")
	}
	if cacheServerMatchesCompiler(utf8, garbled, dir) {
		t.Error("接頭辞が化けているのに合った")
	}
	if cacheServerMatchesCompiler("", utf8, dir) {
		t.Error("直の cl の出力が空でも合った")
	}
}

func TestBrokenDeps(t *testing.T) {
	out := "a/x.cpp.obj: #deps 0, deps mtime 1 (VALID)\n\n" +
		"a/y.cpp.obj: #deps 2, deps mtime 1 (VALID)\n    C:/h/a.h\n    C:/h/b.h\n\n" +
		"b/r.rc.res: #deps 0, deps mtime 2 (VALID)\n" +
		"CMakeFiles/game.dir/main.cpp.obj: #deps 0, deps mtime 2 (VALID)\n"
	objs := parseObjectDeps(out)
	if len(objs) != 3 {
		t.Fatalf(".rc.res を除いた 3 個のはず: %v", objs)
	}
	// 1 個の空は engine の正常な例外。ゲームの object だけは空なら壊れている
	if got := brokenDeps(objs[:2], "CMakeFiles/game.dir/"); len(got) != 0 {
		t.Errorf("空が 1 個だけで壊れ扱い: %v", got)
	}
	if got := brokenDeps(objs, "CMakeFiles/game.dir/"); len(got) != 2 {
		t.Errorf("半分以上が空なのに拾えない: %v", got)
	}
	one := []objectDeps{{Path: "CMakeFiles/game.dir/main.cpp.obj"}, {Path: "e/a.obj", Count: 5}}
	if got := brokenDeps(one, "CMakeFiles/game.dir/"); len(got) != 1 {
		t.Errorf("ゲームの object の空を拾えない: %v", got)
	}
}

func TestParseCacheDir(t *testing.T) {
	stats := `Cache location                  Local disk: "E:\\sccache"` + "\nBase directories (none)\n"
	if got := parseCacheDir(stats); got != `E:\sccache` {
		t.Errorf("got %q", got)
	}
	if got := parseCacheDir("Cache location  Unknown\n"); got != "" {
		t.Errorf("local disk でないのに %q", got)
	}
}
