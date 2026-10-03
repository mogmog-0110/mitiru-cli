package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/mogmog-0110/mitiru-cli/internal/console"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// configureStampFile は、最後に通った configure の入力の hash を out dir に置くファイル名。
// 中身が今の入力の hash と同じなら cmake の configure を飛ばす。CMakeLists.txt や
// 取り込んだ .cmake の書き換えは、cmake が build.ninja に書いた再生成の規則で
// ninja が自分で拾う。ここで見るのは、ninja からは見えない入力だけでよい
// (cmake に -D で渡す値、generator、toolchain、mitiru.toml)。
const configureStampFile = "mitiru_configure.stamp"

// configureInputs は configure の結果を変えうるものを並べる。どれか 1 つでも
// 変われば hash が変わる。
type configureInputs struct {
	Command     string // cmake -S ... -B ... -G ... -D... の行そのもの
	SrcDir      string // 生成した (または standalone の) CMakeLists.txt のある所
	ManifestDir string // mitiru.toml のある所 (= project root)
	Vcvars      string
}

// configureKey は入力から hash を作る。読めないファイルは「無い」として hash に入れ、
// 次に現れたときに configure し直す。
func configureKey(in configureInputs) string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			io.WriteString(h, p)
			h.Write([]byte{0})
		}
	}
	write("mitiru-configure-v1", in.Command, in.Vcvars)
	write(toolsetNames(in.Vcvars)...)
	hashFileInto(h, filepath.Join(in.SrcDir, "CMakeLists.txt"))
	hashFileInto(h, filepath.Join(in.ManifestDir, "mitiru.toml"))
	return hex.EncodeToString(h.Sum(nil))
}

func hashFileInto(w io.Writer, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		io.WriteString(w, "missing:"+path)
		return
	}
	w.Write(data)
}

// toolsetNames は vcvars64.bat から見た VC\Tools\MSVC の下の版の名前を返す。VS の更新で
// コンパイラの版が入れ替わると、CMakeCache.txt に焼いた cl.exe のパスが消えるので、
// そのときは configure し直す。
func toolsetNames(vcvars string) []string {
	if vcvars == "" {
		return nil
	}
	// <VS>\VC\Auxiliary\Build\vcvars64.bat → <VS>\VC\Tools\MSVC
	msvc := filepath.Join(filepath.Dir(vcvars), "..", "..", "Tools", "MSVC")
	entries, err := os.ReadDir(msvc)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// configureUpToDate は outDir が前に同じ入力で configure を通っていれば true。
// CMakeCache.txt が無いとき (消した、途中で失敗した) は false。
func configureUpToDate(outDir, key string) bool {
	if _, err := os.Stat(filepath.Join(outDir, "CMakeCache.txt")); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(outDir, configureStampFile))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == key
}

func writeConfigureStamp(outDir, key string) error {
	return os.WriteFile(filepath.Join(outDir, configureStampFile), []byte(key+"\n"), 0o644)
}

// clearConfigureStamp は configure を走らせる前に呼ぶ。途中で失敗したときに、
// 古い印が残って次の build が configure を飛ばすことの無いようにする。
func clearConfigureStamp(outDir string) {
	_ = os.Remove(filepath.Join(outDir, configureStampFile))
}

// writeFileIfChanged は中身が同じなら書かない。生成した CMakeLists.txt の更新時刻が
// 変わると、ninja は毎回 cmake を走らせ直す。
func writeFileIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

// configureIfNeeded は入力が前回と同じなら cmake の configure を飛ばす。
func configureIfNeeded(vcvars, generator, srcDir, outDir string, opts Options, timer *phaseTimer) error {
	key := configureKey(configureInputs{
		Command:     configureCommand(generator, srcDir, outDir, opts),
		SrcDir:      srcDir,
		ManifestDir: opts.ProjectRoot,
		Vcvars:      vcvars,
	})
	dryRun := os.Getenv("MITIRU_DRY_RUN") == "1"
	if !dryRun && configureUpToDate(outDir, key) {
		timer.mark("configure (skipped)")
		return nil
	}
	console.Fverbosef(opts.Stdout, "Configuring %s (%s)...\n", opts.ProjectName, opts.Config)
	clearConfigureStamp(outDir)
	if err := runCMakeConfigure(vcvars, generator, srcDir, outDir, opts); err != nil {
		return err
	}
	timer.mark("configure")
	if dryRun {
		return nil
	}
	if err := writeConfigureStamp(outDir, key); err != nil {
		return fmt.Errorf("write %s: %w", configureStampFile, err)
	}
	return nil
}
