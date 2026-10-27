// Fills a portal pratica inside the Claude browser pane from a steps file, with no model
// call per field: run.py page writes install.js (this file and the procedure's steps, no
// personal data) and run.js (the person's values), each run in the pane with one call.
//
// The portal reloads on every save, which would end a script running in it, so the
// runner stays in the top page and drives the portal in a same-origin frame over it (the
// portal sends no X-Frame-Options or frame-ancestors). It waits on PrimeFaces' own ajax
// queue instead of fixed pauses, sets dropdowns through their widgets (the same change
// event and server call a click makes), reads every field back after each save, and
// never opens a declaration or clicks what is the person's: payment, signing, sending.
//
//   __pv.start(procedure, values)  starts a run, returns at once
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

  let steps, frame, report;
  const win = () => frame.contentWindow;
  const doc = () => frame.contentDocument;
  const el = (id) => doc().querySelector(`[id$="dynaForm:${id}"]`);
  const widgets = () => Object.values(win().PrimeFaces.widgets);
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

  const errors = () => [...doc().querySelectorAll(".ui-message-error, .ui-messages-error, .ui-growl-message-error")]
    .filter((e) => e.getClientRects().length).map((e) => norm(e.textContent));

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

  // wizard

  async function tree(leaf) {
    const node = () => {
      const s = [...doc().querySelectorAll(".ui-tree .tree-content > span")].filter((x) => x.textContent.trim() === leaf);
      if (s.length !== 1) fail(`tree: ${s.length} nodes named ${leaf}`);
      return s[0];
    };
    // Open its ancestors, outermost first, then select it.
    const keys = [];
    for (let li = node().closest("li.ui-treenode"); (li = li.parentElement && li.parentElement.closest("li.ui-treenode"));) {
      keys.unshift(li.dataset.rowkey);
    }
    for (const k of keys) {
      const c = doc().querySelector(`li[data-rowkey="${k}"] > .ui-treenode-content`);
      if (c.getAttribute("aria-expanded") !== "true") {
        c.querySelector(".ui-treenode-label").click();
        await idle();
      }
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      node().click();
      await idle();
    }
    if (!node().closest("li.ui-treenode").classList.contains("ui-treenode-selected")) {
      fail(`tree: ${leaf} didn't stay selected`);
    }
  }

  async function wizard(step, value) {
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
    await act(() => button(step.confirm).click());
  }

  // form sections

  async function text(f, value) {
    const e = el(f.id) || fail(`${f.id}: no field`);
    checkLabel(e, f.label);
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
      (wrap && wrap.querySelector(".ui-radiobutton-box, .ui-chkbox-box") || e).click();
      await idle();
    }
    if (!el(f.id).checked) fail(`${f.id}: didn't stay ticked`);
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
    lis[items.indexOf(hits[0])].click();
    await idle();
    return hits[0];
  }

  function readBack(f) {
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

  async function section(sec, values) {
    const t0 = performance.now();
    if (steps.declarations.test(sec.section)) fail(`refused to open ${sec.section}: the person's declarations`);
    if (sec.open !== false) {
      const a = [...doc().querySelectorAll("a.linkmenu")].find((x) => norm(x.textContent).startsWith(norm(sec.section)));
      if (!a) fail(`no section ${sec.section}`);
      await act(() => a.click());
    }
    const done = [];
    for (const [i, f] of sec.fields.entries()) {
      const v = values[i];
      if (v === false) continue; // its `when` is false for this pratica
      let typed;
      if (f.kind === "radio" || f.kind === "checkbox") typed = await tick(f);
      else if (v === null || v === "") continue;
      else if (f.kind === "select") typed = await select(f, v);
      else if (f.kind === "lookup") typed = await lookup(f, v);
      else typed = await text(f, v);
      done.push({ ...f, typed });
    }
    await act(() => button("Salva").click());
    const errs = errors();
    const checks = done.map(readBack);
    const bad = checks.filter((c) => !c.ok);
    report.sections.push({ section: sec.section, seconds: +((performance.now() - t0) / 1000).toFixed(1), errors: errs, bad });
    if (errs.length || bad.length) fail(`${sec.section}: portal errors ${JSON.stringify(errs)}, not kept ${JSON.stringify(bad)}`);
  }

  async function run(values) {
    const t0 = performance.now();
    const secs = () => +((performance.now() - t0) / 1000).toFixed(1);
    try {
      frame = document.createElement("iframe");
      frame.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;border:0;z-index:2147483647;background:#fff";
      const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
      frame.src = steps.start;
      document.body.append(frame);
      await loaded;
      await idle();
      if (!doc().location.href.startsWith(steps.logged_in)) fail("not logged in to the portal");
      for (const [i, w] of steps.wizard.entries()) await wizard(w, values.wizard[i]);
      report.wizard_seconds = secs();
      for (const [i, s] of steps.sections.entries()) await section(s, values.sections[i]);
      report.result = "filled";
    } catch (e) {
      report.result = "stopped";
      report.error = e instanceof StepError ? e.message : `${e.name}: ${e.message}`;
    } finally {
      report.total_seconds = secs();
      report.url = frame && doc() ? doc().location.href : null;
    }
  }

  window.__pv = {
    start(procedure, values) {
      const raw = localStorage.getItem("pv-steps:" + procedure) || fail(`no steps installed for ${procedure}`);
      steps = JSON.parse(raw);
      steps.forbidden = new RegExp(steps.forbidden, "i");
      steps.declarations = new RegExp(steps.declarations, "i");
      report = { procedure, result: "running", sections: [] };
      run(values);
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
