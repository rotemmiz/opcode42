/* ============================================================
   opcode42-bits.jsx — shared primitives for the direction mockups
   Syntax token colors, diff rows, status badges, spinner, and the
   real session diff data ported from the TUI's data.js.
   Exports (to window): Tok, DiffRow, CodeLine, Badge, Spinner,
   DIFF_LINES, WRITE_LINES, sem
   ============================================================ */

// semantic syntax colors (identical to the TUI mapping)
const sem = {
  kw: 'var(--purple)', fn: 'var(--blue)', ty: 'var(--cyan)',
  str: 'var(--green)', num: 'var(--amber)', com: 'var(--on-surface-faint)',
  pun: 'var(--on-surface-variant)', var: 'var(--on-surface)', prop: 'var(--red)',
};

function Tok({ c, children }) {
  return <span style={{ color: sem[c] || 'var(--on-surface)', fontStyle: c === 'com' ? 'italic' : 'normal' }}>{children}</span>;
}

// render an array of [cls, text] pairs (data.js token format)
function CodeLine({ tokens }) {
  return <>{tokens.map((t, i) => <Tok key={i} c={t[0]}>{t[1]}</Tok>)}</>;
}

// one diff row: type = ctx|add|del|hunk|hdr-old|hdr-new
function DiffRow({ type, text, num, hl }) {
  const map = {
    add: { bg: 'var(--diff-add)', sign: '+', signColor: 'var(--green)', color: 'var(--on-surface)' },
    del: { bg: 'var(--diff-del)', sign: '-', signColor: 'var(--red)', color: 'var(--on-surface)' },
    ctx: { bg: 'transparent', sign: ' ', signColor: 'transparent', color: 'var(--on-surface-variant)' },
    hunk: { bg: 'var(--hunk-bg)', sign: ' ', signColor: 'transparent', color: 'var(--purple)' },
    'hdr-old': { bg: 'transparent', sign: '', signColor: 'transparent', color: 'var(--red)' },
    'hdr-new': { bg: 'transparent', sign: '', signColor: 'transparent', color: 'var(--cyan)' },
  };
  const s = map[type] || map.ctx;
  const body = hl
    ? <><span>{text.slice(0, hl[0])}</span><span style={{ background: type === 'add' ? 'var(--diff-add-hl)' : 'var(--diff-del-hl)' }}>{text.slice(hl[0], hl[1])}</span><span>{text.slice(hl[1])}</span></>
    : text;
  return (
    <div style={{ display: 'flex', background: s.bg, padding: '0 8px', whiteSpace: 'pre', minHeight: 19 }}>
      <span style={{ width: '1ch', color: s.signColor, flexShrink: 0 }}>{s.sign}</span>
      <span style={{ color: s.color, flex: 1 }}>{body}</span>
    </div>
  );
}

function Badge({ status }) {
  const m = {
    doing:   { c: 'var(--amber)', t: 'in progress' },
    blocked: { c: 'var(--red)',   t: 'blocked' },
    review:  { c: 'var(--cyan)',  t: 'in review' },
    todo:    { c: 'var(--on-surface-variant)', t: 'todo' },
    done:    { c: 'var(--green)', t: 'done' },
  }[status] || { c: 'var(--on-surface-variant)', t: status };
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 12, fontFamily: 'var(--sans)',
      color: m.c, whiteSpace: 'nowrap',
    }}>
      <span style={{ width: 7, height: 7, borderRadius: 999, background: m.c, display: 'inline-block' }} />
      {m.t}
    </span>
  );
}

function Spinner({ color = 'var(--amber)', size = 13 }) {
  const frames = ['⠋','⠙','⠹','⠸','⠼','⠴','⠦','⠧','⠇','⠏'];
  const [i, setI] = React.useState(0);
  React.useEffect(() => {
    const id = setInterval(() => setI(v => (v + 1) % frames.length), 90);
    return () => clearInterval(id);
  }, []);
  return <span style={{ fontFamily: 'var(--mono)', color, fontSize: size }}>{frames[i]}</span>;
}

// ---- the real, full diff from data.js (src/http.ts retry edit) ----
const DIFF_LINES = [
  { type: 'hdr-old', text: '--- src/http.ts' },
  { type: 'hdr-new', text: '+++ src/http.ts' },
  { type: 'hunk', text: '@@ -1,6 +1,7 @@' },
  { type: 'ctx', text: '// Minimal HTTP client over fetch.' },
  { type: 'ctx', text: 'import { Logger } from "./log"' },
  { type: 'add', text: 'import { sleep } from "./util"', hl: [9, 14] },
  { type: 'ctx', text: '' },
  { type: 'ctx', text: 'export interface ReqOpts {' },
  { type: 'ctx', text: '  readonly url: string' },
  { type: 'hunk', text: '@@ -18,9 +19,24 @@' },
  { type: 'ctx', text: 'export async function request(o: ReqOpts) {' },
  { type: 'del', text: '  return fetch(o.url, { method: o.method })', hl: [9, 41] },
  { type: 'add', text: '  return withRetry(() => fetch(o.url, { method: o.method }))', hl: [9, 18] },
  { type: 'ctx', text: '}' },
  { type: 'add', text: '' },
  { type: 'add', text: 'const RETRIABLE = new Set([502, 503, 504])' },
  { type: 'add', text: '' },
  { type: 'add', text: 'async function withRetry(fn, max = 3) {' },
  { type: 'add', text: '  for (let attempt = 1; ; attempt++) {' },
  { type: 'add', text: '    const res = await fn()' },
  { type: 'add', text: '    if (res.ok || !RETRIABLE.has(res.status)) return res' },
  { type: 'add', text: '    if (attempt >= max) return res' },
  { type: 'add', text: '    const backoff = 2 ** attempt * 50 + Math.random() * 50' },
  { type: 'add', text: '    await sleep(backoff)' },
  { type: 'add', text: '  }' },
  { type: 'add', text: '}' },
];

Object.assign(window, { Tok, CodeLine, DiffRow, Badge, Spinner, DIFF_LINES, sem });
