/* ============================================================
   OPCODE42 TUI — main application
   ============================================================ */
const { useState: uS, useEffect: uE, useRef: uR, useCallback } = React;
const F = window.OPCODE42;

const sleep = (ms) => new Promise(r => setTimeout(r, ms));

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "font": "Consolas",
  "density": "regular",
  "accent": "#6fa8dc",
  "scanlines": false
}/*EDITMODE-END*/;

const FONT_STACKS = {
  "Consolas":     '"Consolas", "Cascadia Mono", ui-monospace, monospace',
  "JetBrains Mono": '"JetBrains Mono", ui-monospace, monospace',
  "IBM Plex Mono":  '"IBM Plex Mono", ui-monospace, monospace',
  "SF Mono":      '"SF Mono", ui-monospace, Menlo, monospace',
};

/* delays per event kind (ms before it appears) */
const DELAY = {
  thought: 620, md: 520, rule: 160, tool: 240,
  diff: 760, write: 680, bash: 760, todos: 460,
  subagent: 460, summary: 620,
};

function App() {
  const [screen, setScreen] = uS("splash");      // splash | session
  const [blocks, setBlocks] = uS([]);            // revealed stream blocks
  const [streaming, setStreaming] = uS(false);
  const [input, setInput] = uS("");
  const [modal, setModal] = uS(null);
  const [toast, setToast] = uS(null);

  // settings
  const [mode, setMode] = uS("build");
  const [model, setModel] = uS("Claude Opus 4.8");
  const [provider, setProvider] = uS("Anthropic");
  const [theme, setTheme] = uS("opcode42");
  const [sidebarHidden, setSidebarHidden] = uS(false);
  const [crt, setCrt] = uS(false);
  const [tasksOpen, setTasksOpen] = uS(true);
  const [tasksHidden, setTasksHidden] = uS(false);

  // live context counters
  const [ctx, setCtx] = uS({ tokens: 0, pct: 0, cost: "0.00" });
  const [subRunning, setSubRunning] = uS(false);

  // composer autocomplete
  const [ac, setAc] = uS(null);   // { type:'slash'|'mention', items, sel, q }

  // tweaks
  const [t, setTweak] = useTweaks(TWEAK_DEFAULTS);
  uE(() => {
    const root = document.documentElement;
    root.style.setProperty("--mono", FONT_STACKS[t.font] || FONT_STACKS.Consolas);
    root.style.setProperty("--accent", t.accent);
    root.setAttribute("data-density", t.density === "regular" ? "" : t.density);
  }, [t.font, t.accent, t.density]);
  uE(() => { setCrt(!!t.scanlines); }, [t.scanlines]);

  // ---- splash: bottom margin of the tasks board = textbox height ----
  uE(() => {
    if (screen !== "splash") return;
    const apply = () => {
      const box = document.querySelector(".splash .composer");
      const wrap = document.querySelector(".splash-dock-wrap");
      if (box && wrap) wrap.style.marginBottom = box.offsetHeight + "px";
    };
    apply();
    const id = setTimeout(apply, 60);
    window.addEventListener("resize", apply);
    return () => { clearTimeout(id); window.removeEventListener("resize", apply); };
  }, [screen, tasksOpen, tasksHidden, t.density, t.font]);

  // ---- seed a specific screen from ?view= (used by the Screens gallery) ----
  uE(() => {
    const view = new URLSearchParams(location.search).get("view");
    if (!view || view === "splash") return;
    const finalBlocks = [{ k: "user", text: F.SEED_PROMPT.text, mention: F.SEED_PROMPT.mention }];
    F.EVENTS.forEach(ev => {
      if (ev.kind === "subagent") finalBlocks.push({ ...ev, state: "done", lines: [...ev.lines, { c: "dim", t: "src/sync/poll.ts → flagged for follow-up" }] });
      else finalBlocks.push(ev);
    });
    setBlocks(finalBlocks);
    setScreen("session");
    setStreaming(false);
    setSubRunning(false);
    setCtx({ tokens: 34910, pct: 4, cost: "0.16" });

    const modalViews = ["palette", "models", "themes", "sessions", "agents", "timeline", "status"];
    if (modalViews.includes(view)) setTimeout(() => setModal(view), 80);

    // scroll-position views are handled by the canonical scroll effect (timer-free)
    if (["chat", "tools", "output", "summary"].includes(view)) seedRef.current = view;
  }, []);

  const streamRef = uR(null);
  const cancelRef = uR(false);
  const leaderRef = uR(false);    // ctrl+x leader pressed
  const taRef = uR(null);
  const seedRef = uR(null);       // seeded gallery view → scroll target

  const showToast = useCallback((msg) => {
    setToast(msg);
    setTimeout(() => setToast(null), 1900);
  }, []);

  /* ---- auto-scroll on new block; honor a seeded gallery scroll target ---- */
  uE(() => {
    const el = streamRef.current;
    if (!el) return;
    el.style.scrollBehavior = "auto";
    const v = seedRef.current;
    const bring = (target, pad = 16) => {
      if (!target) { el.scrollTop = 0; return; }
      el.scrollTop += target.getBoundingClientRect().top - el.getBoundingClientRect().top - pad;
    };
    if (v === "chat") el.scrollTop = 0;
    else if (v === "tools") bring(el.querySelector(".diff") && el.querySelector(".diff").closest(".panel"));
    else if (v === "output") {
      const b = [...el.querySelectorAll(".panel-head .ptitle")].find(e => e.textContent.includes("Run"));
      bring(b && b.closest(".panel"));
    }
    else el.scrollTop = el.scrollHeight;   // summary + live streaming
  }, [blocks, streaming]);

  /* ---- the scripted run ---- */
  const runSession = useCallback(async (promptText, promptMention) => {
    cancelRef.current = false;
    setScreen("session");
    setBlocks([{ k: "user", text: promptText, mention: promptMention }]);
    setStreaming(true);
    setCtx({ tokens: 2140, pct: 1, cost: "0.00" });

    let tok = 2140, cost = 0;
    for (const ev of F.EVENTS) {
      if (cancelRef.current) return;
      await sleep(DELAY[ev.kind] || 300);
      if (cancelRef.current) return;
      setBlocks(b => [...b, ev]);
      if (ev.kind === "subagent" && ev.state === "running") setSubRunning(true);
      // grow context
      tok += Math.floor(900 + Math.random() * 2600);
      cost += 0.004 + Math.random() * 0.02;
      setCtx({ tokens: tok, pct: Math.min(42, Math.round(tok / 8000)), cost: cost.toFixed(2) });
    }
    // finish: resolve subagent
    await sleep(700);
    if (cancelRef.current) return;
    setSubRunning(false);
    setBlocks(b => b.map(x => x.kind === "subagent" ? { ...x, state: "done", lines: [...x.lines, { c: "dim", t: "src/sync/poll.ts → flagged for follow-up" }] } : x));
    setStreaming(false);
  }, []);

  /* ---- follow-up (after first run) brief ack ---- */
  const runFollowup = useCallback(async (text) => {
    cancelRef.current = false;
    setBlocks(b => [...b, { k: "user", text }]);
    setStreaming(true);
    await sleep(560);
    setBlocks(b => [...b, { kind: "thought", ms: 410 }]);
    await sleep(520);
    setBlocks(b => [...b, { kind: "md", html:
      `<p>On it — re-running the suite and updating the README with the retry semantics now.</p>` }]);
    await sleep(420);
    setBlocks(b => [...b, { kind: "tool", glyph: "→", label: "Edit", path: "README.md", meta: "· +6 lines" }]);
    await sleep(620);
    setBlocks(b => [...b, { kind: "md", html: `<p style="color:var(--green)">Documented. Anything else?</p>` }]);
    setStreaming(false);
  }, []);

  /* ---- submit composer ---- */
  const submit = useCallback(() => {
    const text = input.trim();
    // slash command that maps to an action
    if (text.startsWith("/")) {
      const cmd = text.split(/\s+/)[0];
      const map = { "/models": "models", "/themes": "themes", "/agents": "agents",
        "/sessions": "sessions", "/new": "new", "/exit": "exit" };
      if (map[cmd]) { setInput(""); setAc(null); doAction(map[cmd]); return; }
    }
    setInput(""); setAc(null);
    if (!text) {
      if (screen === "splash") runSession(F.SEED_PROMPT.text, F.SEED_PROMPT.mention);
      return;
    }
    if (screen === "splash") {
      // strip an @mention out of the typed text for display
      const m = text.match(/@(\S+)/);
      runSession(m ? text.replace(/@\S+/, "").trim() : text, m ? m[1] : null);
    } else {
      runFollowup(text);
    }
  }, [input, screen, runSession, runFollowup]);

  /* ---- actions dispatched from palette / commands / leader keys ---- */
  const doAction = useCallback((action) => {
    setModal(null);
    switch (action) {
      case "models": setModal("models"); break;
      case "themes": setModal("themes"); break;
      case "agents": setModal("agents"); break;
      case "sessions": setModal("sessions"); break;
      case "timeline": setModal("timeline"); break;
      case "status": setModal("status"); break;
      case "palette": setModal("palette"); break;
      case "sidebar": setSidebarHidden(h => !h); break;
      case "tasks": setTasksHidden(h => !h); break;
      case "crt": setCrt(c => !c); break;
      case "new":
        cancelRef.current = true; setStreaming(false); setSubRunning(false);
        setBlocks([]); setScreen("splash"); setCtx({ tokens: 0, pct: 0, cost: "0.00" });
        showToast("Started a new session"); break;
      case "exit": showToast("/exit — (prototype stays open)"); break;
      case "toast": showToast("Command not wired in this prototype"); break;
      default: break;
    }
  }, [showToast]);

  /* ---- global keyboard ---- */
  uE(() => {
    function onKey(e) {
      // leader: ctrl+x then a key
      if (leaderRef.current) {
        leaderRef.current = false;
        const k = e.key.toLowerCase();
        const lead = { l: "sessions", n: "new", m: "models", a: "agents",
          g: "timeline", b: "sidebar", t: "tasks", h: "crt", y: "toast", c: "toast" };
        if (lead[k]) { e.preventDefault(); doAction(lead[k]); return; }
        return;
      }
      if (e.ctrlKey && e.key === "x") { e.preventDefault(); leaderRef.current = true;
        showToast("ctrl+x · l sessions  n new  m model  a agent  g timeline  b sidebar  t tasks"); return; }
      if (e.ctrlKey && e.key === "p") { e.preventDefault(); setModal(m => m === "palette" ? null : "palette"); return; }
      if (e.ctrlKey && e.key === "c" && streaming) { e.preventDefault(); cancelRef.current = true; setStreaming(false); showToast("Interrupted"); return; }
      // tab opens agents (when not in textarea autocomplete)
      if (e.key === "Tab" && !ac && document.activeElement !== taRef.current) {
        e.preventDefault(); setModal("agents"); return;
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [doAction, showToast, streaming, ac]);

  /* ---- composer key handling (autocomplete + submit) ---- */
  const onComposerKey = useCallback((e) => {
    if (ac) {
      if (e.key === "ArrowDown") { e.preventDefault(); setAc(a => ({ ...a, sel: Math.min(a.items.length - 1, a.sel + 1) })); return; }
      if (e.key === "ArrowUp") { e.preventDefault(); setAc(a => ({ ...a, sel: Math.max(0, a.sel - 1) })); return; }
      if (e.key === "Escape") { e.preventDefault(); setAc(null); return; }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        const it = ac.items[ac.sel];
        if (!it) { setAc(null); return; }
        if (ac.type === "slash") {
          // replace whole input with the command
          setInput(it.cmd + " ");
          setAc(null);
          // if it maps to a modal, fire after a tick
          const map = { "/models": "models", "/themes": "themes", "/agents": "agents",
            "/sessions": "sessions", "/new": "new", "/diff": "toast", "/editor": "toast",
            "/help": "toast", "/share": "toast", "/connect": "models", "/compact": "toast",
            "/init": "toast", "/exit": "exit" };
          if (e.key === "Enter" && map[it.cmd]) { setInput(""); doAction(map[it.cmd]); }
          return;
        } else {
          // mention: insert file path
          setInput(prev => prev.replace(/@\S*$/, "@" + it + " "));
          setAc(null); return;
        }
      }
    }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  }, [ac, submit, doAction]);

  /* ---- update autocomplete as input changes ---- */
  const onInput = useCallback((val) => {
    setInput(val);
    if (val.startsWith("/")) {
      const q = val.slice(1).toLowerCase();
      const items = F.SLASH.filter(s => s.cmd.slice(1).startsWith(q.split(/\s/)[0]));
      setAc(items.length ? { type: "slash", items, sel: 0 } : { type: "slash", items: [], sel: 0 });
    } else {
      const m = val.match(/@(\S*)$/);
      if (m) {
        const q = m[1].toLowerCase();
        const items = F.FILES.filter(f => f.toLowerCase().includes(q));
        setAc({ type: "mention", items, sel: 0 });
      } else {
        setAc(null);
      }
    }
  }, []);

  /* auto-grow textarea */
  uE(() => {
    const ta = taRef.current;
    if (ta) { ta.style.height = "auto"; ta.style.height = Math.min(160, ta.scrollHeight) + "px"; }
  }, [input]);

  /* apply theme/crt to dom */
  uE(() => { document.querySelector(".term")?.setAttribute("data-crt", crt ? "on" : "off"); }, [crt]);

  /* ---- render one stream block ---- */
  function renderBlock(b, i) {
    const reveal = "reveal";
    if (b.k === "user") return <div className={"block " + reveal} key={i}><UserTurn text={b.text} mention={b.mention} /></div>;
    switch (b.kind) {
      case "thought": return <div className={reveal} key={i}><Thought ms={b.ms} /></div>;
      case "md": return <div className={"block " + reveal} key={i}><Markdown html={b.html} /></div>;
      case "rule": return <div className="rule" key={i} />;
      case "tool": return <div className={reveal} key={i}><ToolRow glyph={b.glyph} label={b.label} path={b.path} meta={b.meta} /></div>;
      case "diff": return <div className={reveal} key={i}><Diff {...b} /></div>;
      case "write": return <div className={reveal} key={i}><Write {...b} /></div>;
      case "bash": return <div className={reveal} key={i}><Bash {...b} /></div>;
      case "todos": return <div className={reveal} key={i}><Todos {...b} /></div>;
      case "subagent": return <div className={reveal} key={i}><SubAgent {...b} /></div>;
      case "summary": return <div className={reveal} key={i}><Summary {...b} /></div>;
      default: return null;
    }
  }

  /* ---- composer (shared by splash + session) ---- */
  const composer = (
    <div className="composer-wrap">
      {ac && (
        <div className="ac">
          {ac.items.length === 0 && <div className="ac-empty">No matching items</div>}
          {ac.type === "slash" && ac.items.map((it, i) => (
            <div key={i} className={"ac-item" + (i === ac.sel ? " sel" : "")}
              onMouseEnter={() => setAc(a => ({ ...a, sel: i }))}
              onMouseDown={(e) => { e.preventDefault(); setInput(it.cmd + " "); setAc(null); taRef.current?.focus(); }}>
              <span className="cmd"><b>{it.cmd}</b></span><span>{it.desc}</span>
            </div>
          ))}
          {ac.type === "mention" && ac.items.map((it, i) => (
            <div key={i} className={"ac-item" + (i === ac.sel ? " sel" : "")}
              onMouseEnter={() => setAc(a => ({ ...a, sel: i }))}
              onMouseDown={(e) => { e.preventDefault(); setInput(prev => prev.replace(/@\S*$/, "@" + it + " ")); setAc(null); taRef.current?.focus(); }}>
              <span className="cmd">{it}</span>
            </div>
          ))}
        </div>
      )}
      <div className="composer">
        <div className="input-line">
          <textarea
            ref={taRef}
            rows={1}
            value={input}
            placeholder={screen === "splash" ? 'Ask anything…  "Fix a TODO in the codebase"' : "Reply, or / for commands"}
            onChange={(e) => onInput(e.target.value)}
            onKeyDown={onComposerKey}
            autoFocus
            spellCheck={false}
          />
        </div>
        <div className="modeline">
          <span className="mode">{mode[0].toUpperCase() + mode.slice(1)}</span>
          <span className="sep">·</span>
          <span>{model}</span>&nbsp;
          <span className="prov">{provider}</span>
        </div>
      </div>
      <div className="composer-hints">
        <span><kbd>tab</kbd> agents</span>
        <span><kbd>ctrl+p</kbd> commands</span>
        {screen !== "splash" && <span><kbd>ctrl+x</kbd> leader</span>}
      </div>
    </div>
  );

  return (
    <div className="term" data-crt={crt ? "on" : "off"}>
      <div className="body-row">
        <div className="main">
          {screen === "splash" ? (
            <div className="splash">
              <div className="wordmark">opcode42</div>
              {composer}
              {!tasksHidden && (
                <div className={"splash-dock-wrap" + (tasksOpen ? "" : " collapsed")}>
                  <TasksDock
                    data={F.TASKS}
                    open={tasksOpen}
                    onToggle={() => setTasksOpen(o => !o)}
                    onPick={(t) => showToast("#" + t.id + " · " + t.title)}
                  />
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="stream" ref={streamRef}>
                <div className="stream-inner">
                  {blocks.map(renderBlock)}
                  {streaming && (
                    <div className="toolrow" style={{ marginTop: 4 }}>
                      <span className="cursor" />
                    </div>
                  )}
                </div>
              </div>
              {composer}
              {!tasksHidden && (
                <TasksDock
                  data={F.TASKS}
                  open={tasksOpen}
                  onToggle={() => setTasksOpen(o => !o)}
                  onPick={(t) => showToast("#" + t.id + " · " + t.title)}
                />
              )}
            </>
          )}
        </div>

        {screen === "session" && (
          <Sidebar
            title={F.SESSION_TITLE}
            ctx={ctx}
            hidden={sidebarHidden}
            model={model}
            subagentRunning={subRunning}
          />
        )}
      </div>

      <StatusBar
        mode={mode[0].toUpperCase() + mode.slice(1)}
        model={model}
        provider={provider}
        tokens={ctx.tokens ? (ctx.tokens / 1000).toFixed(1) + "K" : "0"}
      />

      {/* ---- modals ---- */}
      {modal === "palette" && (
        <PaletteModal data={F.PALETTE} onClose={() => setModal(null)}
          onPick={(it) => doAction(it.action)} />
      )}
      {modal === "models" && (
        <ModelsModal data={F.MODELS} onClose={() => setModal(null)}
          onPick={(m) => { setModel(m.name); setProvider(m.prov); setModal(null); showToast("Model → " + m.name); }} />
      )}
      {modal === "themes" && (
        <ThemesModal items={F.THEMES} current={theme} onClose={() => setModal(null)}
          onPick={(t) => { setTheme(t); setModal(null); showToast("Theme → " + t); }} />
      )}
      {modal === "agents" && (
        <AgentsModal items={F.AGENTS} onClose={() => setModal(null)}
          onPick={(a) => { setMode(a.name); setModal(null); showToast("Agent → " + a.name); }} />
      )}
      {modal === "sessions" && (
        <SessionsModal groups={F.SESSIONS} onClose={() => setModal(null)}
          onPick={(s) => { setModal(null); showToast(s.current ? "Already on this session" : "Switched → " + s.title); }} />
      )}
      {modal === "timeline" && (
        <TimelineModal items={F.TIMELINE} onClose={() => setModal(null)}
          onPick={() => { setModal(null); showToast("Jumped to message"); }} />
      )}
      {modal === "status" && <StatusModal onClose={() => setModal(null)} />}

      <TweaksPanel title="Tweaks">
        <TweakSection label="Type" />
        <TweakSelect label="Font" value={t.font}
          options={["Consolas", "JetBrains Mono", "IBM Plex Mono", "SF Mono"]}
          onChange={(v) => setTweak("font", v)} />
        <TweakRadio label="Density" value={t.density}
          options={["compact", "regular", "cozy"]}
          onChange={(v) => setTweak("density", v)} />
        <TweakSection label="Accent" />
        <TweakColor label="Color" value={t.accent}
          options={["#6fa8dc", "#8cc265", "#d99a4e", "#b08cd4", "#5fb3c4"]}
          onChange={(v) => setTweak("accent", v)} />
        <TweakSection label="Display" />
        <TweakToggle label="Scanlines (CRT)" value={t.scanlines}
          onChange={(v) => setTweak("scanlines", v)} />
      </TweaksPanel>

      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
