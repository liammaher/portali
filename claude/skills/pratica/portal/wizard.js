// Maps a comune's whole wizard on impresainungiorno, in the Claude browser pane: every
// sector, every operation under it, and for each operation alone the screens that follow
// (interventi eventuali, destinatari, scelte eventuali) and the pratica the portal generates
// (its adempimenti and sections). Starts on a scratch draft's "Controlla l'accesso" screen,
// takes the defaults past the operations (no interventi, the preselected destinatario, no
// scelte), and steps back with "Pagina precedente". Never fills or saves a section, never
// goes past the generated pratica. Like inpage.js it drives a frame over the page.
//
//   __wizard.start({draft, sectors: [names] | null, only: [operation labels] | null, limit, variants, follow})
//   await __wizard.wait(ms)                    the map so far, once done or after ms
(() => {
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
  async function idle(ms = 60000) {
    const end = performance.now() + ms;
    for (let q = 0; q < 4;) { if (performance.now() > end) throw new Error("portal busy"); q = busy() ? 0 : q + 1; await sleep(50); }
  }
  // A wizard button posts, then the portal redirects to the next screen: wait until the
  // screen has changed and settled, or an error shows on this one.
  async function go(fn, ms = 45000) {
    const before = doc().location.href + "|" + title();
    let loads = 0;
    const onload = () => loads++;
    frame.addEventListener("load", onload);
    fn();
    const end = performance.now() + ms;
    for (;;) {
      await sleep(120);
      if (performance.now() > end) { frame.removeEventListener("load", onload); throw new Error(`no change after a click on ${title()}`); }
      if (busy()) continue;
      // "Proseguire senza salvare?": the one dialog the crawler answers, since it saves
      // nothing anyway. Any other dialog (a declaration) stops it.
      const dialog = [...doc().querySelectorAll(".ui-dialog")].find((x) => x.offsetParent || getComputedStyle(x).display !== "none");
      if (dialog && /attendere prego/i.test(dialog.innerText)) continue; // the portal's own loading box
      if (dialog) {
        if (!/non sono state salvate[\s\S]*proseguire senza salvare/i.test(dialog.innerText)) throw new Error(`a dialog: ${dialog.innerText.replace(/\s+/g, " ").slice(0, 120)}`);
        const ok = [...dialog.querySelectorAll("button")].find((b) => norm(b.textContent) === "conferma");
        if (ok && !dialog.dataset.pvAnswered) { dialog.dataset.pvAnswered = "1"; ok.click(); }
        continue;
      }
      if (title() && doc().location.href + "|" + title() !== before) break;
      if (messages().length) break;
    }
    await idle();
    frame.removeEventListener("load", onload);
  }

  const title = () => norm((doc().querySelector(".content-title") || {}).innerText);
  const button = (text) => [...doc().querySelectorAll("button, a")].find((b) => b.offsetParent && norm(b.textContent) === norm(text));
  const messages = () => [...doc().querySelectorAll(".ui-messages-error, .ui-message-error, .ui-growl-message, .ui-messages-warn")]
    .filter((x) => x.getClientRects().length).map((x) => x.innerText.trim().replace(/\s+/g, " ")).filter(Boolean);

  function tree() {
    return [...doc().querySelectorAll(".ui-tree li.ui-treenode")].map((li) => {
      const span = li.querySelector(":scope > .ui-treenode-content .tree-content > span");
      const parent = li.parentElement.closest("li.ui-treenode");
      return {
        key: li.dataset.rowkey, type: li.dataset.nodetype, label: span ? span.textContent.trim() : "",
        parent: parent ? parent.dataset.rowkey : null, selected: li.classList.contains("ui-treenode-selected"),
      };
    });
  }
  const node = (key) => doc().querySelector(`.ui-tree li.ui-treenode[data-rowkey="${key}"]`);
  async function open(key) {
    const keys = [];
    for (let li = node(key); (li = li.parentElement && li.parentElement.closest("li.ui-treenode"));) keys.unshift(li.dataset.rowkey);
    for (const k of keys) {
      const c = node(k).querySelector(":scope > .ui-treenode-content");
      if (c.getAttribute("aria-expanded") !== "true") { c.querySelector(".ui-treenode-label").click(); await idle(); }
    }
  }
  async function setSelected(key, want) {
    await open(key);
    if (node(key).classList.contains("ui-treenode-selected") !== want) {
      node(key).querySelector(":scope > .ui-treenode-content .tree-content > span").click();
      await idle();
    }
    return node(key).classList.contains("ui-treenode-selected") === want;
  }
  const leaves = (t) => t.filter((n) => (n.type === "RAD" || n.type === "CHK") && n.label);
  const radios = () => [...doc().querySelectorAll("input[type=radio]")].map((r) => {
    const box = r.closest("tr") || r.closest("li");
    return { label: (box ? box.innerText : "").replace(/\s+/g, " ").trim().slice(0, 160), checked: r.checked };
  });

  // The portal's error page ends a flow; the draft itself is kept. Reopen it from
  // "Pratiche in bozza" and come back to the sectors.
  async function reopen(force) {
    if (!force && title() && !title().includes("errore") && !title().includes("pratiche in bozza")) return false;
    // /fnsuap/cmd/home starts a fresh flow on the drafts list.
    const loaded = new Promise((r) => frame.addEventListener("load", r, { once: true }));
    win().location.href = "/fnsuap/cmd/home";
    await loaded;
    await idle();
    // The list shows ten drafts a page.
    let row;
    for (let page = 0; page < 30; page++) {
      row = [...doc().querySelectorAll("tr")].find((t) => t.innerText.includes(report.draft));
      const next = doc().querySelector(".ui-paginator-next:not(.ui-state-disabled)");
      if (row || !next) break;
      next.click();
      await sleep(300);
      await idle();
    }
    const apri = row && [...row.querySelectorAll("button")].find((b) => /apri/i.test(b.textContent));
    if (!apri) throw new Error(`no draft named ${report.draft}`);
    apri.click();
    // "Caricamento della pratica": wait for a screen with a title.
    const end = performance.now() + 90000;
    while (!(title() && !busy())) { if (performance.now() > end) throw new Error("the draft didn't open"); await sleep(200); }
    await idle();
    // A reopened draft can start on its first screens again: the description it has, the access.
    for (let n = 0; n < 2 && /assegna una descrizione|controlla l'accesso/.test(title()); n++) await go(() => button("Conferma").click());
    report.reopened++;
    return true;
  }
  async function toScreen(name) {
    await reopen();
    for (let n = 0; n < 10 && !title().includes(name); n++) {
      const b = button("Pagina precedente"); if (!b) break; await go(() => b.click());
    }
    if (!title().includes(name)) throw new Error(`can't get back to ${name} from ${title()}`);
  }

  // Si/No questions (SIN nodes) the portal requires before it goes on.
  const sins = () => tree().filter((n) => n.type === "SIN");
  async function answer(key, yes) {
    const r = doc().querySelector(`[id$="treeInterventi:${key}:sinCond:${yes ? 0 : 1}"]`) ||
      node(key).querySelector(`input[type=radio][value="${yes}"]`);
    if (!r) throw new Error(`no ${yes ? "Si" : "No"} for ${key}`);
    if (!r.checked) { r.closest(".ui-radiobutton").querySelector(".ui-radiobutton-box").click(); await idle(); }
  }
  async function screenOptions() {
    const tr = tree();
    if (tr.length) return tr.filter((x) => x.label).map((x) => ({ key: x.key, type: x.type, label: x.label, selected: x.selected || undefined }));
    if (doc().querySelector("input[type=radio]")) return radios();
    return undefined;
  }
  function generated() {
    const text = doc().body.innerText;
    const pick = (a, b) => { const i = text.indexOf(a); return i < 0 ? "" : text.slice(i + a.length, b ? text.indexOf(b, i) : i + 400).trim(); };
    return {
      interventi: pick("Interventi Selezionati:", "Adempimento"),
      adempimenti: [...text.matchAll(/Adempimento attivato:\s*\n?([^\n]+)\nAmministrazione competente:\s*\n?([^\n]+)/g)].map((m) => ({ adempimento: m[1].trim(), amministrazione: m[2].trim() })),
      sections: ["Scheda anagrafica", ...[...doc().querySelectorAll("a.linkmenu")].map((a) => a.textContent.trim()).filter((s) => s !== "Scheda anagrafica")],
    };
  }
  // Forward with Conferma to the generated pratica, answering every Si/No with No.
  async function forward(screens) {
    for (let n = 0; n < 8; n++) {
      if (title().includes("compilazione della domanda")) return generated();
      // The draft keeps earlier choices: the base pratica is the one with none.
      if (title().includes("scelte eventuali")) {
        for (const n of tree().filter((x) => x.type === "CHK" && x.selected)) await setSelected(n.key, false);
      }
      for (const q of sins()) await answer(q.key, false);
      const b = button("Conferma");
      if (!b) throw new Error(`no Conferma on ${title()}`);
      const was = title();
      await go(() => b.click());
      if (title() === was && messages().length) throw new Error(`${was}: ${messages().join(" / ")}`);
      if (screens && !title().includes("compilazione della domanda")) screens.push({ screen: title(), options: await screenOptions() });
    }
    throw new Error("no pratica after 8 screens");
  }
  async function back(name) {
    for (let n = 0; n < 10 && !title().includes(name); n++) {
      const b = button("Pagina precedente");
      if (!b) throw new Error(`no way back from ${title()}`);
      await go(() => b.click());
    }
    if (!title().includes(name)) throw new Error(`lost on ${title()}`);
  }

  // From the operations screen, with one operation selected: forward to the generated
  // pratica, noting each screen; then each option of Scelte eventuali alone (a Si
  // answered Si, a box ticked), noting the sections it adds or drops; then back.
  // onPratica(pratica, variant) runs on each generated pratica's screen (crawl.js's batch reads
  // its sections there); variant is null for the base pratica. `variants` is true, false, or a
  // function of a scelta's label saying whether to try it. With `interventi`, each top-level
  // tick of "Scelta interventi eventuali" is tried alone the same way (alcohol, an outside sign,
  // fire prevention added to the pratica), its variant's type "intervento".
  async function follow(op, variants, onPratica, interventi) {
    const screens = [];
    const b = button("Conferma");
    await go(() => b.click());
    if (title().includes("scelta operazioni")) { op.refused = messages(); return; }
    if (!title().includes("compilazione della domanda")) screens.push({ screen: title(), options: await screenOptions() });
    op.pratica = await forward(screens);
    op.screens = screens;
    if (onPratica) await onPratica(op.pratica, null);
    const scelte = screens.find((s) => s.screen.includes("scelte eventuali"));
    if (variants && scelte && Array.isArray(scelte.options)) {
      op.variants = [];
      const wanted = (x) => typeof variants !== "function" || variants(x.label);
      for (const o of scelte.options.filter((x) => (x.type === "CHK" || x.type === "SIN") && wanted(x))) {
        const v = { option: o.label, type: o.type };
        op.variants.push(v);
        try {
          await back("scelte eventuali");
          if (o.type === "SIN") await answer(o.key, true);
          else if (!(await setSelected(o.key, true))) throw new Error("didn't tick");
          // A Si can open a follow-up question; answer those No and go on.
          const p = await (async () => {
            const btn = button("Conferma");
            const was = title();
            await go(() => btn.click());
            if (title() === was) {
              // Still here: newly shown Si/No questions under the one answered Si.
              v.opened = sins().filter((q) => q.key !== o.key).map((q) => q.label);
              for (const q of sins()) if (q.key !== o.key) await answer(q.key, false);
              await go(() => button("Conferma").click());
            }
            return forward(null);
          })();
          const base = new Set(op.pratica.sections);
          v.adds = p.sections.filter((s) => !base.has(s));
          v.drops = op.pratica.sections.filter((s) => !p.sections.includes(s));
          if (JSON.stringify(p.adempimenti) !== JSON.stringify(op.pratica.adempimenti)) v.adempimenti = p.adempimenti;
          if (onPratica) await onPratica(p, v);
          await back("scelte eventuali");
          if (o.type === "SIN") await answer(o.key, false);
          else await setSelected(o.key, false);
        } catch (e) {
          v.error = String(e.message || e);
          await toScreen("scelte eventuali").catch(() => {});
        }
      }
    }
    const inter = interventi && screens.find((s) => s.screen.includes("interventi eventuali"));
    if (variants && inter && Array.isArray(inter.options)) {
      op.variants = op.variants || [];
      const wanted = (x) => typeof variants !== "function" || variants(x.label);
      // A child tick ("1_0": the ANAS's own SCIA for a sign) shows only under its parent.
      for (const o of inter.options.filter((x) => x.type === "CHK" && !String(x.key).includes("_") && wanted(x))) {
        const v = { option: o.label, type: "intervento" };
        op.variants.push(v);
        try {
          await back("interventi eventuali");
          if (!(await setSelected(o.key, true))) throw new Error("didn't tick");
          const was = title();
          await go(() => button("Conferma").click());
          if (title() === was) throw new Error(`${was}: ${messages().join(" / ") || "didn't go on"}`);
          const p = await forward(null);
          const base = new Set(op.pratica.sections);
          v.adds = p.sections.filter((s) => !base.has(s));
          v.drops = op.pratica.sections.filter((s) => !p.sections.includes(s));
          if (JSON.stringify(p.adempimenti) !== JSON.stringify(op.pratica.adempimenti)) v.adempimenti = p.adempimenti;
          if (onPratica) await onPratica(p, v);
          await back("interventi eventuali");
          await setSelected(o.key, false);
        } catch (e) {
          v.error = String(e.message || e);
          await toScreen("interventi eventuali").then(() => setSelected(o.key, false)).catch(() => {});
        }
      }
    }
    await back("scelta operazioni");
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
    await reopen(true);
    // A reopened scratch draft lands where it was left: back to the sectors.
    await toScreen("scelta settore");
    if (!title().includes("scelta settore")) throw new Error(`expected Scelta settore, found ${title()}`);
    report.sector_tree = tree().map(({ key, type, label, parent }) => ({ key, type, label, parent }));
    const sectors = leaves(report.sector_tree).filter((s) => !opts.sectors || opts.sectors.some((x) => norm(s.label).includes(norm(x))));
    for (const sec of sectors) {
      const S = { sector: sec.label, key: sec.key, operations: [] };
      report.sectors.push(S);
      try {
        for (const other of tree().filter((n) => n.selected && n.key !== sec.key && (n.type === "RAD" || n.type === "CHK"))) await setSelected(other.key, false);
        if (!(await setSelected(sec.key, true))) throw new Error("sector didn't select");
        await go(() => button("Conferma").click());
        if (!title().includes("scelta operazioni")) throw new Error(`expected Scelta operazioni, found ${title()}`);
        S.tree = tree().map(({ key, type, label, parent }) => ({ key, type, label, parent }));
        // follow: false reads each sector's operations tree only, without generating pratiche.
        let ops = opts.follow === false ? [] : leaves(S.tree);
        if (opts.only) ops = ops.filter((o) => opts.only.some((x) => norm(o.label).includes(norm(x))));
        for (const o of ops.slice(0, opts.limit || 1000)) {
          const op = { key: o.key, type: o.type, label: o.label };
          S.operations.push(op);
          const o0 = performance.now();
          try {
            for (const other of tree().filter((n) => n.selected && n.key !== o.key && (n.type === "RAD" || n.type === "CHK"))) await setSelected(other.key, false);
            if (!(await setSelected(o.key, true))) throw new Error("didn't select");
            await follow(op, opts.variants !== false);
            await setSelected(o.key, false);
          } catch (e) {
            op.error = String(e.message || e);
            // Find the operations screen again before the next one.
            await toScreen("scelta operazioni");
          }
          op.seconds = +((performance.now() - o0) / 1000).toFixed(1);
          report.done_ops++;
        }
        // Back to the sectors for the next one.
        await toScreen("scelta settore");
      } catch (e) {
        S.error = String(e.message || e);
      }
    }
    report.seconds = +((performance.now() - t0) / 1000).toFixed(1);
    report.done = true;
  }

  window.__wizard = {
    // opts.draft: the scratch draft's description, to reopen it after an error page.
    start(opts = {}) {
      report = { started: new Date().toISOString(), draft: opts.draft, sectors: [], done_ops: 0, reopened: 0, done: false };
      crawl(opts).catch((e) => { report.error = String(e.message || e); report.done = true; });
      return "started";
    },
    async wait(ms = 25000) {
      const end = performance.now() + ms;
      while (report && !report.done && performance.now() < end) await sleep(250);
      return report;
    },
    report: () => report,
    // crawl.js's batch: these moves on a frame it shares, for the draft named `draft`.
    driver(f, draft) {
      frame = f;
      if (!report || report.draft !== draft) report = { draft, sectors: [], done_ops: 0, reopened: 0 };
      return { reopen, toScreen, title, tree, setSelected, follow, go, button };
    },
  };
})();
