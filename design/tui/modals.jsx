/* ============================================================
   OPCODE42 TUI — modal overlays
   Command palette, models, themes, sessions, agents, timeline, status.
   Each modal manages its own keyboard nav (↑/↓/enter/esc) via the
   shared useListNav hook. Selecting fires onPick(item).
   ============================================================ */
const { useState: useStateM, useEffect: useEffectM, useRef: useRefM, useMemo } = React;

/* shared keyboard list navigation over a flat array of pickable rows */
function useListNav(count, { onPick, onClose }) {
  const [idx, setIdx] = useStateM(0);
  useEffectM(() => {
    function onKey(e) {
      if (e.key === "Escape") { e.preventDefault(); onClose(); return; }
      if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n") || (e.key === "j" && e.target.tagName !== "INPUT")) {
        e.preventDefault(); setIdx(i => Math.min(count - 1, i + 1));
      } else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p") || (e.key === "k" && e.target.tagName !== "INPUT")) {
        e.preventDefault(); setIdx(i => Math.max(0, i - 1));
      } else if (e.key === "Enter") {
        e.preventDefault(); onPick(idx);
      }
    }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [count, idx, onPick, onClose]);
  return [idx, setIdx];
}

function Scrim({ children, onClose }) {
  return (
    <div className="scrim" onMouseDown={(e) => { if (e.target.classList.contains("scrim")) onClose(); }}>
      {children}
    </div>
  );
}

function ModalHead({ title }) {
  return (
    <div className="modal-head">
      <span className="mtitle">{title}</span>
      <span className="esc">esc</span>
    </div>
  );
}

function SearchRow({ value, onChange, placeholder = "Search" }) {
  const ref = useRefM(null);
  useEffectM(() => { ref.current && ref.current.focus(); }, []);
  return (
    <div className="modal-search">
      <input ref={ref} value={value} placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}

/* ---------- Command palette ---------- */
function PaletteModal({ data, onPick, onClose }) {
  const [q, setQ] = useStateM("");
  const flat = useMemo(() => {
    const rows = [];
    const push = (sec, items) => {
      const f = items.filter(i => i.label.toLowerCase().includes(q.toLowerCase()));
      if (f.length) { rows.push({ sec }); f.forEach(i => rows.push(i)); }
    };
    push("Suggested", data.suggested);
    push("Session", data.session);
    return rows;
  }, [q, data]);
  const pickables = flat.filter(r => !r.sec);
  const [sel, setSel] = useStateM(0);
  useEffectM(() => { setSel(0); }, [q]);
  useEffectM(() => {
    function onKey(e) {
      if (e.key === "Escape") { onClose(); }
      else if (e.key === "ArrowDown") { e.preventDefault(); setSel(s => Math.min(pickables.length - 1, s + 1)); }
      else if (e.key === "ArrowUp") { e.preventDefault(); setSel(s => Math.max(0, s - 1)); }
      else if (e.key === "Enter") { e.preventDefault(); pickables[sel] && onPick(pickables[sel]); }
    }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [pickables, sel, onPick, onClose]);

  let pi = -1;
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Commands" />
        <SearchRow value={q} onChange={setQ} />
        <div className="modal-list">
          {flat.map((r, i) => {
            if (r.sec) return <div className="modal-sec" key={"s" + i}>{r.sec}</div>;
            pi++;
            const me = pi;
            return (
              <div key={i} className={"modal-item" + (me === sel ? " sel" : "")}
                onMouseEnter={() => setSel(me)} onClick={() => onPick(r)}>
                <span className="label">{r.label}</span>
                {r.short ? <span className="short">{r.short}</span> : null}
              </div>
            );
          })}
        </div>
      </div>
    </Scrim>
  );
}

/* ---------- Model selector ---------- */
function ModelsModal({ data, onPick, onClose }) {
  const [q, setQ] = useStateM("");
  const flat = useMemo(() => {
    const rows = [];
    Object.entries(data).forEach(([prov, models]) => {
      const f = models.filter(m => m.name.toLowerCase().includes(q.toLowerCase()));
      if (f.length) { rows.push({ sec: prov }); f.forEach(m => rows.push({ ...m, prov })); }
    });
    return rows;
  }, [q, data]);
  const pickables = flat.filter(r => !r.sec);
  const initial = Math.max(0, pickables.findIndex(p => p.current));
  const [sel, setSel] = useStateM(initial);
  useEffectM(() => { setSel(0); }, [q]);
  useEffectM(() => {
    function onKey(e) {
      if (e.key === "Escape") { onClose(); }
      else if (e.key === "ArrowDown") { e.preventDefault(); setSel(s => Math.min(pickables.length - 1, s + 1)); }
      else if (e.key === "ArrowUp") { e.preventDefault(); setSel(s => Math.max(0, s - 1)); }
      else if (e.key === "Enter") { e.preventDefault(); pickables[sel] && onPick(pickables[sel]); }
    }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [pickables, sel, onPick, onClose]);

  let pi = -1;
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Select model" />
        <SearchRow value={q} onChange={setQ} />
        <div className="modal-list">
          {flat.map((r, i) => {
            if (r.sec) return <div className="modal-sec" key={"s" + i}>{r.sec}</div>;
            pi++; const me = pi;
            return (
              <div key={i} className={"modal-item" + (me === sel ? " sel" : "")}
                onMouseEnter={() => setSel(me)} onClick={() => onPick(r)}>
                <span className="marker">{r.current ? "●" : "\u00a0"}</span>
                <span className="label">{r.name}</span>
                {r.tag ? <span className="right">{r.tag}</span> : null}
              </div>
            );
          })}
        </div>
        <div className="modal-foot">
          <span><b>Connect provider</b> <kbd>ctrl+a</kbd></span>
          <span><b>Favorite</b> <kbd>ctrl+f</kbd></span>
        </div>
      </div>
    </Scrim>
  );
}

/* ---------- generic single-list modal (themes / agents) ---------- */
function ListModal({ title, items, render, onPick, onClose, foot }) {
  const [sel, setSel] = useListNav(items.length, {
    onPick: (i) => onPick(items[i], i),
    onClose,
  });
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title={title} />
        <div className="modal-list" style={{ paddingTop: 8 }}>
          {items.map((it, i) => (
            <div key={i} className={"modal-item" + (i === sel ? " sel" : "")}
              onMouseEnter={() => setSel(i)} onClick={() => onPick(it, i)}>
              {render(it)}
            </div>
          ))}
        </div>
        {foot ? <div className="modal-foot">{foot}</div> : null}
      </div>
    </Scrim>
  );
}

function ThemesModal({ items, current, onPick, onClose }) {
  const start = Math.max(0, items.indexOf(current));
  const [sel, setSel] = useListNav(items.length, { onPick: (i) => onPick(items[i]), onClose });
  useEffectM(() => { setSel(start); }, []);
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Themes" />
        <div className="modal-list" style={{ paddingTop: 8 }}>
          {items.map((t, i) => (
            <div key={i} className={"modal-item" + (i === sel ? " sel" : "")}
              onMouseEnter={() => setSel(i)} onClick={() => onPick(t)}>
              <span className="marker">{t === current ? "●" : "\u00a0"}</span>
              <span className="label">{t}</span>
            </div>
          ))}
        </div>
      </div>
    </Scrim>
  );
}

function AgentsModal({ items, onPick, onClose }) {
  const start = Math.max(0, items.findIndex(a => a.current));
  const [sel, setSel] = useListNav(items.length, { onPick: (i) => onPick(items[i]), onClose });
  useEffectM(() => { setSel(start); }, []);
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Select agent" />
        <div className="modal-list" style={{ paddingTop: 8 }}>
          {items.map((a, i) => (
            <div key={i} className={"modal-item" + (i === sel ? " sel" : "")}
              onMouseEnter={() => setSel(i)} onClick={() => onPick(a)}>
              <span className="marker">{a.current ? "●" : "\u00a0"}</span>
              <span className="label" style={{ flex: "none", fontWeight: 700 }}>{a.name}</span>
              <span className="desc">&nbsp;{a.mode}</span>
            </div>
          ))}
        </div>
      </div>
    </Scrim>
  );
}

/* ---------- sessions ---------- */
function SessionsModal({ groups, onPick, onClose }) {
  const [q, setQ] = useStateM("");
  const flat = useMemo(() => {
    const rows = [];
    groups.forEach(g => {
      const f = g.items.filter(i => i.title.toLowerCase().includes(q.toLowerCase()));
      if (f.length) { rows.push({ sec: g.group }); f.forEach(i => rows.push(i)); }
    });
    return rows;
  }, [q, groups]);
  const pickables = flat.filter(r => !r.sec);
  const init = Math.max(0, pickables.findIndex(p => p.current));
  const [sel, setSel] = useStateM(init);
  useEffectM(() => { setSel(0); }, [q]);
  useEffectM(() => {
    function onKey(e) {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowDown") { e.preventDefault(); setSel(s => Math.min(pickables.length - 1, s + 1)); }
      else if (e.key === "ArrowUp") { e.preventDefault(); setSel(s => Math.max(0, s - 1)); }
      else if (e.key === "Enter") { e.preventDefault(); pickables[sel] && onPick(pickables[sel]); }
    }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [pickables, sel, onPick, onClose]);
  let pi = -1;
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Sessions" />
        <SearchRow value={q} onChange={setQ} />
        <div className="modal-list">
          {flat.map((r, i) => {
            if (r.sec) return <div className="modal-sec" key={"s" + i}>{r.sec}</div>;
            pi++; const me = pi;
            return (
              <div key={i} className={"modal-item" + (me === sel ? " sel" : "")}
                onMouseEnter={() => setSel(me)} onClick={() => onPick(r)}>
                <span className="marker">{r.current ? "●" : "\u00a0"}</span>
                <span className="label">{r.title}</span>
                <span className="right">{r.time}</span>
              </div>
            );
          })}
        </div>
        <div className="modal-foot">
          <span><b>pin/unpin</b> <kbd>ctrl+f</kbd></span>
          <span><b>delete</b> <kbd>ctrl+d</kbd></span>
          <span><b>rename</b> <kbd>ctrl+r</kbd></span>
        </div>
      </div>
    </Scrim>
  );
}

/* ---------- timeline ---------- */
function TimelineModal({ items, onPick, onClose }) {
  const [sel, setSel] = useListNav(items.length, { onPick: (i) => onPick(items[i]), onClose });
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Timeline" />
        <SearchRow value="" onChange={() => {}} />
        <div className="modal-list">
          {items.map((t, i) => (
            <div key={i} className={"modal-item" + (i === sel ? " sel" : "")}
              onMouseEnter={() => setSel(i)} onClick={() => onPick(t)}>
              <span className="label">{t.title}</span>
              <span className="right">{t.time}</span>
            </div>
          ))}
        </div>
      </div>
    </Scrim>
  );
}

/* ---------- status ---------- */
function StatusModal({ onClose }) {
  useEffectM(() => {
    function onKey(e) { if (e.key === "Escape") onClose(); }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);
  return (
    <Scrim onClose={onClose}>
      <div className="modal">
        <ModalHead title="Status" />
        <div className="modal-statline">typescript LSP · <span style={{ color: "var(--green)" }}>ready</span></div>
        <div className="modal-statline">prettier · <span style={{ color: "var(--green)" }}>formatting on save</span></div>
        <div className="modal-statline"><span style={{ color: "var(--fg-dim)" }}>No MCP servers</span></div>
        <div className="modal-statline"><span style={{ color: "var(--fg-dim)" }}>2 plugins loaded</span></div>
        <div style={{ height: 8 }} />
      </div>
    </Scrim>
  );
}

Object.assign(window, {
  PaletteModal, ModelsModal, ThemesModal, AgentsModal,
  SessionsModal, TimelineModal, StatusModal,
});
