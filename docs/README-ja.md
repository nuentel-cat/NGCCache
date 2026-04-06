# NGCCache (Pure Go版)

<div align="center">
  <p><strong>Goのための超高速・Zero-GC・オフヒープ アリーナキャッシュ</strong></p>

  [![Go Reference](https://pkg.go.dev/badge/github.com/nuentel-cat/NGCCache.svg)](https://pkg.go.dev/github.com/nuentel-cat/NGCCache)
  [![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
</div>

<br />

NGCCacheは、Goのガベージコレクション（GC）によるオーバーヘッドを完全に回避するために設計された、高性能な **Pure Go** 製オフヒープメモリキャッシュライブラリです。OSのシステムコール（Unixでは `mmap`、Windowsでは `VirtualAlloc`）を直接利用してメモリを管理することで、Goのランタイム管理外にデータを保持します。

広告配信、オンラインゲーム、高頻度取引（HFT）システムなど、GCによるマイクロ秒単位のレイテンシスパイク（Stop-The-World）が許容されない大規模なトラフィック環境において、「**一度書き込み、何度も読み取る**」ワークロードに特化して最適化されています。

---

## 🚀 主な特徴

- **GCオーバーヘッド・ゼロ**: キャッシュデータとインデックス構造（ハッシュテーブル）の両方をオフヒープに配置。エントリー数が100件でも1億件でも、GoのGCスキャン時間は一定です。
- **Pure Go 実装**: Cgoは不要です。クロスコンパイルが容易で、ビルド時間の短縮とセキュリティの向上を実現します。
- **ゴルーチン・スコープ・アリーナ**: キャッシュの寿命をゴルーチン（リクエスト）に紐付けることができます。ゴルーチン終了時に、使用していたメモリをO(1)で瞬時に再利用可能な状態に戻します。
- **「インメモリ版のInnoDB」**: 読み取り専用モード（`SetReadOnly`）やゼロコピー参照（`GetUnsafe`）など、安全性とパフォーマンスのトレードオフを制御できる高度なチューニングオプションを提供します。

---

## 🔧 インストール

```bash
go get github.com/nuentel-cat/NGCCache
```

---

## 🔧 設定 (Configuration)

NGCCacheの設定は直感的です。メモリ容量ではなく、格納したい「キーの数」でキャパシティを定義します。

```go
type Config struct {
    LocalCacheMaxKeys  uint64 // 各セッション（リクエスト単位）で合計して保持できる最大キー数
    SharedCacheMaxKeys uint64 // グローバル共有キャッシュに保持できる最大キー数
    MaxValueSize       uint64 // 1キーあたりの最大データサイズ（バイト）
    Verbose            bool   // 起動時に詳細なメモリ内訳を表示するかどうか
}
```

### 初期化の例
```go
import "github.com/nuentel-cat/NGCCache"

cache, err := ngccache.NewCache(ngccache.Config{
    LocalCacheMaxKeys:  1000000, // 100万件のセッション用プール
    SharedCacheMaxKeys: 50000,   // 5万件の共有マスターデータ用
    MaxValueSize:       4096,    // 各データ最大4KB
    Verbose:            true,
})
defer cache.Close() // OSメモリを解放するために必須です
```

---

## ⚡️ API リファレンス

### セッションキャッシュ (Local)
リクエスト終了時にメモリを一括リサイクルしたい場合に使用します。
- `BeginSession() (sessionID uint64, endSession func())`
- `Set(sid uint64, key string, data []byte) error`
- `Get(sid uint64, key string) ([]byte, bool)`
- `Delete(sid uint64, key string) bool`
- `Exist(sid uint64, key string) bool`
- `Add(sid uint64, key string, data []byte) error` (キーが存在しない場合のみ格納)
- `Increment(sid uint64, key string, delta int64) (int64, error)` (アトミック)
- `Decrement(sid uint64, key string, delta int64) (int64, error)` (アトミック)

### 共有キャッシュ (Global)
全ゴルーチンからアクセス可能な永続的なキャッシュです。
- `SetShared(key string, data []byte) error`
- `GetShared(key string) ([]byte, bool)`
- `DeleteShared(key string) bool`
- `ExistShared(key string) bool`
- `IncrementShared(key string, delta int64) (int64, error)`
- `DecrementShared(key string, delta int64) (int64, error)`

---

## 🚀 高度なパフォーマンスチューニング

### 1. `SetReadOnly()` - ロックフリー読み取り
マスターデータの投入が完了した後に `cache.SetReadOnly()` を呼び出すことで、共有キャッシュからの読み取り（`GetShared`）が**完全にロックフリー**になります。これにより、並列読み取り性能が **~26 ns/op** まで向上します。
*注意: これ以降の書き込み操作は `ErrReadOnly` を返します。*

### 2. `GetUnsafe()` - ゼロコピー参照
極限の低遅延を求める場合、`GetUnsafe` を使用するとデータのコピーすら行わず、**オフヒープメモリを直接指すスライス**を返します（約 **15 ns/op**）。
- **重要**: 返されたスライスは、そのキーが更新・削除されるか、セッションが終了するまでのみ有効です。
- **禁止事項**: 返されたスライスを保存したり、別のゴルーチンに渡したり、内容を書き換えたり（`append` 等）しないでください。読み取り後、即座に破棄する用途に限定してください。

---

## ❌ エラーハンドリング

NGCCacheは、予測可能な動作のために明示的なエラーを返します。
- `ErrOffHeapOutOfMemory`: 確保したキーの枠を使い切りました。
- `ErrDataTooLarge`: データが設定された `MaxValueSize` を超えています。
- `ErrInvalidSession`: 既に終了したセッションIDを使用しようとしました。
- `ErrCacheAlreadyExists`: `Add` 呼び出し時に、既にキーが存在していました。
- `ErrReadOnly`: ReadOnlyモード中に書き込みを試みました。

---

## 📊 ベンチマーク (共有キャッシュ読み取り)

Intel Core i9-14900HX でのテスト結果:

| エンジン | 戦略 | 性能 (レイテンシ) |
|:---|:---|:---:|
| **NGCCache** | **ReadOnly (Safe)** | **26 ns/op** |
| **NGCCache** | **Unsafe (Zero-Copy)** | **15 ns/op** |
| 一般的な高速キャッシュ | デフォルト | ~33 ns/op |

---

## 💻 プラットフォーム・サポート
- **Linux / macOS**: `syscall.Mmap` を使用
- **Windows**: `syscall.VirtualAlloc` を使用

---

## 📄 ライセンス
NGCCache は **Apache License 2.0** の下で公開されています。詳細は [LICENSE](LICENSE) を参照してください。
