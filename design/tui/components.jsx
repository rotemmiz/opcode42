/* ============================================================
   OPCODE42 TUI — block renderers & chrome
   ============================================================ */
const { useState } = React;

/* ---- right sidebar ---- */
function Sidebar({ title, ctx, hidden, model, subagentRunning }) {
  if (hidden) return null;
  return (
    <aside className="sidebar">
      <div className="sb-title">{title}</div>

      <div className="sb-block">
        <div className="sb-h">Context</div>
        <div className="sb-line"><span className="num">{ctx.tokens.toLocaleString()}</span> tokens</div>
        <div className="sb-line"><span className="num">{ctx.pct}%</span> used</div>
        <div className="sb-line"><span className="num">${ctx.cost}</span> spent</div>
        <div className="sb-bar"><i style={{ width: ctx.pct + "%" }} /></div>
      </div>

      <div className="sb-block">
        <div className="sb-h">LSP</div>
        <div className="sb-line">typescript · ready</div>
      </div>

      {subagentRunning && (
        <div className="sb-block">
          <div className="sb-h">Tasks</div>
          <div className="sb-agent"><span className="dot" />general · auditing</div>
        </div>
      )}

      <div className="sb-spacer" />
      <div className="sb-foot">
        <div>~/git/opcode42/screenshots</div>
        <div>fixture:main</div>
        <div className="ver">• <b>Opcode42</b> 0.4.2</div>
      </div>
    </aside>
  );
}

/* ---- bottom status bar ---- */
function StatusBar({ mode, model, provider, tokens }) {
  return (
    <div className="statusbar">
      <div className="seg">
        <span className="mode">{mode}</span>
        <span className="sep">·</span>
        <span className="model">{model}</span>
        <span className="prov">{provider}</span>
      </div>
      <div className="spacer" />
      <span className="tok">{tokens}</span>
      <span className="sep">·</span>
      <kbd>ctrl+p</kbd>&nbsp;<span className="hint">commands</span>
    </div>
  );
}

/* ---- user turn ---- */
function UserTurn({ text, mention }) {
  // render @mention in cyan if present
  let body = text;
  if (mention) {
    body = (
      <>{text} <span className="mention">@{mention}</span></>
    );
  }
  return <div className="userturn">{body}</div>;
}

/* ---- thinking line ---- */
function Thought({ ms }) {
  return <div className="thought">+ Thought: <span className="ms">{ms}ms</span></div>;
}

/* ---- markdown prose ---- */
function Markdown({ html }) {
  return <div className="md" dangerouslySetInnerHTML={{ __html: html }} />;
}

/* ---- terse tool row ---- */
function ToolRow({ glyph, label, path, meta }) {
  return (
    <div className="toolrow">
      <span className="glyph">{glyph}</span>
      {label} <span className="path">{path}</span>
      {meta ? <span className="meta"> {meta}</span> : null}
    </div>
  );
}

/* ---- collapsible panel shell ---- */
function Panel({ icon, title, file, meta, error, defaultOpen = true, children }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className={"panel" + (open ? " open" : "")}>
      <div className="panel-head" onClick={() => setOpen(o => !o)}>
        <span className="caret">▸</span>
        <span className="ptitle">{icon} {title}</span>
        {file ? <span className="pfile">{file}</span> : null}
        {meta ? <span className="pmeta">{meta}</span> : null}
      </div>
      {error && open ? <div className="panel-err">{error}</div> : null}
      <div className={"panel-body" + (open ? "" : " collapsed")}>{children}</div>
    </div>
  );
}

/* ---- diff body ---- */
function renderHl(text, hl, signClass) {
  // text includes the leading +/-/space; split off sign for coloring
  const sign = text.slice(0, 1);
  const rest = text.slice(1);
  if (!hl) {
    return (<>{signClass ? <span className="sign">{sign}</span> : sign}{rest}</>);
  }
  // hl is [start,end] into `rest`
  const [s, e] = hl;
  const hlClass = signClass === "del" ? "hl-del" : "hl-add";
  return (
    <>
      {signClass ? <span className="sign">{sign}</span> : sign}
      {rest.slice(0, s)}
      <span className={hlClass}>{rest.slice(s, e)}</span>
      {rest.slice(e)}
    </>
  );
}

function Diff({ title, file, error, lines }) {
  return (
    <Panel icon="←" title={title} file={file} error={error}>
      <div className="diff">
        {lines.map((ln, i) => {
          const cls = "row " + ln.t;
          let signClass = ln.t === "add" ? "add" : ln.t === "del" ? "del" : null;
          return (
            <span className={cls} key={i}>
              {renderHl(ln.text, ln.hl, signClass)}
            </span>
          );
        })}
      </div>
    </Panel>
  );
}

/* ---- write (code listing with line numbers + syntax) ---- */
function CodeLine({ tokens }) {
  return (
    <>
      {tokens.map((t, i) => {
        const [cls, txt] = t;
        return <span key={i} className={cls ? "tk-" + cls : undefined}>{txt}</span>;
      })}
    </>
  );
}

function Write({ title, file, lines }) {
  return (
    <Panel icon="#" title={title} file={file}>
      <div className="code">
        {lines.map((toks, i) => (
          <div key={i}>
            <span className="ln">{i + 1}</span>
            <CodeLine tokens={toks} />
          </div>
        ))}
      </div>
    </Panel>
  );
}

/* ---- bash output ---- */
function Bash({ title, cmd, out }) {
  return (
    <Panel icon="#" title={title} defaultOpen={true}>
      <div className="bash">
        <div className="cmd"><span className="dollar">$</span>{cmd}</div>
        <div style={{ height: 6 }} />
        {out.map((l, i) => (
          <div key={i} className={"out " + (l.c || "")}>{l.t || "\u00a0"}</div>
        ))}
      </div>
    </Panel>
  );
}

/* ---- todos ---- */
function Todos({ title, items }) {
  const box = { done: "[✓]", doing: "[•]", pend: "[ ]" };
  return (
    <div className="block">
      <div className="toolrow"><span className="glyph">#</span>{title}</div>
      <div style={{ height: 6 }} />
      <div className="todos">
        {items.map((it, i) => (
          <div key={i} className={"t " + it.state}>
            <span className="box">{box[it.state]}</span>
            <span className="txt">{it.text}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ---- subagent ---- */
function SubAgent({ kind2, name, lines, state }) {
  const [tick, setTick] = useState(0);
  React.useEffect(() => {
    if (state !== "running") return;
    const id = setInterval(() => setTick(t => t + 1), 220);
    return () => clearInterval(id);
  }, [state]);
  const spin = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"][tick % 10];
  return (
    <div className={"subagent " + (state === "running" ? "running" : "")}>
      <div className="head">
        {state === "running" ? <span className="spin">{spin} </span> : "│ "}
        <span className="kind">{kind2}</span> — {name}
      </div>
      <div className="meta">{lines.length} toolcalls · {state === "running" ? "running…" : "1.2s"}</div>
      <div style={{ height: 6 }} />
      {lines.map((l, i) => (
        <div key={i} className={"out " + (l.c || "")} style={{ color: "var(--fg-faint)" }}>{l.t}</div>
      ))}
    </div>
  );
}

/* ---- summary + table ---- */
function Summary({ title, prose, rows, footer }) {
  return (
    <div className="block">
      <div className="md" style={{ marginBottom: 10 }}>
        <h3 style={{ color: "var(--purple)", margin: "0 0 10px" }}>{title}</h3>
      </div>
      <div className="md" dangerouslySetInnerHTML={{ __html: prose }} />
      <table className="sum">
        <thead><tr><th>File</th><th>Change</th></tr></thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}><td className="file">{r.file}</td><td className="chg">{r.chg}</td></tr>
          ))}
        </tbody>
      </table>
      <div className="md" style={{ marginTop: 12 }}>
        <p style={{ color: "var(--fg)" }}>{footer.split("—")[0]}—
          <span style={{ color: "var(--green)" }}>{footer.split("—")[1]}</span>
        </p>
      </div>
    </div>
  );
}

/* ---- bottom tasks dock (tasks.md / issue board) ---- */
function TasksDock({ data, open, onToggle, onPick }) {
  const [sel, setSel] = useState(0);
  const order = { doing: 0, blocked: 1, review: 2, todo: 3, done: 4 };
  const items = [...data.items].sort((a, b) => (order[a.status] - order[b.status]) || (b.id - a.id));
  const openCount = items.filter(t => t.status !== "done").length;
  const statusLabel = { todo: "todo", doing: "in progress", review: "in review", blocked: "blocked", done: "done" };
  return (
    <div className={"tasks-dock" + (open ? "" : " collapsed")}>
      <div className="tasks-head" onClick={onToggle}>
        <span className="caret">▸</span>
        <span className="th-title">Tasks</span>
        <span className="th-src">{data.source}</span>
        <span className="th-count"><b>{openCount}</b> open</span>
        <span className="th-spacer" />
        <span className="th-hint"><kbd>ctrl+x t</kbd> toggle</span>
      </div>
      <div className="tasks-body">
        <table className="tasks">
          <thead>
            <tr>
              <th className="c-id">#</th>
              <th className="c-status">Status</th>
              <th className="c-title">Task</th>
              <th className="c-labels">Labels</th>
              <th className="c-assignee">Owner</th>
            </tr>
          </thead>
          <tbody>
            {items.map((t, i) => (
              <tr key={t.id} className={i === sel ? "sel" : ""}
                onMouseEnter={() => setSel(i)}
                onClick={() => onPick && onPick(t)}>
                <td className="c-id"><span className="hash">#</span>{t.id}</td>
                <td className="c-status"><span className={"tbadge " + t.status}>{statusLabel[t.status]}</span></td>
                <td className="c-title">{t.title}</td>
                <td className="c-labels">{t.labels.map((l, j) => <span key={j} className="tlabel">{l}</span>)}</td>
                <td className="c-assignee">{t.assignee}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/* export to window for other babel scripts */
Object.assign(window, {
  Sidebar, StatusBar, UserTurn, Thought, Markdown, ToolRow,
  Panel, Diff, Write, Bash, Todos, SubAgent, Summary, TasksDock,
});
