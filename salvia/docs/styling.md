# Salvia CSS-in-JSX 方針

## 目的

Salvia のスタイルは、CSS ファイルを追加せず、原則としてスタイルを使う
コンポーネントの TSX 内で完結させる。
通常の宣言は React の `style` オブジェクトを優先し、疑似クラス、疑似要素、
子孫セレクター、メディアクエリなど `style` 属性では表現できない規則だけを、
Salvia から切り出した npm パッケージ `mintcss` で扱う。

## コンポーネントでの書き方

コンポーネント固有のスタイルを共有の巨大なスタイル表へ移さない。
静的な通常宣言と動的な値は、コンポーネント内のオブジェクトとして定義する。

```tsx
const styles = {
	button: {
		width: '100px',
		height: '50px',
		border: 'none',
		borderRadius: '10px',
	} satisfies React.CSSProperties,
};

<button
	className={buttonClass}
	style={{
		...styles.button,
		backgroundColor: isClick ? 'pink' : 'skyblue',
	}}
>
	ボタン
</button>;
```

`buttonClass` のようなクラス名が必要な場合だけ、同じ TSX 内に `mintcss` の `css()` 用の
オブジェクトを置く。

```tsx
import { css } from "mintcss";

const buttonClass = css({
	transition: 'transform 160ms ease',
	'&:hover': {
		transform: 'translateY(-1px)',
	},
	'&:focus-visible': {
		outline: '3px solid var(--focus-ring)',
	},
	'@media (prefers-reduced-motion: reduce)': {
		transition: 'none',
	},
});
```

コンポーネント間で共有してよいのはテーマ値、型、CSS 生成機構、および明確に
再利用される UI コンポーネントである。ページや機能に固有の見た目を共有
`styles.tsx` のようなカタログへ集約しない。

選択UIには `src/components/ui/Dropdown.tsx` の共通ドロップダウンを使う。
ブラウザー標準の `select` に見た目だけを重ねず、listboxとしてのキーボード操作、
外側クリックとEscapeでの閉じ方、フォーカス復帰、選択状態を維持する。開閉の
アニメーションはコンポーネント内のオブジェクトと `keyframes()` で定義し、
Salviaの「動きを減らす」設定とOSのreduced-motion設定の両方を尊重する。

## mintcss ランタイム

npm の `mintcss` に依存し、`css`、`globalCss`、`keyframes` を直接 import する。
`src/lib/css.tsx` の独自実装は保持しない。バージョンは `package.json` と
`pnpm-lock.yaml` で固定する。

- `css(object)` は正規化したオブジェクトから決定的なクラス名を生成する。
- 同じ規則はハッシュとレジストリで重複排除し、単一の `style` 要素へ一度だけ注入する。
- camelCase のプロパティ、CSS カスタムプロパティ、`&` を含むセレクター、
  `@media`、`@supports`、`@keyframes` を必要な範囲で扱う。
- 規則の直列化と挿入順を決定的にし、開発時の再描画や HMR で規則を増殖させない。
- Salvia はクライアント専用 SPA のため、`mintcss/next` や SSR 用 API は使わない。
- ベンダープレフィックスの網羅的な自動付与など、現在の対象ブラウザーに不要な
  CSS プリプロセッサ機能は持たせない。

生成 API はソースコードに書かれたスタイル定義だけを受け取る。API や ActivityPub
から取得した文字列をセレクター、プロパティ名、未検証の CSS 本文へ渡さない。
色や座標などの動的な値は React の `style` 属性または検証済み CSS カスタム
プロパティとして渡す。

## グローバル規則の例外

全体で一度だけ必要な次の規則は、ルートの TSX から注入する最小のグローバル定義に
限定する。

- reset と `html`、`body`、`#root` の土台
- 黄色を基調とするテーマトークンと CSS カスタムプロパティ
- フォント、選択色、スクロールバーなど文書全体の既定値
- 複数コンポーネントが本当に共有する keyframes

グローバル定義から特定コンポーネントの内部構造を参照しない。状態やバリエーションは
コンポーネント自身の `style`、生成クラス、または props で表現する。

## 検証

Salvia の統合テストでは、npm パッケージを直接読み込み、クラス名の決定性、
重複排除、camelCase の変換、ネストしたセレクター、メディアクエリ、keyframes、
危険な入力の拒否を確認する。テストのリセットには `mintcss/testing` の
`__resetCssForTests` を使い、生成スタイルは `style[data-mintcss]` で検査する。
各コンポーネントでは、見た目の実装詳細ではなく状態と操作に応じて適切な
スタイルまたはクラスが選ばれることを確認する。既存の lint、Vitest、production
build に加え、生成物へ `.css` ファイルが混入していないことも CI で検査する。

## 移行順序

1. `css()`、keyframes、グローバル注入とその単体テストを追加する。
2. テーマトークンと真にグローバルな規則だけをルートの TSX へ移す。
3. コンポーネント単位で通常宣言をローカルな `style` オブジェクトへ、残りをローカルな
   `css()` 定義へ移し、各段階で挙動とレスポンシブ表示を確認する。
4. 集約されたコンポーネント用 CSS 文字列を削除し、CSS ファイルがないことを確認する。
   CSS 生成は `mintcss` に統一し、別のランタイムや CSS フレームワークは追加しない。
5. `internal/salvia/dist` を Git の追跡対象から外す。Go の `embed` はビルド時に
   `dist` を必要とするため、CI、Docker、ローカルのリリース手順は必ず
   `pnpm --dir salvia build` を `go build` より前に実行する形へ同時に更新する。

この方針を実装の判断基準とし、通常宣言を共有スタイル表へ戻さない。
