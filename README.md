# mitiru-cli

MitiruEngine プロジェクト管理 CLI。

`CMakeLists.txt` を一切いじらずに MitiruEngine のゲームを作る・ビルドする・動かすためのコマンドラインツールです。Cargo / `go run` のような感覚で使えます。

## インストール

```bash
go install github.com/mogmog-0110/mitiru-cli/cmd/mitiru@latest
```

`$GOPATH/bin` (デフォルトは `$HOME/go/bin`) に `mitiru` が入ります。`PATH` が通っていることだけ確認:

```bash
mitiru version
```

## さっと触る

```bash
mitiru new my-game
cd my-game
mitiru run
```

これで初プロジェクトが立ち上がります。初回は `~/.mitiru/cache/` にエンジン本体を取りに行くので 1〜2 分くらいかかります。2 回目以降は差分ビルドだけなので秒で立ち上がります。

## コマンド一覧

| コマンド | やること |
|------|------|
| `mitiru new <name>` | テンプレートから新しいプロジェクトを作る (`./<name>/`) |
| `mitiru build` | `mitiru.toml` を読んでビルド (Debug がデフォルト) |
| `mitiru run` | ビルドして実行 (stdin/stdout/exitcode を forward) |
| `mitiru clean` | `build/` を削除。`--all` でグローバルキャッシュ (`~/.mitiru/cache/`) もまとめて削除 |
| `mitiru doctor` | 前提ツール (Go / CMake / コンパイラ) のチェック |
| `mitiru version` | バージョン表示 |

`mitiru build` / `mitiru run` には `--release` か `--config <Debug|Release|RelWithDebInfo>` を渡せます。

## プロジェクト構成

`mitiru new` が生成するのは最小セット:

```
my-game/
├── mitiru.toml         # プロジェクトマニフェスト
├── .gitignore
├── README.md
├── src/
│   └── main.cpp        # ゲーム本体
└── assets/
    └── scene.html      # Mode B (CEF) 用の初期 HTML
```

ビルドすると以下が生やされます (どちらも `.gitignore` 済):

```
my-game/
├── build/              # CMake のビルドツリー (mitiru build が生成)
└── build/cmake/        # 自動生成された CMakeLists.txt (触らない)
```

## mitiru.toml

ゲームのウィンドウサイズ、CEF の初期 URL、グラフィクス backend をここで指定します。C++ 側でハードコードする必要はありません。

```toml
[project]
name = "my-game"
version = "0.1.0"
engine = "0.1.0"        # 引っ張ってくる MitiruEngine のバージョン (タグ or "main")

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

`mitiru build` はこの TOML を読んで C++ ヘッダに焼き込みます。`src/main.cpp` 側では `mitiru::EngineConfig` の `title` / `windowWidth` / `windowHeight` / `cefStartUrl` を書かなくて OK。

## 内部の動き

```
mitiru build
  ├─ ./mitiru.toml を解析
  ├─ ~/.mitiru/cache/<engine-version>/ に MitiruEngine が無ければ git clone
  ├─ build/cmake/CMakeLists.txt を生成
  │     (FetchContent_Declare で MitiruEngine を OFFLINE 参照)
  ├─ cmake -S build/cmake -B build (初回 or 設定変更時)
  └─ cmake --build build --config Debug

mitiru run
  └─ mitiru build を再実行 → build/Debug/<name>.exe を起動
```

CMake は完全に隠蔽されます。`mitiru` 経由で 1 回もユーザーが `CMakeLists.txt` を触らずに完結します。

## 自前でビルドしたいとき

```bash
git clone https://github.com/mogmog-0110/mitiru-cli.git
cd mitiru-cli
go build -o mitiru.exe ./cmd/mitiru
```

## ライセンス

MIT.
