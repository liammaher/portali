// Fills a portal pratica inside the Claude browser pane from a steps file, with no model
// call per field: run.py page writes install.js (this file and the procedure's steps, no
// personal data) and run.js (the person's values), each run in the pane with one call.
//
// The portal reloads on every save, which would end a script running in it, so the
// runner stays in the top page and drives the portal in a same-origin frame over it (the
// portal sends no X-Frame-Options or frame-ancestors). It waits on PrimeFaces' own ajax
// queue instead of fixed pauses, sets dropdowns through their widgets (the same change
// event and server call a click makes), reads every field back after each save and
// again on a fresh load of the draft before it says `filled`, and never opens a
// declaration or clicks what is the person's: payment, signing, sending.
// A file is attached by building it in the page and handing it to the file box as a
// drop would (DataTransfer): the pane can't type a path into one, and a public page
// can't fetch from localhost, so its bytes come in run.js (run.py says how big).
//
//   __pv.start(procedure, values[, resume])  starts a run, returns at once. resume picks a
//                                  stopped run up: {url, from, kept, fixes} (run.py --resume)
//   await __pv.wait(ms)            the run's report once it ends, or after ms
//   __pv.open()                    loads the frame's page as the top page, for the person
(() => {
  class StepError extends Error {}
  const fail = (m) => { throw new StepError(m); };
  // The pane slows timers to a few ticks a second; message events aren't slowed.
  const channel = new MessageChannel(), waiting = [];
  channel.port1.onmessage = () => waiting.shift()();
  const yieldTask = () => new Promise((r) => { waiting.push(r); channel.port2.postMessage(0); });
  const sleep = async (ms) => { const end = performance.now() + ms; do await yieldTask(); while (performance.now() < end); };
  const norm = (s) => (s || "").normalize("NFKD").replace(/[^\x00-\x7f]/g, "")
    .replace(/\s+/g, " ").trim().toLowerCase();

  let steps, frame, report, kept;
  const win = () => frame.contentWindow;
  const doc = () => frame.contentDocument;
  const el = (id) => doc().querySelector(`[id$="dynaForm:${id}"]`);
  const widgets = () => Object.values((win().PrimeFaces || {}).widgets || {}); // none: a plain form
  const widgetOf = (e) => widgets().find((w) => w && w.input && w.input[0] === e);

  const busy = () => {
    const w = win(), d = doc();
    return !d || d.readyState !== "complete" || (w.jQuery && w.jQuery.active > 0) ||
      !!(w.PrimeFaces && w.PrimeFaces.ajax && !w.PrimeFaces.ajax.Queue.isEmpty());
  };

  async function idle(ms = 20000) {
    const end = performance.now() + ms;
    for (let quiet = 0; quiet < 3;) {
      if (performance.now() > end) fail("the portal stayed busy");
      quiet = busy() ? 0 : quiet + 1;
      await sleep(30);
    }
  }

  // Runs a click and waits for what it starts, until 300 ms pass with no ajax in flight
  // and no page loading: a load can start another (the last wizard screen redirects).
  async function act(click, ms = 60000) {
    const end = performance.now() + ms;
    let loads = 0, leaving = null;
    const onload = () => { loads++; };
    const watch = () => win().addEventListener("beforeunload", () => { leaving = loads; });
    frame.addEventListener("load", onload);
    watch();
    click();
    for (let quiet = 0; quiet < 6;) {
      if (performance.now() > end) fail("the portal didn't answer");
      await sleep(50);
      if (leaving !== null) {
        while (loads <= leaving) {
          if (performance.now() > end) fail("the page didn't load");
          await sleep(30);
        }
        leaving = null;
        watch();
        quiet = 0;
        continue;
      }
      quiet = busy() ? 0 : quiet + 1;
    }
    frame.removeEventListener("load", onload);
  }

  function button(text) {
    const b = [...doc().querySelectorAll("button, a, input[type=submit]")]
      .find((e) => e.offsetParent && norm(e.textContent || e.value) === norm(text));
    if (!b) fail(`no ${text} button`);
    if (steps.forbidden.test([b.textContent, b.value, b.title].join(" "))) {
      fail(`refused to click ${text}: that step is the person's`);
    }
    return b;
  }

  // ".error-message-text" is the portal's message under a field ("Il valore è obbligatorio").
  // `theirs`: a section's fields the person fills (a statement in their name); the
  // portal's "required" under one of them isn't the run's error.
  const errors = (theirs = []) => [...doc().querySelectorAll(".ui-message-error, .ui-messages-error, .ui-growl-message-error, [id*=\"dynaForm\"] .error-message-text")]
    .filter((e) => e.getClientRects().length && norm(e.textContent))
    .filter((e) => { const box = e.closest("td") && e.closest("td").querySelector("input, select, textarea");
      return !(box && theirs.some((t) => box.id.endsWith("dynaForm:" + t) ||
        win().eval(`(${steps.label_js})`)(box).some((x) => norm(x).includes(norm(t))))); })
    .map((e) => norm(e.textContent));

  function checkLabel(e, label) {
    if (!label) return;
    const texts = win().eval(`(${steps.label_js})`)(e);
    if (!texts.some((t) => norm(t).includes(norm(label)))) {
      fail(`expected the label ${label} by this field, found ${JSON.stringify(texts.slice(0, 3))}`);
    }
  }

  function fire(e) {
    for (const t of ["input", "change"]) e.dispatchEvent(new (win().Event)(t, { bubbles: true }));
  }

  // A script's click reaches e even under a banner, a dialog or a re-render's leftovers,
  // where a person's couldn't: the page is then in a state the steps file doesn't know,
  // so stop there. e must still be in the page and be what's on top at its centre.
  function onTop(e, what) {
    if (!e.isConnected) fail(`${what}: the page changed under it`);
    e.scrollIntoView({ block: "center", inline: "nearest" });
    const r = e.getBoundingClientRect();
    if (!r.width || !r.height) fail(`${what}: not visible`);
    // A label that wraps is several line boxes: the middle of the whole can fall between two
    // lines, on its parent. Any line's middle on it will do.
    const at = (b) => doc().elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    const mine = (h) => h && (h === e || e.contains(h) || h.control === e);
    const lines = [...e.getClientRects()].filter((b) => b.width && b.height);
    const hit = at(r);
    if (!mine(hit) && !lines.some((b) => mine(at(b))) && hit) {
      const name = hit.id ? `#${hit.id}` : hit.className ? `.${String(hit.className).split(" ")[0]}` : "";
      fail(`${what}: covered by ${hit.tagName.toLowerCase()}${name} "${norm(hit.innerText).slice(0, 60)}"`);
    }
  }

  function press(e, what) {
    onTop(e, what);
    e.click();
  }

  // wizard

  async function tree(item) {
    // A name, or {leaf, nth} for a name the tree has under several parents (Napoli's SUAP
    // lists "Acconciatore, barbiere, ..." twice).
    const { leaf, nth } = typeof item === "string" ? { leaf: item, nth: 0 } : item;
    const node = () => {
      const s = [...doc().querySelectorAll(".ui-tree .tree-content > span")].filter((x) => x.textContent.trim() === leaf);
      if (nth ? s.length < nth : s.length !== 1) fail(`tree: ${s.length} nodes named ${leaf}`);
      return s[nth ? nth - 1 : 0];
    };
    // Open its ancestors, outermost first, then select it.
    const keys = [];
    for (let li = node().closest("li.ui-treenode"); (li = li.parentElement && li.parentElement.closest("li.ui-treenode"));) {
      keys.unshift(li.dataset.rowkey);
    }
    for (const k of keys) {
      const c = doc().querySelector(`li[data-rowkey="${k}"] > .ui-treenode-content`);
      if (c.getAttribute("aria-expanded") !== "true") {
        press(c.querySelector(".ui-treenode-label"), `tree: ${leaf}`);
        await idle();
      }
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      press(node(), `tree: ${leaf}`);
      await idle();
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      fail(`tree: ${leaf} didn't stay selected`);
    }
  }

  async function wizard(step, value, wi) {
    report.at = { phase: "wizard", index: wi, screen: step.screen };
    if (!norm(doc().body.innerText).includes(norm(step.screen))) fail(`expected the screen ${step.screen}`);
    if (step.fill) {
      const e = doc().querySelector(step.fill.css) || fail(`no field ${step.fill.css}`);
      e.focus();
      e.value = value;
      fire(e);
      e.blur();
      await idle();
    }
    for (const leaf of [].concat(step.tree || [])) await tree(leaf);
    // A Si/No question in the tree ("Le opere ricadono nell'ambito della Sanatoria ...?").
    for (const [question, answer] of Object.entries(step.answers || {})) {
      const q = [...doc().querySelectorAll('table[id$=":sinCond"]')]
        .filter((t) => norm(t.closest("li, tr, div").innerText).includes(norm(question)));
      if (q.length !== 1) fail(`${q.length} Si/No questions ${question}`);
      const td = [...q[0].querySelectorAll("td")].find((c) => norm(c.innerText) === norm(answer)) || fail(`${question}: no ${answer}`);
      press(td.querySelector(".ui-radiobutton-box") || td.querySelector("input"), `${question}: ${answer}`);
      await idle();
    }
    // The page's text lists every screen in its menu, so a screen is told by its title: the
    // same title after CONFERMA means the portal refused it.
    const title = () => norm((doc().querySelector(".content-title") || {}).innerText);
    const before = title();
    await act(() => press(button(step.confirm), step.confirm));
    if (before && title() === before) {
      const why = [...doc().querySelectorAll("span, div")].filter((e) => !e.children.length && e.offsetParent && /obbligatori|errore/i.test(e.textContent));
      fail(`still on ${step.screen} after ${step.confirm}: ${why.map((e) => norm(e.textContent)).join("; ") || "no message"}`);
    }
  }

  // form sections

  // A street's suggestion list can open again after its pick and lie over the next boxes.
  async function closeLists() {
    const open = [...doc().querySelectorAll(".ui-autocomplete-panel")].filter((p) => p.offsetParent);
    for (const p of open) {
      const w = widgets().find((x) => x && x.panel && x.panel[0] === p);
      if (w && w.hide) w.hide(); else p.style.display = "none";
    }
    if (open.length) await idle();
  }

  async function text(f, value) {
    const e = el(f.id) || fail(`${f.id}: no field`);
    checkLabel(e, f.label);
    await closeLists();
    onTop(e, f.label || f.id);
    const w = widgetOf(e);
    e.focus();
    if (w && typeof w.setValue === "function") w.setValue(value);
    else e.value = value;
    fire(e);
    e.blur();
    await idle();
    return value;
  }

  async function select(f, value) {
    const sel = el(f.id + ":combo_input") || fail(`${f.id}: no dropdown`);
    checkLabel(el(f.id + ":combo_label"), f.label);
    const opts = [...sel.options];
    const i = opts.findIndex((o) => o.value === value || o.text.trim().toUpperCase() === value.toUpperCase());
    if (i < 0) fail(`${f.id}: no option ${value}`);
    if (sel.value !== opts[i].value) {
      const w = widgetOf(sel) || fail(`${f.id}: no dropdown widget`);
      if (!w.items) { // its list is built when it first opens
        w.callHandleMethod(null, null);
        await idle();
      }
      // selectValue is silent in PrimeFaces 11; selectItem fires the change the portal
      // listens to (a province loads its comuni).
      w.selectItem(w.items.eq(i));
      await idle();
    }
    if (el(f.id + ":combo_input").value !== opts[i].value) fail(`${f.id}: ${value} didn't stay selected`);
    return opts[i].value;
  }

  async function tick(f) {
    const e = el(f.id) || fail(`${f.id}: no box`);
    checkLabel(e, f.label);
    if (!e.checked) {
      const wrap = e.closest(".ui-radiobutton, .ui-chkbox");
      press(wrap && wrap.querySelector(".ui-radiobutton-box, .ui-chkbox-box") || e, f.label || f.id);
      await idle();
    }
    // A box that opens rows renumbers itself: find a label-named one again.
    const now = el(f.id) || (f.spec && el(resolve(f.spec).id));
    if (!now || !now.checked) fail(`${f.id}: didn't stay ticked`);
    return "checked";
  }

  async function lookup(f, value) {
    const parts = value.split(/\s+/);
    const [dug, street] = parts.length > 1 && steps.dug.includes(parts[0].toUpperCase())
      ? [parts[0].toUpperCase(), parts.slice(1).join(" ")] : ["ALTRO", value];
    const words = street.toUpperCase().split(/\W+/).filter((w) => w.length > 2);
    const e = el(f.id) || fail(`${f.id}: no field`);
    checkLabel(e, f.label);
    const query = words.reduce((a, b) => (b.length > a.length ? b : a));
    e.value = query;
    (widgetOf(e) || fail(`${f.id}: no lookup widget`)).search(query);
    await idle();
    const lis = [...el(f.id.replace(/_input$/, "_panel")).querySelectorAll("li")];
    const items = lis.map((l) => norm(l.textContent).toUpperCase());
    const hits = items.filter((t) => t.split(" ")[0] === dug && words.every((w) => t.split(" ").includes(w)));
    if (hits.length !== 1) fail(`${f.id}: ${value} matches ${JSON.stringify(hits)} among ${items.length}`);
    press(lis[items.indexOf(hits[0])], `${f.id}: ${hits[0]}`);
    await idle();
    // The list can stay open a moment over the next boxes: wait for it, else close it.
    const panel = el(f.id.replace(/_input$/, "_panel"));
    for (const end = performance.now() + 3000; performance.now() < end && panel.offsetParent;) await sleep(50);
    if (panel.offsetParent) { widgetOf(e).hide(); await idle(); }
    return hits[0];
  }

  // v is {name, type, b64}. The portal's own change handler then uploads it, as when a
  // person picks the file.
  async function attach(f, v) {
    const e = (f.css ? doc().querySelector(f.css) : el(f.id)) || fail(`${f.css || f.id}: no file box`);
    if (e.type !== "file") fail(`${f.css || f.id}: not a file box`);
    checkLabel(e, f.label);
    const w = win(), bytes = Uint8Array.from(w.atob(v.b64), (c) => c.charCodeAt(0));
    const dt = new w.DataTransfer();
    dt.items.add(new w.File([bytes], v.name, { type: v.type }));
    e.files = dt.files;
    fire(e);
    await idle();
    // An upload that isn't jQuery's doesn't show as busy: give it until its list names
    // the file. A portal that names it only after Salva costs these 10 s once.
    for (const end = performance.now() + 10000; performance.now() < end &&
      !norm(doc().body.innerText).includes(norm(v.name));) await sleep(100);
    // Some slots ("Allegati liberi") want a description next to the uploaded file: a box
    // with no id of its own in the slot's cell.
    if (f.describe) {
      const box = [...(el(f.id) || e).closest("td").querySelectorAll("input[type=text], textarea")].find((x) => !/dropzone/.test(x.id));
      if (!box) fail(`${f.label}: no description box`);
      box.focus(); box.value = f.describe; fire(box); box.blur();
      await idle();
    }
    return v.name;
  }

  function readBack(f) {
    if (f.spec) f = { ...f, id: resolve(f.spec).id }; // rows added since may have renumbered it
    else if (f.id && f.label && !el(f.kind === "select" ? f.id + ":combo_input" : f.id)) {
      f = { ...f, id: resolve({ kind: f.kind, label: f.label, nth: f.nth }).id }; // as when it was filled
    }
    if (f.kind === "file") {
      // An upload widget empties its box once it has sent the file; its list names it.
      const got = norm(doc().body.innerText).includes(norm(f.typed)) ? f.typed : null;
      return { label: f.label || f.css || f.id, want: f.typed, got, ok: got !== null };
    }
    if (f.kind === "radio" || f.kind === "checkbox") {
      const got = el(f.id) && el(f.id).checked ? "checked" : "unchecked";
      return { label: f.label, want: "checked", got, ok: got === "checked" };
    }
    const e = el(f.kind === "select" ? f.id + ":combo_input" : f.id);
    const got = e ? e.value : null;
    let ok = norm(got) === norm(f.typed);
    if (!ok && f.kind === "number") ok = parseFloat(got) === parseFloat(f.typed);
    return { label: f.label || f.id, want: f.typed, got, ok };
  }

  // A field named by its label instead of an id: the portal numbers ids by row, so they
  // shift when a choice or a "+" adds rows. `anchor` + `block` pick the block-th repeat of
  // a group (the first field of each repeat carries the anchor label); `nth` the n-th field
  // with that label inside it. Returns the id, as the steps file's `id` would be.
  function shown(e) {
    return !!(e.offsetParent || (e.closest(".ui-radiobutton, .ui-chkbox, .ui-selectonemenu") || {}).offsetParent);
  }
  function kindOf(e) {
    if (e.tagName === "SELECT") return "select";
    if (e.type === "radio" || e.type === "checkbox" || e.type === "file") return e.type === "checkbox" ? "checkbox" : e.type;
    if (/txtws_input$/.test(e.id)) return "lookup";
    return "text";
  }
  function inputs() {
    return [...doc().querySelectorAll('[id*="dynaForm:"]')].filter((e) => /^(INPUT|SELECT|TEXTAREA)$/.test(e.tagName) &&
      e.type !== "hidden" && !/combo_focus$|combo_filter$|editor_file_dropzone$/.test(e.id) && shown(e));
  }
  // A box's validation message ("Il valore è obbligatorio", as its title and in its cell) is
  // not its label.
  const MESSAGE = /^(il valore e obbligatorio|deve essere selezionata)/;
  // Where label is among the box's first texts (0: its own), or -1.
  function labelRank(e, label) {
    const texts = win().eval(`(${steps.label_js})`)(e);
    if (e.title) texts.unshift(e.title);
    // Only the messages in front: an empty cell keeps its place, or the label of the box to
    // the left would count as this box's.
    while (texts.length && MESSAGE.test(norm(texts[0]))) texts.shift();
    // A text said twice (the title "mq" and its cell "mq") counts once, so the third is the
    // label beside it ("Superficie di somministrazione").
    return texts.filter((t, i) => !t || texts.indexOf(t) === i).slice(0, 3).findIndex((t) => norm(t).startsWith(norm(label)));
  }
  const labelled = (e, label) => labelRank(e, label) >= 0;
  function resolve(f) {
    if (f.id || f.css) return f;
    let list = inputs();
    if (f.anchor) {
      const starts = list.map((e, i) => (labelled(e, f.anchor) ? i : -1)).filter((i) => i >= 0);
      const b = (f.block || 1) - 1;
      if (b >= starts.length) fail(`${f.label}: no block ${f.block} of ${f.anchor} (found ${starts.length})`);
      list = list.slice(starts[b], starts[b + 1]);
    }
    const want = f.kind === "number" || f.kind === "date" ? "text" : f.kind;
    let hits = list.filter((e) => kindOf(e) === want && labelled(e, f.label));
    // With no nth, a box whose own text is the label before one that only has it further
    // off: in a row of three boxes, the last column's label is the one before each in the page.
    if (!f.nth && hits.length > 1) {
      const best = Math.min(...hits.map((e) => labelRank(e, f.label)));
      hits = hits.filter((e) => labelRank(e, f.label) === best);
    }
    const e = hits[(f.nth || 1) - 1] || fail(`${f.label}: ${hits.length} fields with this label${f.anchor ? ` in block ${f.block || 1}` : ""}`);
    return { ...f, id: e.id.split("dynaForm:")[1].replace(/:combo_input$/, "") };
  }
  async function addBlock(f) {
    const count = () => inputs().filter((e) => labelled(e, f.anchor)).length;
    const before = count();
    // `block`: the block this "+" is for. A resumed draft may have it already.
    if (f.block && before >= f.block) return `block ${f.block}`;
    const plus = [...doc().querySelectorAll('[id*="dynaForm"] button')].filter((b) => shown(b) && b.textContent.trim() === "+");
    if (!plus.length) fail(`no + to add a block of ${f.anchor}`);
    press(plus[plus.length - 1], `+ ${f.anchor}`);
    await idle();
    if (count() !== before + 1) fail(`+ didn't add a block of ${f.anchor}`);
    return `block ${before + 1}`;
  }

  // The open section has no link: a reopened draft shows the one it was left on.
  const isOpen = (name) => {
    const cur = doc().querySelector("li#corrente");
    return !!cur && norm(cur.innerText).startsWith(norm(name));
  };

  async function section(sec, values, si) {
    const t0 = performance.now();
    report.at = { phase: "section", index: si, section: sec.section };
    if (steps.declarations.test(sec.section)) fail(`refused to open ${sec.section}: the person's declarations`);
    if (sec.go) {
      // A screen after the form (Pagamento oneri, Allega documenti): reached with its button,
      // unless a resumed draft is already on it.
      const here = () => norm((doc().querySelector(".content-title") || {}).innerText).includes(norm(sec.section));
      if (!here()) await act(() => press(button(sec.go), sec.go));
      if (!here()) fail(`expected the screen ${sec.section}`);
    } else if (sec.open !== false) {
      const a = [...doc().querySelectorAll("a.linkmenu")].find((x) => norm(x.textContent).startsWith(norm(sec.section)));
      if (a) await act(() => press(a, sec.section));
      else if (!isOpen(sec.section)) fail(`no section ${sec.section}`);
    }
    const done = [];
    for (let [i, f] of sec.fields.entries()) {
      const v = values[i];
      if (v === false) continue; // its `when` is false for this pratica
      report.at.field = i;
      report.at.label = f.label || f.id || f.css;
      let typed;
      if (f.kind === "add") { await addBlock(f); continue; }
      // An id a combination of choices moved (one the crawl didn't try together) is found
      // again by its label, held to the same checks.
      if (f.id && f.label && !el(f.kind === "select" ? f.id + ":combo_input" : f.id)) {
        const { id, ...byLabel } = f;
        f = byLabel;
      }
      if (!f.id && !f.css && f.if_present) {
        // A box only some pratiche show: skipped where this one doesn't.
        try { f = { ...resolve(f), spec: f }; } catch (e) { if (e instanceof StepError) continue; throw e; }
      }
      f = f.id || f.css ? f : { ...resolve(f), spec: f };
      if (f.kind === "radio" || f.kind === "checkbox") typed = await tick(f);
      else if (v === null || v === "") continue;
      else if (f.kind === "select") typed = await select(f, v);
      else if (f.kind === "lookup") typed = await lookup(f, v);
      else if (f.kind === "file") typed = await attach(f, v);
      else typed = await text(f, v);
      done.push({ ...f, kind: f.kind === "date" ? "text" : f.kind, typed, field: i });
    }
    delete report.at.field;
    delete report.at.label;
    // An attachments screen may keep each file as it uploads, with no Salva (save: false).
    if (sec.save !== false) await act(() => press(button(sec.save || "Salva"), sec.save || "Salva"));
    const errs = errors(sec.theirs);
    const checks = done.map(readBack);
    const bad = checks.filter((c) => !c.ok);
    report.sections.push({ section: sec.section, seconds: +((performance.now() - t0) / 1000).toFixed(1), errors: errs, bad });
    if (errs.length || bad.length) fail(`${sec.section}: portal errors ${JSON.stringify(errs)}, not kept ${JSON.stringify(bad)}`);
    keep(si, done);
  }

  // What each finished section typed, in the report too: a run resumed after a stop hands
  // it back, so the fresh load of the draft still checks every section, not only its own.
  function keep(si, done) {
    kept = kept.filter((k) => k.sec !== steps.sections[si]).concat([{ sec: steps.sections[si], done }]);
    report.kept = (report.kept || []).filter((k) => k.section !== si)
      .concat([{ section: si, fields: done.map((d) => [d.field, d.typed]) }]);
  }

  // Each read-back above saw its section right after its own Salva. A later section can
  // re-render an earlier one, and a portal can show a value it never stored, so the
  // draft is loaded again from its address and every value read once more.
  async function verify() {
    const t0 = performance.now();
    // Going to the same address plus a #section only scrolls, so drop that part; a route
    // after # (#/…, #!…) is the page itself, so reload it instead.
    const u = new (win().URL)(doc().location.href);
    const route = /^#[!/]/.test(u.hash);
    if (!route) u.hash = "";
    await act(() => (route ? win().location.reload() : win().location.replace(u.href)));
    // Every section is saved by now: the person can open the draft from Pratiche in bozza.
    if (/\/cmd\/error/.test(doc().location.href)) fail("fresh load of the draft: the portal's error page (the sections are saved; open the draft from Pratiche in bozza)");
    const bad = [];
    for (const { sec, done } of kept.filter((k) => !k.sec.go)) {
      const a = [...doc().querySelectorAll("a.linkmenu")].find((x) => norm(x.textContent).startsWith(norm(sec.section)));
      if (a) await act(() => press(a, sec.section));
      else if (sec.open !== false && !isOpen(sec.section)) fail(`fresh load of the draft: no section ${sec.section}`);
      for (const c of done.map(readBack)) if (!c.ok) bad.push({ section: sec.section, ...c });
    }
    report.verify_seconds = +((performance.now() - t0) / 1000).toFixed(1);
    if (bad.length) fail(`fresh load of the draft: not kept ${JSON.stringify(bad)}`);
  }

  async function run(values, resume) {
    const t0 = performance.now();
    const secs = () => +((performance.now() - t0) / 1000).toFixed(1);
    try {
      frame = document.createElement("iframe");
      frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
      const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
      // A resumed run opens the draft it stopped on; a new one starts a new pratica.
      frame.src = resume.url || steps.start;
      document.body.append(frame);
      await loaded;
      await idle();
      if (!doc().location.href.startsWith(steps.logged_in)) fail("not logged in to the portal");
      if (/\/cmd\/error/.test(doc().location.href)) fail("the draft's address has expired: open it again from Pratiche in bozza");
      const from = resume.from || {};
      for (const k of resume.kept || []) {
        keep(k.section, k.fields.map(([field, typed]) => {
          const def = steps.sections[k.section].fields[field];
          return { ...def, typed, field, ...(def.id || def.css ? {} : { spec: def }) };
        }));
      }
      if (from.section === undefined) {
        for (const [i, w] of steps.wizard.entries()) if (i >= (from.wizard || 0)) await wizard(w, values.wizard[i], i);
      }
      report.wizard_seconds = secs();
      for (const [i, s] of steps.sections.entries()) {
        if (!s.go && i >= (from.section || 0)) await section(s, values.sections[i], i);
      }
      if (steps.sections.some((s, i) => !s.go && i >= (from.section || 0)) || !steps.sections.some((s) => s.go)) await verify();
      for (const [i, s] of steps.sections.entries()) {
        if (s.go && i >= (from.section || 0)) await section(s, values.sections[i], i);
      }
      report.result = "filled";
      report.at = null;
    } catch (e) {
      report.result = "stopped";
      report.error = e instanceof StepError ? e.message : `${e.name}: ${e.message}`;
    } finally {
      report.total_seconds = secs();
      report.url = frame && doc() ? doc().location.href : null;
    }
  }

  window.__pv = {
    start(procedure, values, resume = {}) {
      const raw = localStorage.getItem("pv-steps:" + procedure) || fail(`no steps installed for ${procedure}`);
      steps = JSON.parse(raw);
      steps.forbidden = new RegExp(steps.forbidden, "i");
      steps.declarations = new RegExp(steps.declarations, "i");
      // A fix is what Claude found the page really has for one field (a moved id, a renamed
      // label): this run uses it, and run.py writes it down for the steps file.
      for (const x of resume.fixes || []) Object.assign(steps.sections[x.section].fields[x.field], x.set);
      report = { procedure, result: "running", sections: [], at: null, resumed: !!resume.url };
      kept = [];
      run(values, resume);
      return "started";
    },
    async wait(ms = 25000) {
      const end = performance.now() + ms;
      while (report && report.result === "running" && performance.now() < end) await sleep(200);
      return report;
    },
    open() {
      location.href = doc().location.href;
    },
  };
})();
