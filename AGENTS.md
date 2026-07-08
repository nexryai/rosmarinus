# Rosmarinus ActivityPub Server

## 追加規約
 - ActivityPub HTTP Signature の署名・検証は、ドラフト段階の仕様が現在も使われているため、独自暗号実装ではなく `github.com/go-fed/httpsig` を使用すること。Concorde / `@peertube/http-signature` 相当の互換挙動が必要な場合も、このライブラリを薄くラップして実装すること。
 - 実装がまとまり、テストが通った適切なタイミングで git commit を作成すること。未完了の作業や無関係な差分は同じ commit に混ぜないこと。

## 規約
 - Goのベストプラクティスに従うこと
 - DIを使用すること
 - Goの`log`パッケージを使用してなるべくlogすること
 - テストをなるべく書くこと
 - ソースコードの改行コードにはLFを使用すること

## 要件
 - このプロジェクトは、concordeというMisskeyフォークの後継プロジェクトです。
 - Concordeの参考となるソースコードは`./concorde`にあります。このディレクトリは編集しないこと。ActivityPubのパース、署名関係の実装や、MFM、カスタム絵文字周りの扱いについてはこのディレクトリを参考にしてなるべく同等の処理で実装すること。
 - rosmarinusはActivityPubを使用して通信する部分のマイクロサービスであり、このプロジェクトにフロントエンドやAPIは含まれません。主な責務はActivityPubで他のインスタンスと通信し、MongoDBに書き込むことです。
 - データベースにはMongoDBを使用します。MongoDBのベストプラクティスに従ってDBを設計してください。
 - 設定はすべて環境変数で流し込むようにして
