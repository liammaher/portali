// Maps a portal draft's sections by itself, in the Claude browser pane: what each section
// shows, then each choice in it tried one at a time (a radio, a checkbox, a short dropdown)
// to see which fields it opens, undone with the section's Annulla. Never presses Salva, never
// opens the dichiarante's declarations or ticks an acknowledgement, never uploads, pays or
// sends. Like inpage.js it drives the portal in a same-origin frame over the page, so the
// portal's reloads don't end it.
//
//   __crawl.start({url, sections: [names] | null, depth: 2})  starts, returns at once
//   await __crawl.wait(ms)                               the map so far, once done or after ms
//   __crawl.slots()                                      the attachments screen's slots
//
// A choice's node says what it reveals, hides and moves (old id → new id); gen.py reads them.
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
    for (let q = 0; q < 5;) {
      // "Proseguire senza salvare?" after a section left half undone: the one dialog answered,
      // since the crawler saves nothing anyway.
      const dialog = [...doc().querySelectorAll(".ui-dialog")].find((x) => x.offsetParent || getComputedStyle(x).display !== "none");
      if (dialog && /non sono state salvate[\s\S]*proseguire senza salvare/i.test(dialog.innerText) && !dialog.dataset.pvAnswered) {
        const ok = [...dialog.querySelectorAll("button")].find((b) => norm(b.textContent) === "conferma");
        if (ok) { dialog.dataset.pvAnswered = "1"; ok.click(); q = 0; }
      }
      q = busy() ? 0 : q + 1; await sleep(60);
    }
    frame.removeEventListener("load", onload);
    return loads;
  }

  // By the element, else by the id: a section just reloaded can hold the old widget's entry.
  const widgetOf = (e) => {
    const ws = Object.values((win().PrimeFaces || {}).widgets || {});
    return ws.find((w) => w && w.input && w.input[0] === e) || ws.find((w) => w && w.id === e.id.replace(/_input$/, ""));
  };
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
    return texts.map((s) => s.replace(/\s+/g, " ").trim()).filter((s) => s && !/^Selezionare un valore/.test(s) && !/^(ui-button|true|false)$/.test(s));
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
      // A radio's group: its name without the row ids, which shift.
      if (kind === "radio") f.group = (e.name || "").split("dynaForm:")[1] || e.name;
      if (labels[1] && labels[1] !== labels[0]) f.context = labels[1].slice(0, 120);
      if (kind === "select") {
        f.options = e.options.length > 40 ? `${e.options.length} options` : [...e.options].filter((o) => o.value).map((o) => o.text.trim());
        if (e.value) f.value = e.options[e.selectedIndex].text.trim();
      } else if (kind === "radio" || kind === "checkbox") { if (e.checked) f.checked = true; } else if (e.value && kind !== "file") f.prefilled = true; // never its text: the login's name and CF
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
      const sel = el(c.id + ":combo_input");
      let w = widgetOf(sel);
      if (!w) { await idle(); await sleep(300); w = widgetOf(sel); }
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
    // Scheda anagrafica reloads the whole page, which act() can't see starting.
    for (let end = performance.now() + 10000; performance.now() < end && !norm(current()).startsWith(norm(name).slice(0, 20));) await sleep(200);
    await idle();
    return true;
  }
  // The open section's title: li#corrente also holds the section's form.
  const current = () => {
    const li = doc().querySelector("li#corrente");
    const h = li && (li.querySelector(".headerDynaFormControl") || li.querySelector("a, span"));
    return h ? h.innerText.trim() : li ? li.innerText.trim().split("\n")[0] : "";
  };

  // What applying `path` (a list of choices, in order) shows beyond `before`.
  async function explore(path, before, depth, out) {
    for (const c of path) await apply(c);
    const now = snap();
    const seen = new Set(before.map(key));
    const reveals = now.filter((f) => !seen.has(key(f)));
    const keep = new Map(before.map((f) => [key(f), f.id]));
    const moved = now.filter((f) => keep.has(key(f)) && keep.get(key(f)) !== f.id).map((f) => [keep.get(key(f)), f.id]);
    // What the choice hides: fields of `before` with no counterpart now (counted per key).
    const left = new Map();
    for (const f of now) left.set(key(f), (left.get(key(f)) || 0) + 1);
    const hides = before.filter((f) => { const n = left.get(key(f)) || 0; left.set(key(f), n - 1); return n <= 0 && f.kind !== "message"; }).map((f) => f.id);
    const last = path[path.length - 1];
    const node = { choice: last.option ? `${last.label} = ${last.option}` : last.label, id: last.id, kind: last.kind };
    if (last.option) node.option = last.option;
    if (reveals.length) node.reveals = reveals;
    if (moved.length) node.moved = moved;
    if (hides.length) node.hides = hides;
    // A "+" open in this state repeats a block: press it once, note what it adds.
    if (depth > 1) {
      const plus = [...doc().querySelectorAll('[id*="dynaForm"] button')].filter((b) => b.offsetParent && b.textContent.trim() === "+");
      if (plus.length) {
        await act(() => plus[plus.length - 1].click());
        const after = snap();
        const had = new Set(now.map(key));
        const adds = after.filter((f) => !had.has(key(f)) || after.filter((g) => key(g) === key(f)).length > now.filter((g) => key(g) === key(f)).length);
        node.plus = { anchor: (adds.find((f) => f.kind !== "message") || {}).label, adds: adds.length };
      }
    }
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
    if (frame) frame.remove();
    frame = document.createElement("iframe");
    frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
    const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
    frame.src = opts.url || location.href;
    document.body.append(frame);
    await loaded;
    await idle();
    // The open section has no link of its own: it goes first.
    const names = opts.sections || [current(), ...[...doc().querySelectorAll("a.linkmenu")].map((a) => a.textContent.trim())].filter(Boolean);
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
  }

  // The attachments screen (Allega documenti): each slot's title, the types it takes, and
  // whether the portal marks it required.
  function slots() {
    return [...doc().querySelectorAll('input[type=file]')].map((e) => {
      let row = e.closest("tr");
      for (let k = 0; k < 6 && row && !row.querySelector("td"); k++) row = row.parentElement.closest("tr");
      // The title is the row above, or, for a document the portal makes, the one above its
      // "Scarica il documento" link.
      let title = "", portal = false;
      for (let r = row && row.previousElementSibling; r; r = r.previousElementSibling) {
        const t = r.innerText.replace(/\s+/g, " ").trim();
        if (/^Scarica il documento$/.test(t)) { portal = true; continue; }
        if (t) { title = t; break; }
      }
      const cell = e.closest("td");
      return { id: e.id.split("dynaForm:")[1], title: title.slice(0, 160),
        accepts: ((cell && cell.innerText.match(/Tipologie ammesse:\s*([^\n]*)/)) || [])[1] || "",
        required: !!(row && /obbligatori/i.test(row.innerText)), portal_document: portal };
    });
  }

  window.__crawl = {
    slots: () => { frame = frame || document.querySelector("iframe"); return slots(); },
    report: () => report,
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
