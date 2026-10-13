# tadoru

Miller カラム形式でディレクトリを辿り、スクロール可能なプレビューを表示するターミナルファイラー。

## インストール

Go 1.26 以降が必要（古い Go でも GOTOOLCHAIN により自動で取得される）。リポジトリは private なので、git が GitHub に認証できる状態にしておく。

```sh
gh auth setup-git
GOPRIVATE=github.com/TAATHub go install github.com/TAATHub/tadoru@latest
```

バイナリは `$(go env GOPATH)/bin`（通常 `~/go/bin`）に入るので、PATH に含めておく。更新も同じコマンドで行う。

```sh
tadoru [-theme NAME] [dir]
```

## キー操作

| キー | 動作 |
| --- | --- |
| `↑` / `↓`（`k` / `j`） | カーソル移動 |
| `←` / `→`（`h` / `l`） | 親ディレクトリへ / ディレクトリの中へ |
| `/` | 名前で絞り込み（`Enter` 確定 / `Esc` 解除） |
| `r` | Markdown の整形表示 / ソース表示を切り替え |
| `c` | 選択中のファイル・ディレクトリの絶対パスをコピー |
| `o` / `O` | 既定アプリで開く / Finder で表示 |
| `q` | 終了 |

マウスホイールはカーソル位置のカラムに効く（プレビューはホイールでスクロールする）。クリックで選択し、プレビューがディレクトリのときはクリックした項目へ移動する。テキスト・Markdown のプレビューはドラッグで範囲選択でき、離すとクリップボードにコピーされる（`pbcopy`、なければ OSC 52）。ドラッグせずに URL（`http(s)://`）をクリックするとブラウザで開く。隠しファイルは表示しない。

## プレビュー

- テキスト: chroma によるシンタックスハイライト（先頭 1 MiB、ハイライトは 256 KiB まで）
- Markdown: glamour で整形表示（見出し・表・コードブロック等）
  - 1 行で書かれたローカル画像（`![alt](path)` / `<img src="path">`）はその位置に画像として表示する（1 文書 12 枚まで、元の大きさより拡大しない）。リモート画像は取得せずリンク表示のまま
  - ` ```mermaid ` のフローチャート・シーケンス図・ER 図は罫線の図として表示する。それ以外の図はソースを表示する
- 画像: png / jpeg / gif / webp / bmp / tiff。Kitty graphics 対応端末（Ghostty / kitty / WezTerm）では Unicode プレースホルダー方式で高解像度表示し、それ以外は半ブロック文字 + truecolor で描画する。`TADORU_IMAGE=kitty|blocks` で強制できる
- バイナリ: 中身は表示せず、種別（先頭バイトから判定した MIME タイプ）とサイズだけを表示

## テーマ

`-theme NAME` か `TADORU_THEME` で指定する。UI の配色・コードのハイライト・Markdown の配色がまとめて切り替わる。未指定なら Ghostty の `theme` 設定に合わせ、該当がなければ `catppuccin-mocha`。

`catppuccin-mocha` / `dracula` / `gruvbox` / `monokai` / `nord` / `onedark` / `solarized-dark` / `tokyonight`（`Catppuccin Mocha` のような表記揺れも可）
