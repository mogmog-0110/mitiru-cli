# mitiru-cli

MitiruEngine のプロジェクトを管理する CLI です。`CMakeLists.txt` を編集せず、ゲームの作成、ビルド、実行までを行えます。Cargo や `go run` に近い操作で利用できます。

## インストール

```bash
go install github.com/mogmog-0110/mitiru-cli/cmd/mitiru@latest
```

`mitiru` は `$GOPATH/bin` にインストールされます。デフォルトのパスは `$HOME/go/bin` です。`PATH` が通っていることを確認してください。

```bash
mitiru version
```

## クイックスタート

```bash
mitiru new my-game
cd my-game
mitiru run
```

初回実行時は、エンジン本体を `~/.mitiru/cache/` に取得するため、起動まで1〜2分ほどかかります。2回目以降は差分のみをビルドするため、数秒で起動します。

## コマンド

| コマンド | 内容 |
| --- | --- |
| `mitiru new <name>` | テンプレートから `./<name>/` にプロジェクトを作成 |
| `mitiru build` | `mitiru.toml` を読み込んでビルド。デフォルトは Debug |
| `mitiru test` | `tests/*.cpp` を `cl /std:c++20 /utf-8` で 1 本ずつビルド・実行し、exit code で合否集計。`--filter` / `--release` / `--include` に対応 |
| `mitiru run` | ビルドして実行。stdin、stdout、exit code を転送 |
| `mitiru watch` | ビルドして起動し、`src/` の保存時に state を維持したまま hot reload |
| `mitiru dist` | 配布フォルダを生成。ランタイムを `data/` に分離し、コンソールなしの `<name>.exe` を出力。`--bat` でログ用 `.bat`、`--zip` で zip を追加。`--onefile` で配布物全体を自己展開の単一 exe に畳む (ファイル名は `[dist] exe_name`、既定は `project.name`)。アセットは既定で `assets.mtpak` に埋め込む (`--pack=false` で外す)。`assets/ui/` と 3D モデル・ナビメッシュは pack に入れずバラ置きで残す (engine がファイルから直に読む)。ゲームの読む DLL を調べて VC ランタイムを同梱し、足りない DLL があれば止まる。セーブは `%APPDATA%/<name>/`。pack の前に資産を読ませ、読み込みの cache を配布物に入れる (下の「配布物に入れる cache」、`--no-bake` で飛ばす)。`--check` で配布物を素の環境に近い一時フォルダで headless に動かして確かめ (`--check-script` で入力の台本を流し、`--check-frames` で回すフレーム数を変える。`assets/` の下の `strings.json` の訳の抜けと、同梱の書体に無い字もファイル・言語・キーで並べ、`--strict-i18n` を付けるとそれも失敗にする)、`--debug` で Debug ビルドを配る (自分のテスト機用)。初回は Release のエンジンを `build/dist-out/` に一から作るので数分かかる |
| `mitiru debug` | Debug 構成でビルドし、engine debug helper（`MITIRU_DEBUG=1` / `MITIRU_INSPECTOR=1`）を有効にして実行 |
| `mitiru inspect [pid]` | 実行中の game を別の OS window に表示したツール画面で観察。`--inspectable input\|timetravel`、`--all` に対応 |
| `mitiru replay <file>` | 記録済みの入力を決定論的に再生。`--test` で回帰判定、`--suite <dir>` で `*.mtrr` を一括判定 |
| `mitiru renderer` / `audio` / `input` / `scene` | 各 subsystem を単独で起動 |
| `mitiru ui` | ゲームを窓なしで回し、RML の UI ごと最後のフレームを PNG に撮る (`--frames` / `--input-script` / `--out`) |
| `mitiru capture` | ビルドしたこのプロジェクトの host を窓なしで回し、`--every` フレームごとに PNG を `build/capture/` へ書く (`--frames` / `--script` / `--out`) |
| `mitiru lint` | `assets/ui/main.rml` が引く変数を、C++ の `hud.set("view.x", ...)` と突き合わせる |
| `mitiru bisect` | どのビルドから決定論が壊れたかを二分探索で特定 |
| `mitiru fuzz` | ランダム入力でクラッシュ・非決定・不変条件違反を探す |
| `mitiru ai-playtest` | 状態 API 経由でゲームを自動プレイし、仕様違反を判定 (`--driver sweep\|claude-code`) |
| `mitiru verify` | ウィンドウを出さずにビルド・起動・スクリーンショットを撮り、合否を JSON で出力 |
| `mitiru mcp` | MCP (Model Context Protocol) サーバーを stdio で起動し、AI ツールから状態取得・操作 |
| `mitiru menu` | 対話メニューでコマンドを選ぶ (`mitiru` 引数なしと同じ) |
| `mitiru self-update` | `mitiru` CLI 本体を最新リリースへ更新 |
| `mitiru update` | このプロジェクトが pin する engine バージョンを最新へ更新 |
| `mitiru clean` | `build/` を削除。`--all` でグローバルキャッシュ `~/.mitiru/cache/` も削除 |
| `mitiru doctor` | Go、CMake、コンパイラを確認 |
| `mitiru version` | バージョンを表示 |
| `mitiru`（引数なし） | 番号でコマンドを選ぶ対話メニューを表示 |

`mitiru build`、`mitiru run`、`mitiru capture` には、`--release` または `--config <Debug|Release|RelWithDebInfo>` を指定できます。ビルドツリーは構成ごとに分かれ、Debug は `build/out`、Release は `build/out-release` に置きます。Release で組んでも Debug の DLL と host は残ります。

`mitiru debug` は常に `--config Debug` を使用します。

## テンプレート

`mitiru new -t <template>` で選べる 6 種類。UI はどれも `assets/ui/main.rml` (RmlUi の RML / RCSS) で、C++ の `hud.set("view.x", ...)` が `{{ x }}` に届く。機能の重なりは無く、それぞれ違う API の入口を見せる出発点です。

| テンプレート | 内容 | 使う API | アセット |
| --- | --- | --- | --- |
| `welcome`（既定） | 額装した絵・ロゴ・歩くキャラ・マウス追従・舞う桜 + 右側の操作パネル | `MITIRU_GAME`、`s.sprite`、`in.mouse*` | 画像スプライト複数、効果音 |
| `hello` | 図形・物理（跳ねるボール）・パーティクル・マウス入力を 1 画面で試すショーケース | `MITIRU_GAME`、`s.fillCircle` 等の図形 API、簡易物理 | 画像スプライト、効果音 |
| `clicker` | クリックでカウンタを増やす最小構成 | `MITIRU_GAME`、`in.mousePressed()`、`hud.set` | なし（`s.fillCircle` のみ） |
| `shooter` | 縦スクロール STG。固定タイムラインの敵出現、パワーアップ、ボス戦 | `MITIRU_GAME`、`Pool<T,N>`、`MsgQueue` | なし（場は C++ の図形、HUD は RML） |
| `objects` | クラスと仮想関数で書く場面 + flat POD の進行データ | `MITIRU_GAME_OBJECTS`、`hud.save` / `hud.load` | なし |
| `action3d` | 3D の庭をキャラが歩き、敵 1 体が見つけると経路をたどって追ってきて、構えてから突く | `mitiru/action/` (キャラ、カメラ、当たり判定)、`mitiru/gameai/` (視界、経路、攻撃の時間割) | `assets/level.obj` (描画・当たり判定・ナビメッシュが同じファイルを読む) |

`action3d` は engine 0.35.0 以降で、`[engine] features = ["nav"]` と `[nav] source` が最初から入っている。`objects` 以外は `Game.hpp` の `MITIRU_GAME` マクロ (`struct { update(Input,Hud,dt); draw(Screen&); }`) から始まる最小 API 経由。

## プロジェクト構成

`mitiru new` は次のファイルを生成します。

```text
my-game/
├── mitiru.toml         # プロジェクトマニフェスト
├── .gitignore
├── README.md
├── src/
│   └── main.cpp        # ゲーム本体
└── assets/
    └── ui/main.rml     # 画面 UI (RmlUi の RML / RCSS)。C++ の絵の上に重なる
```

ビルド時には、次のディレクトリとファイルが生成されます。いずれも `.gitignore` に登録されています。

```text
my-game/
├── build/out/          # Debug のビルドツリー（mitiru build が生成）
├── build/out-release/  # Release のビルドツリー（--release のとき）
└── build/cmake/        # 自動生成された CMakeLists.txt（編集不要）
```

## `mitiru.toml`

ゲームのウィンドウサイズ、フォントアトラス、グラフィクス backend を設定します。C++ に直接記述する必要はありません。

```toml
[project]
name = "my-game"
version = "0.1.0"
engine = "0.42.0"       # 取得する MitiruEngine のバージョン（タグまたは "main"）。mitiru new は CLI の既定の版を書く

[window]
title = "my-game"
width = 1280
height = 720
vsync = true

[build]
backend = "auto"        # auto / dx11 / dx12 / vulkan / opengl / webgl2 / null
```

`mitiru build` はこの TOML を読み込み、設定を C++ ヘッダへ埋め込みます。`src/main.cpp` で `mitiru::EngineConfig` の `title`、`windowWidth`、`windowHeight` を設定する必要はありません。

`mitiru build`、`run`、`watch`、`capture` の構成は既定で Debug です。毎フレームの計算が重いゲームは、Debug では最適化されずに 60 fps を割ります。そのときは普段の構成を `[build] config` で変えます。`--config` と `--release` はこれより先に効きます。止めて調べるときは `mitiru debug` を使います。

```toml
[build]
config = "RelWithDebInfo"   # Debug / Release / RelWithDebInfo。RelWithDebInfo は最適化して記号も残す
```

`mitiru run` と `mitiru watch` は、配置した `assets/` を host の `--watch-assets` に渡します。走らせたまま `build/out/<project>/assets` の JSON を書き換えると、ゲームに `asset.reloaded` が届きます (`mitiru watch` は `assets/` の変更をそこへ写します)。切るときは次のように書きます。

```toml
[run]
watch_assets = false
```

古い `mitiru.toml` の `[cef]` は読み捨てます (警告を 1 行出す)。UI は DLL の隣の `assets/ui/main.rml` があれば host が自動で重ねます。

### engine の追加ライブラリ (`[engine]` と `[nav]`)

生成される CMakeLists.txt は、ゲーム DLL に engine 本体 (`mitiru`) だけを link する。ナビメッシュのように engine 本体に入っていない部品を使うときは、`[engine] features` に名前を書く。

```toml
[engine]
features = ["nav"]

[nav]
source = "assets/level.obj"         # .obj / .gltf / .glb
# args = ["--radius", "0.4"]        # mitiru_navbake の option
```

| 名前 | DLL に足すもの | 使う場面 |
| --- | --- | --- |
| `nav` | `mitiru_nav` (Detour) | 焼いた `.navmesh` を読んで経路を引く。群衆 (`NavCrowd`) と動く障害物もここに入る |
| `navbake` | `mitiru_nav_bake` (Recast、`nav` を含む) | DLL の中でメッシュからナビメッシュを焼く |
| `jolt` | なし (engine が Jolt 付きなら本体に入っている) | Jolt を使う DLL で、engine が Jolt 付きかを configure の時点で確かめる |
| `online` | なし (`mitiru_host` に `mitiru_rollback_net` を link し、engine を `-DMITIRU_WITH_GEKKONET=ON` で作る) | オンライン協力プレイ (`mitiru_host --net`、`hud.net*`)。GekkoNet は静的ライブラリで、通信は Windows の Winsock なので、`mitiru dist` の配布物に足す DLL は無い |

知らない名前を書くと `mitiru build` が使える名前の一覧を出して止まる。`crowd` や `fbx` のように間違えやすい名前には、代わりの書き方も出す。FBX の取り込みは engine 本体に入っているので、名前を書かなくても使える。engine がその target を持っていない (取得した engine に submodule が無い) ときは、CMake の configure が直し方を出して止まる。

`[nav] source` を書くと、ビルドの一工程で engine の `mitiru_navbake` がそのメッシュを `.navmesh` に焼き、DLL の隣の同じ相対位置に置く。`assets/level.obj` なら `<DLL のフォルダ>/assets/level.navmesh` になり、ゲームは `"<プロジェクト名>/assets/level.navmesh"` を開く。焼き直すのはメッシュが変わったときだけ。`.navmesh` を読むには Detour が要るので、`[engine] features` に `nav` か `navbake` が無いとエラーにする。

### 焼いた光 (`[lighting]`)

engine 0.38.0 以降は、レベルの間接光 (放射照度と反射のプローブ) をビルドの一工程で焼ける。`source` には `*.lighting.json` のパスか glob を書く。1 つなら文字列、複数なら配列でよい。

```toml
[lighting]
source = "assets/*.lighting.json"   # ["assets/stage.lighting.json", "assets/maps/*.lighting.json"] も可
# args = ["--rays", "512"]          # mitiru_lightbake の option (--threads / --rays)
```

engine の `mitiru_lightbake` が json ごとに `.lighting.bin` を焼き、DLL の隣の同じ相対位置に置く。`assets/stage.lighting.json` なら `<DLL のフォルダ>/assets/stage.lighting.bin` になり、ゲームは `s.lightingBake3D("<プロジェクト名>/assets/stage.lighting.bin")` で読む。焼き直すのは json か、json の `"level"` が指す glTF が変わったときだけ。json の書き方は engine の `docs/LIGHTING_GI.md` にある。glob はビルドのたびに広げ直すので、json を足したら `mitiru build` を回せば焼く対象に入る。何にも当たらない glob、読めない json、見つからない `"level"` は configure の前にエラーにする。

`mitiru dist` の配布物には焼いた `.lighting.bin` がそのまま入る。pack には畳まずバラ置きで残し、配布前の `--bake-caches` で host に一度読ませて、壊れたファイルを配らないようにする。

### 自前の CMakeLists.txt を持つプロジェクト

エンジンをライブラリとして取り込み、自分で exe を作るプロジェクトは `[build] kind = "standalone"` にします。`mitiru` は CMakeLists.txt を生成せず、`source` の CMake をそのまま configure と build して、`target` の exe を起動します。ビルドツリーは `build/` です。

```toml
[project]
name = "desktop_world"

[build]
kind = "standalone"
source = "src"           # CMakeLists.txt のある場所
target = "desktop_world" # ビルドする CMake target。exe の名前でもある
```

`project.engine` は不要です。`mitiru run -- --selftest` のように `--` の後ろの引数は exe に渡ります。`--inspect`、`--console`、`--record`、`mitiru watch`、`mitiru dist` は mitiru_host の機能なので使えません。

## 配布物に入れる cache

engine は 3D の資産を初めて読むときに変換の cache を作る。FBX は `<x>.fbx.glb`、drawModel の世界のモデルは `<x>.clod`、材質と地形のテクスチャは BC 圧縮の `.dds` になり、元のファイルの隣に置かれる。シェーダーのコンパイル結果も残す。遊ぶ側でこれを作らせると、初回の起動が止まり、書けない場所 (Program Files など) に入れたときは毎回作り直しになる。

`mitiru dist` は配布物を組んだあと、pack に畳む前に `data/mitiru_host.exe <game.dll> --bake-caches <一覧>` を窓なしの DX12 で走らせ、これらを配布物の中に作る。シェーダーは `data/shader_cache/` に入り、host はここを読むだけの置き場として使う。`.dds`・`.clod`・`.glb` は pack に入れずバラ置きで残る。

焼く資産の一覧は `assets/bake.txt` があればその行をそのまま使う。書式は 1 行 1 つで、`#` から後は読まない。パスはゲームが読むときと同じ書き方でよい。行の頭に `model:` (glTF / FBX をスキンのモデルとして)、`clod:` (drawModel の世界のモデル) を付けると種類を決められる。

```text
# assets/bake.txt
clod: assets/castle.glb      # drawModel で置く建物
assets/hero.fbx
assets/field.region.json     # 区画の world.json も全部焼く
```

`bake.txt` が無ければ `assets/` を走査する。`.glb` `.gltf` `.fbx` `.vrm` は `model:`、`.obj` と `.clod` は `clod:`、`*.world.json`、`*.region.json`、`*.lighting.bin` はそのまま一覧に入る。engine が作った cache (`.fbx.glb` `.obj.clod` `.dds`) と `assets/ui/` は入れない。glTF を drawModel で置くゲームは、`bake.txt` に `clod:` で書くと世界のモデルの cache まで焼ける。

読めない資産が 1 つでもあれば (host の終了コード 4)、その資産を並べて dist を止める。そのまま配ると遊ぶ側でも読めないからだ。DX12 の GPU が無いなどで host が走れないときは、警告を出して cache 無しで続ける。`--no-bake` を付けると焼き込みを飛ばす。

## ビルドと実行の流れ

```text
mitiru build
  ├─ ./mitiru.toml を解析
  ├─ ~/.mitiru/cache/<engine-version>/ に MitiruEngine がなければ git clone
  ├─ build/cmake/CMakeLists.txt を生成
  │     （FetchContent_Declare で MitiruEngine を OFFLINE 参照）
  ├─ cmake -S build/cmake -B build（初回または設定変更時）
  └─ cmake --build build --config Debug

mitiru run
  └─ mitiru build を再実行 → build/Debug/<name>.exe を起動
```

CMake の操作は `mitiru` が処理します。利用者が `CMakeLists.txt` を編集することなく、ビルドと実行を完了できます。

## リポジトリからのビルド

```bash
git clone https://github.com/mogmog-0110/mitiru-cli.git
cd mitiru-cli
go build -o mitiru.exe ./cmd/mitiru
```

## ライセンス

MIT.
