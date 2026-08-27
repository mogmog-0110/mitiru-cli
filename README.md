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
| `mitiru run` | ビルドして実行。stdin、stdout、exit code を転送 |
| `mitiru watch` | ビルドして起動し、`src/` の保存時に state を維持したまま hot reload |
| `mitiru dist` | 配布フォルダを生成。ランタイムを `data/` に分離し、コンソールなしの `<name>.exe` を出力。`--bat` でログ用 `.bat`、`--pack` でアセットを埋め込み、`--zip` で zip を追加。`[cef] enabled=false` の場合は Chromium を同梱しない |
| `mitiru debug` | Debug 構成でビルドし、engine debug helper（`MITIRU_DEBUG=1` / `MITIRU_INSPECTOR=1`）を有効にして実行 |
| `mitiru inspect [pid]` | 実行中の game を別の OS window に表示したツール画面で観察。`--inspectable input\|timetravel`、`--all` に対応 |
| `mitiru replay <file>` | 記録済みの入力を決定論的に再生 |
| `mitiru renderer` / `audio` / `input` / `scene` | 各 subsystem を単独で起動 |
| `mitiru ui` / `lint` | HTML/CSS UI をブラウザで preview / `data-m-*` バインディングを検査 |
| `mitiru clean` | `build/` を削除。`--all` でグローバルキャッシュ `~/.mitiru/cache/` も削除 |
| `mitiru doctor` | Go、CMake、コンパイラを確認 |
| `mitiru version` | バージョンを表示 |
| `mitiru`（引数なし） | 番号でコマンドを選ぶ対話メニューを表示 |

`mitiru build` と `mitiru run` には、`--release` または `--config <Debug|Release|RelWithDebInfo>` を指定できます。

`mitiru debug` は常に `--config Debug` を使用します。

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
    └── scene.html      # Mode B (CEF) 用の初期 HTML
```

ビルド時には、次のディレクトリとファイルが生成されます。いずれも `.gitignore` に登録されています。

```text
my-game/
├── build/              # CMake のビルドツリー（mitiru build が生成）
└── build/cmake/        # 自動生成された CMakeLists.txt（編集不要）
```

## `mitiru.toml`

ゲームのウィンドウサイズ、CEF の初期 URL、グラフィクス backend を設定します。C++ に直接記述する必要はありません。

```toml
[project]
name = "my-game"
version = "0.1.0"
engine = "0.1.0"        # 取得する MitiruEngine のバージョン（タグまたは "main"）

[window]
title = "my-game"
width = 1280
height = 720
vsync = true

[cef]
start_url = "assets/scene.html"
skip_default_font = true

[build]
backend = "auto"        # auto / dx11 / dx12 / vulkan / opengl / webgl2 / null
```

`mitiru build` はこの TOML を読み込み、設定を C++ ヘッダへ埋め込みます。`src/main.cpp` で `mitiru::EngineConfig` の `title`、`windowWidth`、`windowHeight`、`cefStartUrl` を設定する必要はありません。

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
