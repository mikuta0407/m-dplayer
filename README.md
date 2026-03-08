# mtalker

Discord 向けの URL ベース音楽再生 Bot です。`/dplay` で直接音声ファイルリンクまたは `yt-dlp` 対応 URL を再生し、ギルド単位でキュー管理します。

> 現在のローカル実行手順は新しい音楽 Bot 仕様に追従しています。Docker / compose まわりは次の更新段階で整理予定です。

## 主な機能

- `/dplay <url>` で再生開始またはキュー追加
- `/dstop` / `/dnext` で現在曲を停止して次へ進行
- `/dterm` で現在曲停止 + キュー全破棄 + ボイス切断
- `/dqueue` で現在曲と待機キューをエフェメラル表示
- `/dvol <0-10>` でセッション音量を変更
- キューが空になったら自動でボイスチャンネルから切断
- 直接音声ファイルリンクと `yt-dlp` 対応 URL を同じフローで再生

## コマンド

| コマンド | 説明 |
| --- | --- |
| `/dplay <url>` | URL を再生開始するか、既存セッションのキューへ追加します |
| `/dstop` | 現在再生中の曲を停止し、次の曲へ進めます |
| `/dterm` | 現在の曲を停止し、キューを全破棄してボイス接続を切断します |
| `/dnext` | `/dstop` と同様に次の曲へ進めます |
| `/dqueue` | 現在の曲と待機キューをエフェメラルで表示します |
| `/dvol <0-10>` | セッション音量を 0-10 で変更します。10 が等倍です |

### コマンド仕様メモ

- `/dplay` はサーバー内でのみ利用できます
- セッション未作成時の `/dplay` は実行者の VC に接続して再生を開始します
- 既存セッションが同一 VC にある場合の `/dplay` はキュー追加として扱います
- 既存セッションが別 VC にある場合の `/dplay` は拒否します
- キュー追加時は `Queued: [タイトル](URL) from ニックネーム` をテキストチャンネルへ即時投稿します
- `/dqueue` は実行者にのみ見えるエフェメラル応答です
- キューが空になると VC から切断し、ギルドセッションを破棄します

## 動作概要

1. 起動時に `DISCORD_TOKEN`、`ffmpeg`、`yt-dlp` の利用可否を検証します。
2. ユーザーが `/dplay <url>` を実行します。
3. Bot は実行者が参加しているボイスチャンネルへ接続し、URL を解決します。
4. 解決結果をキューへ追加し、テキストチャンネルへ `Queued: ...` を投稿します。
5. 音声は `ffmpeg` で PCM 化し、20ms 単位で Opus フレームへ変換して送信します。
6. 待機キューがなくなると、Bot は自動でボイスチャンネルから切断します。

## ローカル実行の前提条件

- Go 1.26 以上
- `ffmpeg`
- `yt-dlp`
- `github.com/disgoorg/godave/golibdave` が利用するネイティブライブラリ `libdave`
- Discord Bot アプリケーション

## 手動セットアップ

### 1. `ffmpeg` と `yt-dlp` を準備する

#### macOS (Homebrew)

```bash
brew install ffmpeg yt-dlp
```

#### Ubuntu / Debian 系

```bash
sudo apt-get update
sudo apt-get install -y ffmpeg python3-pip
python3 -m pip install --user -U yt-dlp
```

`yt-dlp` をユーザー領域へ入れた場合は、実行ファイルのあるディレクトリが `PATH` に入っていることを確認してください。

### 2. `libdave` を準備する

Discord 音声接続のために `godave` / `golibdave` を使用しており、ネイティブライブラリ `libdave` が必要です。

公式 README:

- [disgoorg/godave README](https://github.com/disgoorg/godave/blob/master/README.md)

公式スクリプトの利用例:

```bash
curl -fsSL -o libdave_install.sh https://raw.githubusercontent.com/disgoorg/godave/refs/heads/master/scripts/libdave_install.sh
chmod +x libdave_install.sh
./libdave_install.sh v1.1.1
```

`go build` や `go run` で `pkg-config` 関連のエラーが出る場合は、少なくとも次をシェルへ通してください。

```bash
export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig:$PKG_CONFIG_PATH"
```

### 3. Discord Bot を準備する

#### OAuth2 Scope

- `bot`
- `applications.commands`

#### Privileged Intent

- 不要です
- `MESSAGE CONTENT INTENT` は不要です

#### Bot に必要な権限の目安

- 「メッセージを送る」
- 「接続」
- 「発言」

## 環境変数

| 変数名 | 必須 | 説明 |
| --- | --- | --- |
| `DISCORD_TOKEN` | 必須 | Discord Bot トークン |
| `DISCORD_COMMAND_GUILD_ID` | 任意 | コマンドを特定ギルドへだけ登録したい場合のギルド ID |
| `FFMPEG_PATH` | 任意 | `ffmpeg` の実行パス。未指定時は `PATH` から探索します |
| `YT_DLP_PATH` | 任意 | `yt-dlp` の実行パス。未指定時は `PATH` から探索します |

### macOS 向け設定例

```bash
export DISCORD_TOKEN="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
export FFMPEG_PATH="$(command -v ffmpeg)"
export YT_DLP_PATH="$(command -v yt-dlp)"
```

### Linux 向け設定例

```bash
export DISCORD_TOKEN="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
export FFMPEG_PATH="$(command -v ffmpeg)"
export YT_DLP_PATH="$(command -v yt-dlp)"
```

## 起動方法

```bash
go run .
```

ビルドする場合:

```bash
go build ./...
```

起動時に以下を満たさない場合は即座に終了します。

- `DISCORD_TOKEN` が未設定
- `ffmpeg` が見つからない
- `yt-dlp` が見つからない

## Docker / compose

現在の `Dockerfile` と `docker-compose.yaml` は旧 TTS 構成のままです。コンテナ実行手順は次の更新段階で音楽 Bot 用へ整理します。

## 注意点

- グローバルスラッシュコマンドの反映には時間がかかることがあります
- 直接リンクは 100MB を超えると拒否されます
- `yt-dlp` 側で未対応の URL は再生できません

## ライセンス

このリポジトリ内のソースコードは [MIT License](LICENSE) です。

ただし、Docker ビルド時に取得する `tohoku-f01-neutral.htsvoice` などの外部コンポーネントには、それぞれ個別のライセンスが適用されます。利用時は配布元の条件も確認してください。

