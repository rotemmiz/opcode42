/* ============================================================
   dir-b.jsx — Direction B · "Terminal-Material"
   M3 bones, Opcode42 skin. Hairline borders, tight radii, mono-forward,
   the amber active rail carried from the TUI. Exports: DirB
   ============================================================ */

function DirBToolRow({ glyph, gcolor, label, path, meta, last }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 10, minHeight: 44, padding: '0 14px',
      borderBottom: last ? 'none' : '1px solid var(--hairline)', fontFamily: 'var(--mono)', fontSize: 13,
    }}>
      <span style={{ color: gcolor, width: '1.1em', flexShrink: 0 }}>{glyph}</span>
      <span style={{ color: 'var(--on-surface)' }}>{label}</span>
      <span style={{ color: 'var(--on-surface-variant)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{path}</span>
      {meta && <span style={{ color: 'var(--on-surface-faint)', marginLeft: 'auto', flexShrink: 0 }}>{meta}</span>}
    </div>
  );
}

const TODO_ITEMS = [
  ['done', 'Add withRetry() with exponential backoff'],
  ['done', 'Skip retry on 4xx responses'],
  ['doing', 'Cover with a flaky-server test'],
  ['pend', 'Document retry behaviour in README'],
];

function TodoSheet() {
  const PEEK = 50, EXP = 308;
  const [h, setH] = React.useState(PEEK);
  const drag = React.useRef(null);
  const ref = React.useRef(null);
  const open = h > PEEK + 24;
  const onDown = (e) => {
    const scale = ref.current.getBoundingClientRect().width / ref.current.offsetWidth || 1;
    drag.current = { y: e.clientY, h, scale, moved: false };
    try { e.currentTarget.setPointerCapture(e.pointerId); } catch (_) {}
  };
  const onMove = (e) => {
    if (!drag.current) return;
    const dy = (drag.current.y - e.clientY) / drag.current.scale;
    if (Math.abs(dy) > 3) drag.current.moved = true;
    setH(Math.max(PEEK, Math.min(EXP, drag.current.h + dy)));
  };
  const onUp = () => {
    if (!drag.current) return;
    if (!drag.current.moved) setH(v => (v > PEEK + 24 ? PEEK : EXP));
    else setH(v => (v > (PEEK + EXP) / 2 ? EXP : PEEK));
    drag.current = null;
  };
  return (
    <React.Fragment>
      {open && <div onClick={() => setH(PEEK)} style={{ position: 'absolute', top: 53, left: 0, right: 0, bottom: 100, background: 'rgba(8,9,10,0.5)', zIndex: 5 }} />}
      <div ref={ref} style={{
        position: 'absolute', left: 0, right: 0, bottom: 100, height: h, zIndex: 6,
        background: 'var(--surface-container-high)', borderTopLeftRadius: 16, borderTopRightRadius: 16,
        borderTop: '1px solid var(--hairline)', boxShadow: '0 -10px 34px rgba(0,0,0,0.45)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
        transition: drag.current ? 'none' : 'height .24s cubic-bezier(.2,.8,.2,1)',
      }}>
        {/* drag handle + peek row */}
        <div onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp}
          style={{ flexShrink: 0, cursor: 'grab', padding: '8px 14px 8px', touchAction: 'none', userSelect: 'none' }}>
          <div style={{ width: 32, height: 4, borderRadius: 2, background: 'var(--on-surface-faint)', margin: '0 auto 9px' }} />
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <Icon n="tasks" s={16} c="var(--purple)" />
            <span style={{ fontWeight: 500, fontSize: 14, color: 'var(--on-surface)' }}>Todos</span>
            <span style={{ fontFamily: 'var(--mono)', fontSize: 11.5, color: 'var(--cyan)', background: 'rgba(95,179,196,0.12)', padding: '1px 7px', borderRadius: 4 }}>tasks.md</span>
            <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--on-surface-variant)', whiteSpace: 'nowrap' }}>
              <b style={{ color: 'var(--amber)' }}>1</b> active · 2 done
            </span>
            <Icon n={open ? 'chevdown' : 'chevron'} s={16} c="var(--on-surface-faint)" />
          </div>
        </div>
        {/* list */}
        <div style={{ overflowY: 'auto', padding: '2px 14px 14px' }}>
          {TODO_ITEMS.map(([st, t], i) => (
            <div key={i} style={{ display: 'flex', gap: 12, alignItems: 'center', minHeight: 46, borderTop: i ? '1px solid var(--hairline)' : 'none' }}>
              <span style={{
                width: 20, height: 20, borderRadius: st === 'pend' ? 5 : 999, flexShrink: 0,
                border: st === 'pend' ? '2px solid var(--on-surface-ghost)' : 'none',
                background: st === 'done' ? 'var(--green)' : st === 'doing' ? 'transparent' : 'transparent',
                boxShadow: st === 'doing' ? 'inset 0 0 0 2px var(--amber)' : 'none',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}>
                {st === 'done' && <Icon n="check" s={13} c="var(--on-primary)" />}
                {st === 'doing' && <span style={{ width: 7, height: 7, borderRadius: 999, background: 'var(--amber)' }} />}
              </span>
              <span style={{ flex: 1, fontSize: 14, lineHeight: 1.35,
                color: st === 'doing' ? 'var(--amber)' : st === 'done' ? 'var(--on-surface-variant)' : 'var(--on-surface)',
                fontWeight: st === 'doing' ? 600 : 400,
                textDecoration: st === 'done' ? 'none' : 'none' }}>{t}</span>
              {st === 'doing' && <Spinner color="var(--amber)" size={13} />}
            </div>
          ))}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 12, color: 'var(--cyan)', fontSize: 13, whiteSpace: 'nowrap' }}>
            <Icon n="tasks" s={14} c="var(--cyan)" />
            <span>Open tasks board</span>
            <Icon n="chevron" s={14} c="var(--cyan)" />
          </div>
        </div>
      </div>
    </React.Fragment>
  );
}

function DirB() {
  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: 'var(--surface)', fontFamily: 'var(--sans)', position: 'relative' }}>
      {/* top app bar — dense, hairline */}
      <div style={{ flexShrink: 0, background: 'var(--surface)', borderBottom: '1px solid var(--hairline)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '0 6px', height: 52 }}>
          <button style={iconBtnB}><Icon n="back" s={21} c="var(--on-surface)" /></button>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 15, fontWeight: 500, color: 'var(--on-surface)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>Add retry + backoff to http client</div>
            <div style={{ fontSize: 11.5, color: 'var(--on-surface-faint)', fontFamily: 'var(--mono)' }}>~/git/opcode42 · fixture:main</div>
          </div>
          <button style={iconBtnB}><Icon n="info" s={20} c="var(--on-surface-variant)" /></button>
          <button style={iconBtnB}><Icon n="more" s={20} c="var(--on-surface-variant)" /></button>
        </div>
      </div>

      {/* stream */}
      <div style={{ flex: 1, overflow: 'auto', padding: '14px 14px 64px', display: 'flex', flexDirection: 'column', gap: 16 }}>
        {/* user turn — 2px blue accent bar (TUI idiom) */}
        <div style={{ borderLeft: '2px solid var(--blue)', paddingLeft: 13 }}>
          <div style={{ fontSize: 14.5, color: 'var(--on-surface)', lineHeight: 1.5 }}>
            Add retry with exponential backoff to the HTTP client, then cover{' '}
            <span style={{ color: 'var(--cyan)', fontFamily: 'var(--mono)' }}>@src/http.ts</span> with a test.
          </div>
        </div>

        {/* thought */}
        <div style={{ fontFamily: 'var(--mono)', fontSize: 13, color: 'var(--amber)' }}>
          + Thought: <span style={{ color: 'var(--on-surface-faint)' }}>740ms</span>
        </div>

        {/* agent prose */}
        <div>
          <div style={{ fontFamily: 'var(--mono)', fontSize: 11, letterSpacing: 1, textTransform: 'uppercase', color: 'var(--purple)', marginBottom: 8, fontWeight: 700 }}>Adding retry with backoff</div>
          <div style={{ fontSize: 14.5, color: 'var(--on-surface)', lineHeight: 1.55 }}>
            I&rsquo;ll wrap the request path in <code style={cdB}>src/http.ts</code> with a bounded retry loop that backs off exponentially, then add a flaky-endpoint test.
          </div>
          <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {['Add a withRetry() helper with jittered backoff','Only retry on 5xx / network — never 4xx','Cover with a flaky-server test'].map((t, i) => (
              <div key={i} style={{ display: 'flex', gap: 10, fontSize: 14, color: 'var(--on-surface)', lineHeight: 1.45 }}>
                <span style={{ color: 'var(--green)', fontWeight: 700, fontFamily: 'var(--mono)' }}>{i + 1}.</span>{t}
              </div>
            ))}
          </div>
        </div>

        {/* tool rows — single hairline card */}
        <div style={{ border: '1px solid var(--hairline)', borderRadius: 8, overflow: 'hidden', background: 'var(--surface-container)' }}>
          <DirBToolRow glyph="→" gcolor="var(--on-surface-faint)" label="Read" path="src/http.ts" />
          <DirBToolRow glyph="↳" gcolor="var(--on-surface-faint)" label="Loaded" path="src/http.ts" meta="64 lines" />
          <DirBToolRow glyph="*" gcolor="var(--on-surface-faint)" label="Grep" path={'"fetch("'} meta="2" />
          <DirBToolRow glyph="*" gcolor="var(--on-surface-faint)" label="Glob" path={'"src/**/*.ts"'} meta="5" last />
        </div>

        {/* diff — hairline card, amber active rail header, tight 4dp code */}
        <div style={{ border: '1px solid var(--outline-variant)', borderRadius: 8, overflow: 'hidden', background: 'var(--surface-container)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, minHeight: 46, padding: '0 14px', boxShadow: 'inset 2px 0 0 var(--amber)', background: 'var(--secondary-container)' }}>
            <Icon n="chevdown" s={16} c="var(--amber)" />
            <span style={{ fontFamily: 'var(--mono)', fontSize: 13, color: 'var(--on-surface)' }}>Edit <span style={{ color: 'var(--green)' }}>src/http.ts</span></span>
            <span style={{ marginLeft: 'auto', fontFamily: 'var(--mono)', fontSize: 12.5 }}><span style={{ color: 'var(--green)' }}>+14</span> <span style={{ color: 'var(--red)' }}>−1</span></span>
          </div>
          <div style={{ borderTop: '1px solid var(--hairline)', padding: '8px 0', fontFamily: 'var(--mono)', fontSize: 12, lineHeight: 1.65, overflowX: 'auto' }}>
            {DIFF_LINES.map((l, i) => <DiffRow key={i} {...l} />)}
          </div>
        </div>
      </div>

      {/* todos — slide-up bottom sheet (drag the handle, or tap) */}
      <TodoSheet />

      {/* status strip — compact mono bar (TUI status bar) */}
      <div style={{ flexShrink: 0, background: 'var(--surface)', borderTop: '1px solid var(--hairline)', display: 'flex', alignItems: 'center', gap: 10, padding: '0 12px', height: 32, fontFamily: 'var(--mono)', fontSize: 12 }}>
        <span style={{ background: 'var(--blue)', color: 'var(--on-primary)', fontWeight: 700, padding: '1px 8px', borderRadius: 4 }}>Build</span>
        <span style={{ color: 'var(--on-surface)' }}>Opus 4.8</span>
        <span style={{ color: 'var(--on-surface-ghost)' }}>·</span>
        <span style={{ color: 'var(--on-surface-variant)' }}>Anthropic</span>
        <span style={{ marginLeft: 'auto', color: 'var(--on-surface-faint)' }}>34.9K</span>
      </div>

      {/* composer — hairline panel, 2px blue accent bar */}
      <div style={{ flexShrink: 0, background: 'var(--surface)', padding: '8px 12px 12px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, borderLeft: '2px solid var(--blue)', background: 'var(--surface-container)', border: '1px solid var(--hairline)', borderLeftWidth: 2, borderLeftColor: 'var(--blue)', borderRadius: 6, padding: '0 6px 0 13px', minHeight: 48 }}>
          <span style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 13.5, color: 'var(--on-surface-ghost)' }}>Ask anything…  /  @</span>
          <button style={iconBtnB}><Icon n="add" s={19} c="var(--on-surface-variant)" /></button>
          <button style={{ width: 40, height: 40, borderRadius: 6, border: 'none', background: 'var(--blue)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Icon n="send" s={20} c="var(--on-primary)" fill="var(--on-primary)" />
          </button>
        </div>
      </div>
    </div>
  );
}

const iconBtnB = { width: 42, height: 42, borderRadius: 8, border: 'none', background: 'transparent', display: 'flex', alignItems: 'center', justifyContent: 'center', cursor: 'pointer', padding: 0 };
const cdB = { fontFamily: 'var(--mono)', fontSize: 13, color: 'var(--amber)' };

window.DirB = DirB;
