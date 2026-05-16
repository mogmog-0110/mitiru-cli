# mitiru-cli

MitiruEngine プロジェクト管理 CLI。

`CMakeLists.txt` を一切いじらずに MitiruEngine のゲームを作る・ビルドする・動かすためのコマンドラインツールです。Cargo / `go run` のような感覚で使えます。

## 使いかた (Phase 1)

```
mitiru new myGame      # ./myGame/ にプロジェクトを生成
mitiru doctor          # 前提条件 (CMake, VS, Windows SDK) のチェック
mitiru version         # バージョン表示
```

`mitiru build` / `mitiru run` / `mitiru clean` は Phase 2 で実装予定 (現状は stub)。

## ビルド

```
go build -o mitiru.exe ./cmd/mitiru
```

## ライセンス

MIT.
