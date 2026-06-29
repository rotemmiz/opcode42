/* ============================================================
   system-board.jsx — the design system proposal
   Token mapping, type scale, shape/density, idiom translation,
   component inventory. Rendered as one wide board on the canvas.
   Exports: SystemBoard (to window)
   ============================================================ */

const sbWrap = {
  fontFamily: 'var(--sans)',
  color: 'var(--on-surface)',
  background: 'var(--surface)',
  width: 1080,
  padding: '38px 40px 44px',
  boxSizing: 'border-box',
  display: 'grid',
  gridTemplateColumns: 'repeat(12, 1fr)',
  gap: 18,
};

function Card({ span = 6, title, kicker, children, pad = 20 }) {
  return (
    <div style={{
      gridColumn: `span ${span}`,
      background: 'var(--surface-container)',
      border: '1px solid var(--hairline)',
      borderRadius: 'var(--r-md)',
      padding: pad,
      boxSizing: 'border-box',
    }}>
      {kicker && (
        <div style={{
          fontFamily: 'var(--mono)', fontSize: 11, letterSpacing: 1.2,
          textTransform: 'uppercase', color: 'var(--purple)', marginBottom: 8,
        }}>{kicker}</div>
      )}
      {title && (
        <div style={{ fontSize: 17, fontWeight: 500, marginBottom: 14, color: 'var(--on-surface)' }}>{title}</div>
      )}
      {children}
    </div>
  );
}

// ---- color role row ----
function Role({ hex, name, role, meaning }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '6px 0' }}>
      <div style={{
        width: 30, height: 30, borderRadius: 7, background: hex, flexShrink: 0,
        border: '1px solid rgba(255,255,255,0.10)',
      }} />
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
          <span style={{ fontFamily: 'var(--mono)', fontSize: 12.5, color: 'var(--on-surface)' }}>{hex}</span>
          <span style={{ fontFamily: 'var(--mono)', fontSize: 11.5, color: 'var(--on-surface-faint)' }}>{name}</span>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', marginTop: 2 }}>
          <span style={{ fontSize: 12.5, color: 'var(--cyan)' }}>{role}</span>
          <span style={{ fontSize: 12, color: 'var(--on-surface-variant)' }}>· {meaning}</span>
        </div>
      </div>
    </div>
  );
}

// ---- idiom translation row ----
function Idiom({ from, to }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '7px 0', borderBottom: '1px solid var(--hairline)' }}>
      <div style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12.5, color: 'var(--on-surface-variant)' }}>{from}</div>
      <div style={{ color: 'var(--on-surface-faint)', fontSize: 13 }}>→</div>
      <div style={{ flex: 1, fontSize: 13, color: 'var(--on-surface)' }}>{to}</div>
    </div>
  );
}

function Chip({ children, color = 'var(--on-surface-variant)', bg = 'var(--surface-container-high)' }) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      fontSize: 12, fontFamily: 'var(--sans)', color,
      background: bg, border: '1px solid var(--hairline)',
      borderRadius: 'var(--r-full)', padding: '4px 11px', lineHeight: 1.1,
    }}>{children}</span>
  );
}

function SystemBoard() {
  return (
    <div style={sbWrap}>
      {/* header */}
      <div style={{ gridColumn: 'span 12', display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', marginBottom: 2 }}>
        <div>
          <div style={{ fontFamily: 'var(--mono)', fontWeight: 700, fontSize: 13, letterSpacing: 2, color: 'var(--on-surface-faint)', textTransform: 'uppercase' }}>Opcode42 · Android</div>
          <div style={{ fontSize: 30, fontWeight: 500, marginTop: 6, letterSpacing: -0.5 }}>The system, before we build</div>
          <div style={{ fontSize: 15, color: 'var(--on-surface-variant)', marginTop: 6, maxWidth: 720, lineHeight: 1.5 }}>
            A touch-first sibling of the Opcode42 TUI. Same charcoal, same semantic colors, same agent-conversation model —
            translated into Material 3 patterns. Not a terminal emulator on a phone.
          </div>
        </div>
        <div style={{ textAlign: 'right', fontFamily: 'var(--mono)', fontSize: 12, color: 'var(--on-surface-faint)', lineHeight: 1.7 }}>
          <div>phone-primary · tablet-adaptive</div>
          <div>dark primary · light toggle</div>
          <div>M3 bones · Opcode42 character</div>
        </div>
      </div>

      {/* assumptions */}
      <Card span={12} kicker="What I'm assuming" title="Brief">
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 22, fontSize: 13.5, lineHeight: 1.55, color: 'var(--on-surface-variant)' }}>
          <div>The app talks to the <strong style={{ color: 'var(--on-surface)' }}>same coding-agent backend</strong> as the TUI. The hero is the conversation stream — reasoning, collapsible tool calls, syntax-highlighted diffs, command output, todos, sub-agents, summary.</div>
          <div>Material idioms replace terminal ones: the amber selection bar becomes a <strong style={{ color: 'var(--on-surface)' }}>selected-row / active state</strong>; slash &amp; @ palettes become <strong style={{ color: 'var(--on-surface)' }}>bottom sheets</strong>; the status bar becomes a <strong style={{ color: 'var(--on-surface)' }}>compact strip</strong> above the composer; the tasks board becomes a <strong style={{ color: 'var(--on-surface)' }}>bottom-nav tab</strong>.</div>
          <div>Density is <strong style={{ color: 'var(--on-surface)' }}>balanced</strong> — dense content, but every hit target ≥ 48dp. UI text in Roboto; all code, diffs &amp; tool output stay in Roboto Mono with the TUI's syntax-color mapping intact. No emoji, minimal chrome.</div>
        </div>
      </Card>

      {/* color mapping */}
      <Card span={6} kicker="TUI → M3 color roles" title="Surfaces & text" pad={18}>
        <Role hex="#15171a" name="--bg" role="surface" meaning="page background" />
        <Role hex="#1c1f23" name="--bg-panel" role="surfaceContainer" meaning="cards, composer, tool panels" />
        <Role hex="#20242a" name="--bg-elev" role="surfaceContainerHigh" meaning="bottom sheets, menus" />
        <Role hex="#262b31" name="--bg-sel" role="surfaceContainerHighest" meaning="hover / pressed / drag" />
        <Role hex="#2c3137" name="--border" role="outlineVariant" meaning="hairline borders, dividers" />
        <div style={{ height: 1, background: 'var(--hairline)', margin: '8px 0' }} />
        <Role hex="#d6dade" name="--fg" role="onSurface" meaning="primary text" />
        <Role hex="#8b929a" name="--fg-dim" role="onSurfaceVariant" meaning="secondary, tool lines" />
        <Role hex="#585f67" name="--fg-faint" role="outline" meaning="hints, line numbers, meta" />
      </Card>

      <Card span={6} kicker="Semantic — meanings preserved" title="Accent colors" pad={18}>
        <Role hex="#6fa8dc" name="--blue" role="primary" meaning="agent mode · fn names · prompt accent" />
        <Role hex="#8cc265" name="--green" role="tertiary / success" meaning="added diff · pass · file paths · strings" />
        <Role hex="#e0606e" name="--red" role="error" meaning="removed diff · errors · blocked" />
        <Role hex="#d99a4e" name="--amber" role="secondary / active" meaning="selection bar · in-progress · numbers" />
        <Role hex="#b08cd4" name="--purple" role="headers / keywords" meaning="section heads · h3 · table heads" />
        <Role hex="#5fb3c4" name="--cyan" role="links / types" meaning="@-mentions · types · in-review · pills" />
        <div style={{ marginTop: 12, fontSize: 12, color: 'var(--on-surface-faint)', lineHeight: 1.5 }}>
          The amber <span style={{ color: 'var(--amber)' }}>selection bar</span> — the TUI's signature cursor row — becomes the M3
          selected/active state: a tonal fill + 2px amber inset, never a full-bleed solid (too loud on touch).
        </div>
      </Card>

      {/* type scale */}
      <Card span={7} kicker="Roboto + Roboto Mono" title="Type scale">
        <div style={{ display: 'flex', flexDirection: 'column', gap: 13 }}>
          <TypeRow label="Display / wordmark" note="Roboto Mono 700 · 28" style={{ fontFamily: 'var(--mono)', fontWeight: 700, fontSize: 28, letterSpacing: -0.5 }}>opcode42</TypeRow>
          <TypeRow label="Title large" note="Roboto 400 · 22" style={{ fontSize: 22 }}>Add retry + backoff</TypeRow>
          <TypeRow label="Title medium" note="Roboto 500 · 16" style={{ fontSize: 16, fontWeight: 500 }}>Adding retry with backoff</TypeRow>
          <TypeRow label="Body large" note="Roboto 400 · 15" style={{ fontSize: 15 }}>I&rsquo;ll wrap the request path with a bounded retry loop…</TypeRow>
          <TypeRow label="Label / chip" note="Roboto 500 · 13" style={{ fontSize: 13, fontWeight: 500 }}>Build · Claude Opus 4.8</TypeRow>
          <TypeRow label="Code · mono" note="Roboto Mono 400 · 13 / 1.5" style={{ fontFamily: 'var(--mono)', fontSize: 13 }}><span style={{ color: 'var(--purple)' }}>const</span> <span style={{ color: 'var(--blue)' }}>withRetry</span> = <span style={{ color: 'var(--green)' }}>async</span> () =&gt; {'{'}</TypeRow>
        </div>
      </Card>

      {/* shape & density */}
      <Card span={5} kicker="Opcode42 character" title="Shape & density">
        <div style={{ fontSize: 13, color: 'var(--on-surface-variant)', lineHeight: 1.55, marginBottom: 14 }}>
          Tighter corners than stock M3 — developer-grade, not bubbly. Code surfaces are nearly square so monospace reads as code.
        </div>
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          {[['4', 'code / diff'], ['8', 'tool cards'], ['12', 'message cards'], ['16', 'sheets'], ['full', 'chips · FAB']].map(([r, l]) => (
            <div key={r} style={{ textAlign: 'center' }}>
              <div style={{
                width: 56, height: 56, background: 'var(--surface-container-high)',
                border: '1px solid var(--outline-variant)',
                borderRadius: r === 'full' ? 999 : `${r}px`,
              }} />
              <div style={{ fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--on-surface)', marginTop: 6 }}>{r === 'full' ? 'pill' : `${r}dp`}</div>
              <div style={{ fontSize: 10.5, color: 'var(--on-surface-faint)' }}>{l}</div>
            </div>
          ))}
        </div>
        <div style={{ marginTop: 16, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Chip>4dp grid spacing</Chip>
          <Chip>≥ 48dp targets</Chip>
          <Chip>hairlines over elevation</Chip>
        </div>
      </Card>

      {/* idiom translation */}
      <Card span={6} kicker="Terminal → Material" title="Idiom translation">
        <Idiom from="Box-drawing panels" to="Expandable M3 cards / surfaces" />
        <Idiom from="Amber full-width selection bar" to="Selected-row state · active chip" />
        <Idiom from="Slash-command palette" to="Bottom-sheet command palette" />
        <Idiom from="@-mention picker" to="Inline autocomplete sheet" />
        <Idiom from="Right context sidebar" to="App-bar subtitle + session-info sheet" />
        <Idiom from="Bottom status bar" to="Compact strip above composer" />
        <Idiom from="Tasks board dock" to="Bottom-nav tab · list + status chips" />
        <Idiom from="Vim leader keys (ctrl+x)" to="Swipe · long-press · FAB / overflow" />
      </Card>

      {/* component inventory */}
      <Card span={6} kicker="What we'll build" title="Component inventory">
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 7 }}>
          {['Top app bar + context subtitle','Composer + send','Status strip','Bottom nav','User turn','Agent prose','Thought line','Tool row','Expandable tool card','Diff viewer','Code viewer','Bash output','Todos list','Sub-agent card','Summary table','Command palette sheet','@-mention sheet','Model picker','Agent picker','Session list','Session-info sheet','Mode chip','Status badges','Empty / splash'].map(c => (
            <Chip key={c}>{c}</Chip>
          ))}
        </div>
        <div style={{ marginTop: 16, fontSize: 12.5, color: 'var(--on-surface-faint)', lineHeight: 1.5 }}>
          The streaming, expand/collapse, and sheet interactions will be mocked so they feel real in the chosen direction.
        </div>
      </Card>

      {/* directions intro */}
      <Card span={12} kicker="Pick one — then I build it out fully" title="Three directions, same screen">
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 22, fontSize: 13.5, lineHeight: 1.55 }}>
          <DirBlurb tag="A" color="var(--blue)" name="Faithful M3" desc="By-the-book Material 3 in charcoal. Rounded cards, M3 elevation tints, friendly. The safe, recognizable baseline." />
          <DirBlurb tag="B" color="var(--amber)" name="Terminal-Material" desc="M3 bones, Opcode42 skin. Hairline borders, tight radii, monospace-forward, the amber active rail. The true sibling of the TUI." />
          <DirBlurb tag="C" color="var(--purple)" name="Expressive spine" desc="M3 Expressive. A semantic-colored timeline spine threads the turn; bigger type, more air, color-forward. The bold take." />
        </div>
        <div style={{ marginTop: 16, fontSize: 12.5, color: 'var(--on-surface-variant)' }}>
          Each phone below renders the <em>same</em> agent turn — “add retry + backoff to the HTTP client” — so you can compare apples to apples.
          Mix &amp; match welcome (e.g. B&rsquo;s density with C&rsquo;s spine).
        </div>
      </Card>
    </div>
  );
}

function TypeRow({ label, note, style, children }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 16 }}>
      <div style={{ width: 130, flexShrink: 0 }}>
        <div style={{ fontSize: 12.5, color: 'var(--on-surface)' }}>{label}</div>
        <div style={{ fontFamily: 'var(--mono)', fontSize: 10.5, color: 'var(--on-surface-faint)' }}>{note}</div>
      </div>
      <div style={{ ...style, color: style.color || 'var(--on-surface)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{children}</div>
    </div>
  );
}

function DirBlurb({ tag, color, name, desc }) {
  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <div style={{
        width: 30, height: 30, flexShrink: 0, borderRadius: 8,
        background: color, color: 'var(--surface)', fontFamily: 'var(--mono)', fontWeight: 700,
        display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 15,
      }}>{tag}</div>
      <div>
        <div style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--on-surface)' }}>{name}</div>
        <div style={{ fontSize: 13, color: 'var(--on-surface-variant)', marginTop: 3 }}>{desc}</div>
      </div>
    </div>
  );
}

window.SystemBoard = SystemBoard;
