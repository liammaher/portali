// Maps a portal draft's sections by itself, in the Claude browser pane: what each section
// shows, then each choice in it tried one at a time (a radio, a checkbox, a short dropdown)
// to see which fields it opens, undone with the section's Annulla. Never presses Salva, never
// opens the dichiarante's declarations or ticks an acknowledgement, never uploads, pays or
// sends. Like inpage.js it drives the portal in a same-origin frame over the page, so the
// portal's reloads don't end it.
//
//   __crawl.start({sections: [names] | null, depth: 2})  starts, returns at once
//   await __crawl.wait(ms)                               the map so far, once done or after ms
(() => {
  const DECLARATIONS = /consapevolezza|informativa sulla privacy|requisiti di o|altre dichiarazioni|dichiarazioni ai sensi dell'art\. 6|rispetto sulle tasse|art\. ?75/i;
  // A checkbox never ticked, even for a moment: an acknowledgement in the person's name.
  // Radios aren't held to it: "dichiara" starts many factual choices (d.1 names "la
  // dichiarazione del progettista"), and the declarations' own sections are never opened.
  const PERSONAL = /presa visione|consapevol|privacy|autorizz|sottoscritt/i;
  const channel = new MessageChannel(), waiting = [];
  channel.port1.onmessage = () => waiting.shift()();
  const tick = () => new Promise((r) => { waiting.push(r); channel.port2.postMessage(0); });
  const sleep = async (ms) => { const end = performance.now() + ms; do await tick(); while (performance.now() < end); };
  const norm = (s) => (s || "").normalize("NFKD").replace(/[^\x00-\x7f]/g, "").replace(/\s+/g, " ").trim().toLowerCase();

  let frame, report;
  const win = () => frame.contentWindow, doc = () => frame.contentDocument;
  const busy = () => {
    const w = win(), d = doc();
    return !d || d.readyState !== "complete" || (w.jQuery && w.jQuery.active > 0) ||
      !!(w.PrimeFaces && w.PrimeFaces.ajax && !w.PrimeFaces.ajax.Queue.isEmpty());
  };
  async function idle(ms = 30000) {
    const end = performance.now() + ms;
    for (let q = 0; q < 3;) { if (performance.now() > end) throw new Error("portal busy"); q = busy() ? 0 : q + 1; await sleep(40); }
  }
  async function act(fn) {
    let loads = 0;
    const onload = () => loads++;
    frame.addEventListener("load", onload);
    fn();
    await sleep(150);
    for (let q = 0; q < 5;) { q = busy() ? 0 : q + 1; await sleep(60); }
    frame.removeEventListener("load", onload);
    return loads;
  }

  const widgetOf = (e) => Object.values((win().PrimeFaces || {}).widgets || {}).find((w) => w && w.input && w.input[0] === e);
  function labelOf(el) {
    const texts = [];
    const t = el.closest("[title]"); if (t && t !== el) texts.push(t.title);
    const td = el.closest("td");
    if (td) {
      texts.push(td.innerText);
      for (let tr = td.parentElement.previousElementSibling; tr; tr = tr.previousElementSibling) {
        const c = tr.cells[td.cellIndex]; if (c && c.innerText.trim()) { texts.push(c.innerText); break; }
      }
      for (let c = td.previousElementSibling; c; c = c.previousElementSibling) if (c.innerText.trim()) { texts.push(c.innerText); break; }
    }
    return texts.map((s) => s.replace(/\s+/g, " ").trim()).filter((s) => s && !/^Selezionare un valore/.test(s) && s !== "ui-button");
  }
  function kindOf(e) {
    if (e.tagName === "SELECT") return "select";
    if (e.tagName === "TEXTAREA") return "textarea";
    if (e.type === "radio" || e.type === "checkbox" || e.type === "file") return e.type;
    if (/txtws_input$/.test(e.id)) return "lookup";
    if (/calendar_input$/.test(e.id)) return "date";
    return "text";
  }
  // The section's fields as they are now. A field's key is its kind and label, which survive
  // the renumbering a choice causes; its id is what the steps file needs in this state.
  function snap() {
    const out = [];
    for (const e of doc().querySelectorAll('[id*="dynaForm:"]')) {
      if (!/^(INPUT|SELECT|TEXTAREA)$/.test(e.tagName) || e.type === "hidden") continue;
      if (/combo_focus$|combo_filter$|editor_file_dropzone$/.test(e.id)) continue;
      const shown = e.offsetParent || (e.closest(".ui-radiobutton, .ui-chkbox, .ui-selectonemenu") || {}).offsetParent;
      if (!shown) continue;
      const kind = kindOf(e), labels = labelOf(e);
      const f = { id: e.id.split("dynaForm:")[1].replace(/:combo_input$/, ""), kind, label: (labels[0] || "").slice(0, 160) };
      if (labels[1] && labels[1] !== labels[0]) f.context = labels[1].slice(0, 120);
      if (kind === "select") {
        f.options = e.options.length > 40 ? `${e.options.length} options` : [...e.options].filter((o) => o.value).map((o) => o.text.trim());
        if (e.value) f.value = e.options[e.selectedIndex].text.trim();
      } else if (kind === "radio" || kind === "checkbox") { if (e.checked) f.checked = true; } else if (e.value && kind !== "file") f.value = e.value;
      if (kind === "file") { const t = e.closest("td"); f.accepts = ((t && t.innerText.match(/Tipologie ammesse:[^\n]*/)) || [""])[0]; }
      out.push(f);
    }
    for (const x of doc().querySelectorAll('[id*="dynaForm"] .error-message-text, [id*="dynaForm"] .alert-message-text')) {
      if (x.innerText.trim()) out.push({ kind: "message", label: x.innerText.trim() });
    }
    return out;
  }
  const key = (f) => `${f.kind}|${norm(f.label).slice(0, 90)}|${norm(f.context || "").slice(0, 40)}`;

  // The choices a person makes in this state: radios and checkboxes not yet set, and the
  // options of a short dropdown. Statements in the person's name are left alone.
  function choices(fields) {
    const out = [];
    for (const f of fields) {
      if (f.kind === "radio" && !f.checked) out.push({ id: f.id, kind: f.kind, label: f.label });
      if (f.kind === "checkbox" && !f.checked && !PERSONAL.test(f.label)) out.push({ id: f.id, kind: f.kind, label: f.label });
      if (f.kind === "select" && Array.isArray(f.options) && f.options.length <= 12) {
        for (const o of f.options) if (o !== f.value) out.push({ id: f.id, kind: "select", label: f.label, option: o });
      }
    }
    return out;
  }
  const el = (id) => doc().querySelector(`[id$="dynaForm:${id}"]`);
  async function apply(c) {
    if (c.kind === "select") {
      const sel = el(c.id + ":combo_input"), w = widgetOf(sel);
      if (!w) throw new Error(`no widget for ${c.id}`);
      if (!w.items) { w.callHandleMethod(null, null); await idle(); }
      const i = [...sel.options].findIndex((o) => o.text.trim() === c.option);
      w.selectItem(w.items.eq(i));
    } else {
      const e = el(c.id);
      if (!e) throw new Error(`no box ${c.id}`);
      const wrap = e.closest(".ui-radiobutton, .ui-chkbox");
      (wrap && wrap.querySelector(".ui-radiobutton-box, .ui-chkbox-box") || e).click();
    }
    await idle();
  }
  const button = (text) => [...doc().querySelectorAll("button")].find((b) => b.offsetParent && b.textContent.trim() === text);
  async function undo() {
    const b = button("Annulla");
    if (!b) throw new Error("no Annulla");
    await act(() => b.click());
  }
  async function open(name) {
    const a = [...doc().querySelectorAll("a.linkmenu")].find((x) => norm(x.textContent).startsWith(norm(name)));
    if (!a) return false; // already open, or not in this pratica
    await act(() => a.click());
    return true;
  }
  const current = () => {
    const li = doc().querySelector("li#corrente");
    return li ? li.innerText.trim() : "";
  };

  // What applying `path` (a list of choices, in order) shows beyond `before`.
  async function explore(path, before, depth, out) {
    for (const c of path) await apply(c);
    const now = snap();
    const seen = new Set(before.map(key));
    const reveals = now.filter((f) => !seen.has(key(f)));
    const keep = new Map(before.map((f) => [key(f), f.id]));
    const moved = now.filter((f) => keep.has(key(f)) && keep.get(key(f)) !== f.id).map((f) => [keep.get(key(f)), f.id]);
    const last = path[path.length - 1];
    const node = { choice: last.option ? `${last.label} = ${last.option}` : last.label, id: last.id, kind: last.kind };
    if (last.option) node.option = last.option;
    if (reveals.length) node.reveals = reveals;
    if (moved.length) node.moved = moved;
    out.push(node);
    report.tried++;
    await undo();
    if (depth > 1 && reveals.length) {
      const sub = choices(reveals);
      if (sub.length) {
        node.then = [];
        for (const c of sub.slice(0, 12)) {
          // Re-apply the parent after the undo: the ids it opens are those of `now`.
          await explore([...path, c], now, depth - 1, node.then);
        }
      }
    }
  }

  async function crawl(opts) {
    const t0 = performance.now();
    frame = document.createElement("iframe");
    frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
    const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
    frame.src = location.href;
    document.body.append(frame);
    await loaded;
    await idle();
    const names = opts.sections || [...doc().querySelectorAll("a.linkmenu")].map((a) => a.textContent.trim());
    for (const name of names) {
      const s0 = performance.now();
      const sec = { section: name };
      report.sections.push(sec);
      if (DECLARATIONS.test(name)) { sec.skipped = "the person's declarations"; continue; }
      try {
        await open(name);
        if (!norm(current()).startsWith(norm(name).slice(0, 20))) throw new Error(`opened ${current()}`);
        sec.fields = snap();
        sec.branches = [];
        const cs = choices(sec.fields);
        for (const c of cs.slice(0, opts.limit || 30)) await explore([c], sec.fields, opts.depth || 2, sec.branches);
        if (cs.length > (opts.limit || 30)) sec.truncated = cs.length;
      } catch (e) {
        sec.error = String(e.message || e);
        try { await undo(); } catch (_) { /* nothing to undo */ }
      }
      sec.seconds = +((performance.now() - s0) / 1000).toFixed(1);
    }
    report.seconds = +((performance.now() - t0) / 1000).toFixed(1);
    report.done = true;
    frame.remove();
  }

  window.__crawl = {
    start(opts = {}) {
      report = { started: new Date().toISOString(), sections: [], tried: 0, done: false };
      crawl(opts).catch((e) => { report.error = String(e.message || e); report.done = true; });
      return "started";
    },
    async wait(ms = 25000) {
      const end = performance.now() + ms;
      while (report && !report.done && performance.now() < end) await sleep(250);
      return report;
    },
  };
})();
