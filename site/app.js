/* OpenAgentFleet landing: the mock desktop, the hero's autonomous run with
   take-over, the scroll-scrubbed story, and the small stuff. No dependencies. */
(function () {
  'use strict';
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const finePointer = window.matchMedia('(pointer: fine)').matches;

  /* ------------------------------------------------------------ theme */
  const root = document.documentElement;
  const themeBtn = document.getElementById('themeBtn');
  const order = ['system', 'light', 'dark'];
  function currentTheme() { return root.getAttribute('data-theme') || 'system'; }
  function applyTheme(t) {
    if (t === 'system') root.removeAttribute('data-theme'); else root.setAttribute('data-theme', t);
    try { if (t === 'system') localStorage.removeItem('oaf-theme'); else localStorage.setItem('oaf-theme', t); } catch (e) {}
    themeBtn.title = 'Theme: ' + t;
  }
  themeBtn.title = 'Theme: ' + currentTheme();
  themeBtn.addEventListener('click', () => {
    const next = order[(order.indexOf(currentTheme()) + 1) % order.length];
    applyTheme(next);
  });

  /* ------------------------------------------------------------ the mock desktop */
  const CURSOR_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3l14 8.5-6.2 1.4L9.6 19z" fill="#fff" stroke="#111" stroke-width="1.4" stroke-linejoin="round"/></svg>';
  const CURSOR_HUMAN_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3l14 8.5-6.2 1.4L9.6 19z" fill="#38bdf8" stroke="#0b2a3a" stroke-width="1.4" stroke-linejoin="round"/></svg>';

  function hnPage() {
    return '<div class="hn"><div class="hn-bar"><span>Y</span><span>Hacker News</span><span>new</span><span>past</span></div>' +
      '<ol><li>Show HN: an agent that drives a real desktop<span>412 points · 187 comments</span></li>' +
      '<li>Why sandboxes beat tool lists<span>298 points · 94 comments</span></li>' +
      '<li>The accessibility tree is the best API you already have<span>250 points · 63 comments</span></li>' +
      '<li>Running a 300B MoE on a LAN box<span>187 points · 71 comments</span></li></ol></div>';
  }
  function notesPage(state) {
    const notes = state.notes || [];
    return '<div class="page-title">Fleet Notes</div><div class="page-sub">' + notes.length + (notes.length === 1 ? ' note' : ' notes') + ' · saved in this browser · N for a new note</div>' +
      '<div class="note-row"><div class="note-input' + (state.focus ? ' focus' : '') + '" data-role="note-input">' + (state.typing || '<span style="opacity:.7">Write a note…</span>') + (state.focus ? '<span class="caret"></span>' : '') + '</div>' +
      '<span class="btn-mini' + (state.pressed ? ' pressed' : '') + '" data-role="save">Save</span></div>' +
      '<ul class="notes">' + notes.map((n, i) => '<li' + (i === 0 && state.fresh ? ' class="new"' : '') + '><span>' + n.text + '</span><span class="meta">' + n.when + '</span></li>').join('') + '</ul>';
  }

  /* Build the desktop DOM. `opts.host` names the machine; `opts.mini` is a
     fleet tile with one small window. */
  function buildDesk(el, opts) {
    opts = opts || {};
    const host = opts.host || 'builder';
    el.innerHTML =
      '<div class="panel"><span class="apps"><i></i>Applications</span><span class="host">agent@' + host + '</span><span class="clock">' + (opts.clock || '10:42') + '</span></div>' +
      (opts.mini
        ? '<div class="win noaddr" style="left:8%;top:14%;width:84%;height:74%"><div class="win-bar"><span class="t"></span><span class="t"></span><span class="t"></span><span class="title">' + (opts.title || 'Mozilla Firefox') + '</span></div><div class="win-body">' + (opts.body || '') + '</div></div>'
        : '<div class="win browser behind" data-win="browser" style="left:26%;top:10%;width:70%;height:82%">' +
            '<div class="win-bar"><span class="t"></span><span class="t"></span><span class="t"></span><span class="title" data-role="title">Hacker News — Mozilla Firefox</span></div>' +
            '<div class="win-url"><div class="field" data-role="url"><span data-role="urltext">news.ycombinator.com</span></div></div>' +
            '<div class="win-body" data-role="body">' + hnPage() + '</div></div>' +
          '<div class="win term" data-win="term" style="left:4%;top:16%;width:46%;height:52%">' +
            '<div class="win-bar"><span class="t"></span><span class="t"></span><span class="t"></span><span class="title">Terminal — agent@' + host + ': ~/fleet-notes</span></div>' +
            '<div class="win-body" data-role="term"><span class="pr">agent@' + host + ':~/fleet-notes$</span> <span class="caret"></span></div></div>') +
      '<div class="cursor agent" data-role="cursor">' + CURSOR_SVG + '<span class="ripple"></span></div>' +
      '<div class="cursor human" data-role="human">' + CURSOR_HUMAN_SVG + '<span class="ripple"></span></div>';
    const q = (r) => el.querySelector('[data-role="' + r + '"]');
    const api = {
      el,
      cursor: q('cursor'),
      human: q('human'),
      moveCursor(x, y) { api.cursor.style.transform = 'translate(' + x + 'cqw, ' + y + 'cqw)'; },
      moveHuman(x, y) { api.human.style.transform = 'translate(' + x + 'cqw, ' + y + 'cqw)'; },
      click(which) { const c = which === 'human' ? api.human : api.cursor; c.classList.remove('click'); void c.offsetWidth; c.classList.add('click'); },
      raise(name) {
        el.querySelectorAll('.win').forEach(w => w.classList.toggle('behind', w.dataset.win !== name));
      },
      setBody(html) { const b = q('body'); if (b) b.innerHTML = html; },
      setTitle(t) { const x = q('title'); if (x) x.textContent = t; },
      setURL(text, focus) { const f = q('url'); if (!f) return; f.classList.toggle('focus', !!focus); f.innerHTML = '<span data-role="urltext">' + text + '</span>' + (focus ? '<span class="caret"></span>' : ''); },
      setTerm(html) { const t = q('term'); if (t) t.innerHTML = html; },
    };
    return api;
  }

  /* ------------------------------------------------------------ hero: an agent at work */
  const heroDeskEl = document.getElementById('heroDesk');
  const heroStage = document.getElementById('heroStage');
  const tickerLines = document.getElementById('tickerLines');
  const hero = buildDesk(heroDeskEl, { host: 'builder' });

  const PROMPT = '<span class="pr">agent@builder:~/fleet-notes$</span> ';
  function tick(step, action, cls) {
    const li = document.createElement('li');
    if (cls) li.className = cls;
    li.innerHTML = '<span class="n">' + (step === '' ? '' : String(step).padStart(2, '0')) + '</span><span>' + action + '</span>';
    tickerLines.appendChild(li);
    while (tickerLines.children.length > 4) tickerLines.removeChild(tickerLines.firstChild);
  }
  function act(json) {
    return json.replace(/"(action|thought|text|target|summary|key)"/g, '<span class="k">"$1"</span>');
  }

  // Timeline steps: [delay before, fn]. Positions are percentages of the desk width.
  let heroTimer = null, heroRun = 0, driving = false;
  const NOTES = [{ text: 'Ship the landing page', when: 'just now' }, { text: 'Recreate Checker on the new image', when: '2h ago' }, { text: 'Tell the gateway maintainer thanks', when: 'yesterday' }];
  const heroSteps = [
    [400, () => { hero.raise('term'); hero.moveCursor(24, 30); hero.setTerm(PROMPT + '<span class="caret"></span>'); }],
    [700, () => { hero.click(); tick(1, act('{"thought": "The server is not up yet.", "action": "shell", "text": "python3 -m http.server 8000"}')); }],
    [500, () => typeInto((s) => hero.setTerm(PROMPT + s + '<span class="caret"></span>'), 'python3 -m http.server 8000', 38)],
    [1300, () => { hero.setTerm(PROMPT + 'python3 -m http.server 8000\n<span class="dim">Serving HTTP on 0.0.0.0 port 8000 (http://0.0.0.0:8000/) ...</span>'); }],
    [700, () => { hero.moveCursor(62, 15); tick(2, act('{"action": "focus", "target": "Mozilla Firefox"}')); }],
    [650, () => { hero.click(); hero.raise('browser'); }],
    [600, () => { hero.moveCursor(58, 22); tick(3, act('{"action": "click", "target": "Search or enter address"}')); }],
    [600, () => { hero.click(); hero.setURL('', true); }],
    [400, () => { tick(4, act('{"action": "type", "text": "af-2f74f6ae:8000"}')); typeInto((s) => hero.setURL(s, true), 'af-2f74f6ae:8000', 42); }],
    [1300, () => { tick(5, act('{"action": "key", "key": "Return"}')); hero.setURL('af-2f74f6ae:8000', false); hero.setTitle('Fleet Notes — Mozilla Firefox'); hero.setBody(notesPage({ notes: NOTES.slice(1) })); }],
    [700, () => { tick(6, act('{"action": "wait_for", "text": "Fleet Notes"}')); }],
    [900, () => { hero.moveCursor(44, 46); tick(7, act('{"action": "click", "target": "Write a note…"}')); }],
    [650, () => { hero.click(); hero.setBody(notesPage({ notes: NOTES.slice(1), focus: true, typing: '' })); }],
    [400, () => { tick(8, act('{"action": "type", "text": "Ship the landing page"}')); typeInto((s) => hero.setBody(notesPage({ notes: NOTES.slice(1), focus: true, typing: s })), 'Ship the landing page', 40); }],
    [1400, () => { hero.moveCursor(86, 46); tick(9, act('{"action": "click", "target": "Save"}')); }],
    [600, () => { hero.click(); hero.setBody(notesPage({ notes: NOTES.slice(1), pressed: true, typing: 'Ship the landing page' })); }],
    [180, () => { hero.setBody(notesPage({ notes: NOTES, fresh: true })); }],
    [900, () => { hero.moveCursor(70, 70); tick(10, act('{"action": "done", "summary": "Note saved. App renders at http://af-2f74f6ae:8000"}')); }],
    [3600, () => { resetHero(); }],
  ];
  let typers = [];
  function typeInto(setter, text, ms) {
    let i = 0;
    const t = setInterval(() => {
      i++; setter(text.slice(0, i));
      if (i >= text.length) clearInterval(t);
    }, ms);
    typers.push(t);
  }
  function clearTypers() { typers.forEach(clearInterval); typers = []; }
  function resetHero() {
    clearTypers();
    hero.setTerm(PROMPT + '<span class="caret"></span>');
    hero.setTitle('Hacker News — Mozilla Firefox');
    hero.setURL('news.ycombinator.com', false);
    hero.setBody(hnPage());
    hero.raise('browser');
    hero.moveCursor(60, 40);
    tickerLines.innerHTML = '';
    tick('', '<span class="n">—</span> new window, goal: add a note in Fleet Notes and confirm it renders');
    scheduleHero(0);
  }
  function scheduleHero(i) {
    if (reduced || driving) return;
    if (i >= heroSteps.length) i = 0;
    const [delay, fn] = heroSteps[i];
    heroTimer = setTimeout(() => { if (driving) return; fn(); heroRun = i + 1; scheduleHero(i + 1); }, delay);
  }
  if (reduced) {
    // A still that shows the finished state.
    hero.setTitle('Fleet Notes — Mozilla Firefox'); hero.setURL('af-2f74f6ae:8000', false); hero.setBody(notesPage({ notes: NOTES }));
    hero.setTerm(PROMPT + 'python3 -m http.server 8000\n<span class="dim">Serving HTTP on 0.0.0.0 port 8000 ...</span>'); hero.raise('browser'); hero.moveCursor(86, 46);
    tick(9, act('{"action": "click", "target": "Save"}')); tick(10, act('{"action": "done", "summary": "Note saved."}'));
  } else {
    resetHero();
  }

  // Take over: on a fine pointer, hovering the desktop hands you the cursor.
  const driveChip = document.getElementById('driveChip');
  const heroHint = document.getElementById('heroHint');
  let resumeTimer = null;
  function startDriving() {
    if (driving) return;
    driving = true;
    clearTimeout(heroTimer); clearTypers(); clearTimeout(resumeTimer);
    heroStage.classList.add('driving');
    tick('', '<span class="n">op</span> operator took the mouse — agent paused', 'op');
  }
  function stopDriving() {
    if (!driving) return;
    driving = false;
    heroStage.classList.remove('driving');
    tick('', '<span class="n">op</span> operator let go — agent resumes', 'op');
    resumeTimer = setTimeout(() => { if (!driving) scheduleHero(heroRun); }, 900);
  }
  if (finePointer && !reduced) {
    heroDeskEl.addEventListener('pointerenter', startDriving);
    heroDeskEl.addEventListener('pointerleave', stopDriving);
    heroDeskEl.addEventListener('pointermove', (e) => {
      if (!driving) return;
      const r = heroDeskEl.getBoundingClientRect();
      const x = (e.clientX - r.left) / r.width * 100, y = (e.clientY - r.top) / r.width * 100;
      hero.moveHuman(x, y);
    });
    heroDeskEl.addEventListener('pointerdown', (e) => {
      if (!driving) return;
      hero.click('human');
      const t = e.target.closest('[data-win]');
      if (t) hero.raise(t.dataset.win);
      tick('', '<span class="n">op</span> click at ' + Math.round(e.offsetX) + ',' + Math.round(e.offsetY) + (t ? ' on ' + t.dataset.win : ''), 'op');
    });
  } else if (heroHint) {
    heroHint.textContent = 'A real agent run, replayed. On a desktop, hovering the screen hands you the mouse.';
  }

  /* ------------------------------------------------------------ the scroll story */
  const stage = document.getElementById('stage');
  const storyDeskEl = document.getElementById('storyDesk');
  const story = buildDesk(storyDeskEl, { host: 'builder', clock: '10:44' });
  story.setTitle('Fleet Notes — Mozilla Firefox'); story.setURL('af-2f74f6ae:8000', false);
  story.raise('browser');
  story.setBody(notesPage({ notes: NOTES.slice(1) }));
  story.setTerm(PROMPT + 'python3 -m http.server 8000\n<span class="dim">Serving HTTP on 0.0.0.0 port 8000 ...</span>');
  story.moveCursor(20, 78);
  storyDeskEl.classList.add('instant');

  // Set-of-Marks badges, in desk-width percentages (x, y) with the label they sit on.
  const MARKS = [
    [58, 22, 'address bar'], [44, 46, 'Write a note…'], [86, 46, 'Save'],
    [40, 56, 'note: Recreate Checker…'], [40, 65, 'note: Tell the gateway…'], [60, 15, 'Fleet Notes — Mozilla Firefox'], [24, 30, 'Terminal'],
  ];
  const marksEl = document.getElementById('marks');
  MARKS.forEach((m, i) => {
    const b = document.createElement('span');
    b.className = 'mark' + (i === 1 ? ' hot' : '');
    b.textContent = i + 1;
    b.style.left = m[0] + '%'; b.style.top = (m[1] * 10 / 16 * 1.0) + '%';
    marksEl.appendChild(b);
  });
  // The desk is 16:10, and positions are in cqw (width units); convert y to a
  // percentage of the desk height: y_cqw / (100 * 10/16).
  Array.from(marksEl.children).forEach((b, i) => { b.style.top = (MARKS[i][1] / 62.5 * 100) + '%'; });

  const TREE = [
    'frame  "Fleet Notes — Mozilla Firefox"',
    '  document  "Fleet Notes"',
    '    <span class="r">entry</span>       "Search or enter address"   [1]',
    '    heading     "Fleet Notes"',
    '    <span class="r">entry</span>       "Write a note…"             [2]',
    '    <span class="r">push button</span> "Save"                      [3]',
    '    list  2 items',
    '      list item "Recreate Checker…"        [4]',
    '      list item "Tell the gateway…"        [5]',
    'frame  "Terminal — agent@builder"         [7]',
  ];
  const treeEl = document.getElementById('tree'), treeLines = document.getElementById('treeLines');
  const actionCard = document.getElementById('actionCard'), actionText = document.getElementById('actionText');
  const ACTION = '{\n  "thought": "The note field [2] is empty and the goal is a saved note. Click it, then type.",\n  "action": "click",\n  "mark": 2\n}';
  function renderAction(s) {
    actionText.innerHTML = s
      .replace(/"(thought|action|mark)"/g, '<span class="k">"$1"</span>')
      .replace(/: "([^"]*)"/g, ': <span class="s">"$1"</span>');
  }

  // Fleet tiles.
  const fleetEl = document.getElementById('fleet'), fleetGrid = document.getElementById('fleetGrid');
  const BOTS = [
    ['Builder', 'developer-heavy', 'Fleet Notes — Mozilla Firefox', '<div class="page-title" style="font-size:2.6cqw">Fleet Notes</div><div class="page-sub">3 notes · served on :8000</div>'],
    ['Checker', 'micro', 'Fleet Notes — Mozilla Firefox', '<div class="page-sub">http://af-2f74f6ae:8000</div><div class="page-title" style="font-size:2.6cqw">Fleet Notes</div><div class="page-sub">testing: empty state, 200 notes, contrast…</div>'],
    ['Scout', 'standard', 'Odoo — Mozilla Firefox', '<div class="page-title" style="font-size:2.4cqw">Sales › Reporting</div><div class="page-sub">exporting August…</div>'],
    ['Writer', 'standard', 'README.md — Text Editor', '<div class="page-sub" style="font-family:var(--font-mono)"># Fleet Notes<br>A single-page notes app…</div>'],
    ['Auditor', 'micro', 'findings.md', '<div class="page-sub" style="font-family:var(--font-mono)">1. Save has no focus ring<br>2. 300-char title overflows<br>3. Empty search says nothing</div>'],
    ['Designer', 'standard', 'spec.md', '<div class="page-sub" style="font-family:var(--font-mono)">theme: follow system<br>shortcut: N<br>store: localStorage</div>'],
  ];
  const tiles = BOTS.map((b) => {
    const cell = document.createElement('div'); cell.className = 'cell';
    const d = document.createElement('div'); d.className = 'desk instant';
    cell.appendChild(d);
    const name = document.createElement('span'); name.className = 'name'; name.innerHTML = '<b>' + b[0] + '</b> · ' + b[1];
    cell.appendChild(name);
    fleetGrid.appendChild(cell);
    buildDesk(d, { host: b[0].toLowerCase(), mini: true, title: b[2], body: b[3], clock: '10:5' + (BOTS.indexOf(b) + 1) });
    return { cell, desk: d };
  });
  const arcs = document.getElementById('arcs'), handoffsEl = document.getElementById('handoffs');
  const HANDOFFS = [
    [0, 1, '<b>Builder → Checker</b><br>v1 is live at http://af-2f74f6ae:8000 — please test'],
    [1, 0, '<b>Checker → Builder</b><br>3 findings, ordered by severity, in findings.md'],
    [5, 0, '<b>Designer → Builder</b><br>spec published to the catalog'],
  ];
  let arcPaths = [];
  function layoutArcs() {
    arcs.innerHTML = ''; handoffsEl.innerHTML = ''; arcPaths = [];
    const sr = stage.getBoundingClientRect();
    arcs.setAttribute('viewBox', '0 0 ' + sr.width + ' ' + sr.height);
    HANDOFFS.forEach((h, i) => {
      const a = tiles[h[0]].cell.getBoundingClientRect(), b = tiles[h[1]].cell.getBoundingClientRect();
      const x1 = a.left - sr.left + a.width / 2, y1 = a.top - sr.top + a.height / 2;
      const x2 = b.left - sr.left + b.width / 2, y2 = b.top - sr.top + b.height / 2;
      const bend = (i % 2 ? -1 : 1) * Math.max(40, Math.abs(x2 - x1) * 0.35 + 30);
      const cx = (x1 + x2) / 2, cy = (y1 + y2) / 2 - bend * (i === 2 ? 1.2 : 0.6);
      const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      p.setAttribute('d', 'M' + x1 + ' ' + y1 + ' Q' + cx + ' ' + cy + ' ' + x2 + ' ' + y2);
      arcs.appendChild(p);
      const len = p.getTotalLength();
      p.style.strokeDasharray = len; p.style.strokeDashoffset = len;
      const dot = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      dot.setAttribute('r', '4'); arcs.appendChild(dot);
      const label = document.createElement('div'); label.className = 'handoff'; label.innerHTML = h[2];
      const mid = p.getPointAtLength(len * 0.5);
      label.style.left = mid.x + 'px'; label.style.top = (mid.y + (i === 1 ? 26 : -26)) + 'px';
      handoffsEl.appendChild(label);
      arcPaths.push({ p, dot, len, label });
    });
  }
  const operator = document.getElementById('operator');

  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const ease = (t) => t < .5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
  const sub = (p, a, b) => clamp((p - a) / (b - a), 0, 1);

  const steps = Array.from(document.querySelectorAll('#storySteps .step'));
  let lastScene = -1, typedLen = -1, bodyState = '';
  function renderScene(p) {
    // 0 look · 1 decide · 2 act · 3 fleet · 4 takeover
    const scene = Math.min(4, Math.floor(p * 5));
    if (scene !== lastScene) { steps.forEach((s, i) => s.classList.toggle('active', i === scene)); lastScene = scene; }

    // marks pop in over the first fifth, staggered
    const look = sub(p, 0.0, 0.2);
    Array.from(marksEl.children).forEach((b, i) => {
      const t = ease(sub(look, i * 0.08, i * 0.08 + 0.35));
      const visible = p < 0.62;
      b.style.opacity = visible ? t : 0;
      b.style.transform = 'translate(-50%,-50%) scale(' + (0.4 + 0.6 * t) + ')';
    });
    // accessibility tree
    const treeT = ease(sub(look, 0.25, 1)) * (p < 0.62 ? 1 : 0);
    treeEl.style.opacity = treeT; treeEl.style.transform = 'translateX(' + (16 - 16 * treeT) + 'px)';
    const lines = Math.round(TREE.length * sub(look, 0.3, 1));
    if (treeLines.dataset.n !== String(lines)) { treeLines.innerHTML = TREE.slice(0, lines).join('\n'); treeLines.dataset.n = String(lines); }

    // the action card types itself over the second fifth
    const decide = sub(p, 0.2, 0.4);
    const cardIn = ease(sub(decide, 0, 0.3)) * (p < 0.62 ? 1 : 0);
    actionCard.style.opacity = cardIn; actionCard.style.transform = 'translateY(' + (18 - 18 * cardIn) + 'px)';
    const n = Math.round(ACTION.length * sub(decide, 0.15, 1));
    if (n !== typedLen) { renderAction(ACTION.slice(0, n) + (n < ACTION.length && n > 0 ? '▍' : '')); typedLen = n; }

    // act: the cursor flies to mark 2, clicks, the field focuses, text lands, Save
    const actT = sub(p, 0.4, 0.6);
    if (p >= 0.38 && p < 0.62) {
      const fly = ease(sub(actT, 0, 0.45));
      story.moveCursor(20 + (44 - 20) * fly, 78 + (46 - 78) * fly);
      const typed = 'Ship the landing page'.slice(0, Math.round(21 * sub(actT, 0.5, 0.85)));
      const state = actT < 0.45 ? 'idle' : actT < 0.9 ? 'typing:' + typed : 'saved';
      if (state !== bodyState) {
        if (state === 'idle') story.setBody(notesPage({ notes: NOTES.slice(1) }));
        else if (state === 'saved') { story.setBody(notesPage({ notes: NOTES, fresh: true })); story.moveCursor(86, 46); }
        else story.setBody(notesPage({ notes: NOTES.slice(1), focus: true, typing: typed }));
        if (bodyState === 'idle' && state.startsWith('typing')) story.click();
        bodyState = state;
      }
    } else if (p < 0.38 && bodyState !== 'idle') { story.setBody(notesPage({ notes: NOTES.slice(1) })); story.moveCursor(20, 78); bodyState = 'idle'; }

    // fleet: the single desk dissolves into six
    const fleetT = sub(p, 0.6, 0.8);
    const fleetIn = ease(sub(fleetT, 0, 0.35));
    storyDeskEl.style.opacity = 1 - fleetIn;
    storyDeskEl.style.transform = 'scale(' + (1 - 0.06 * fleetIn) + ')';
    fleetEl.style.opacity = fleetIn;
    tiles.forEach((t, i) => {
      const k = ease(sub(fleetT, 0.05 + i * 0.05, 0.35 + i * 0.05));
      t.desk.style.opacity = k; t.desk.style.transform = 'translateY(' + (12 - 12 * k) + 'px) scale(' + (0.94 + 0.06 * k) + ')';
    });
    arcPaths.forEach((a, i) => {
      const k = ease(sub(fleetT, 0.4 + i * 0.15, 0.75 + i * 0.15));
      a.p.style.strokeDashoffset = a.len * (1 - k);
      if (k > 0 && k < 1) { const pt = a.p.getPointAtLength(a.len * k); a.dot.setAttribute('cx', pt.x); a.dot.setAttribute('cy', pt.y); a.dot.style.opacity = 1; }
      else a.dot.style.opacity = 0;
      a.label.style.opacity = k >= 1 ? 1 : 0;
    });

    // takeover: the operator takes Checker's mouse
    const takeT = sub(p, 0.8, 1);
    const on = takeT > 0.1;
    stage.classList.toggle('takeover', on);
    operator.style.opacity = ease(sub(takeT, 0.1, 0.4));
    operator.style.transform = 'translateY(' + (on ? 0 : -6) + 'px)';
    const checker = tiles[1];
    const hx = 70 - 42 * ease(sub(takeT, 0.15, 0.7)), hy = 90 - 46 * ease(sub(takeT, 0.15, 0.7));
    const human = checker.desk.querySelector('[data-role="human"]');
    if (human) { human.style.opacity = on ? 1 : 0; human.style.transform = 'translate(' + hx + 'cqw, ' + hy + 'cqw)'; }
    checker.cell.style.zIndex = on ? 8 : '';
    checker.desk.style.boxShadow = on ? '0 0 0 3px var(--cool), 0 14px 40px -18px rgba(0,0,0,.6)' : '';
  }

  const storySection = document.getElementById('loop');
  let raf = 0;
  function onScroll() {
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      const r = storySection.getBoundingClientRect();
      const vh = window.innerHeight;
      const total = r.height - vh;
      const p = total > 0 ? clamp(-r.top / total, 0, 1) : 0;
      stage.style.setProperty('--p', p.toFixed(4));
      renderScene(p);
    });
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  let resizeT = 0;
  window.addEventListener('resize', () => { clearTimeout(resizeT); resizeT = setTimeout(() => { layoutArcs(); onScroll(); }, 120); });
  // Arcs need the tiles laid out; the fleet layer is invisible but present.
  requestAnimationFrame(() => { layoutArcs(); onScroll(); });
  document.fonts && document.fonts.ready.then(() => { layoutArcs(); onScroll(); });

  /* ------------------------------------------------------------ reveals */
  const reveals = document.querySelectorAll('.reveal');
  if ('IntersectionObserver' in window && !reduced) {
    const io = new IntersectionObserver((es) => es.forEach(e => { if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target); } }), { rootMargin: '0px 0px -10% 0px', threshold: 0.15 });
    reveals.forEach(r => io.observe(r));
  } else reveals.forEach(r => r.classList.add('in'));

  /* ------------------------------------------------------------ trailer */
  const tv = document.getElementById('trailerVideo'), tp = document.getElementById('trailerPlay');
  if (tv && tp) {
    const start = () => { tp.hidden = true; tv.controls = true; tv.muted = false; tv.play().catch(() => { tp.hidden = false; }); };
    tp.addEventListener('click', start);
    tv.addEventListener('play', () => { tp.hidden = true; tv.controls = true; });
    document.querySelectorAll('a[href="#trailer"]').forEach(a => a.addEventListener('click', () => setTimeout(() => tp.focus({ preventScroll: true }), 600)));
  }

  /* ------------------------------------------------------------ copy buttons */
  document.querySelectorAll('.copy').forEach(btn => {
    btn.addEventListener('click', async () => {
      const text = btn.dataset.copy.replace(/&amp;/g, '&');
      try { await navigator.clipboard.writeText(text); btn.textContent = 'Copied'; btn.classList.add('done'); }
      catch (e) { btn.textContent = 'Select and copy'; }
      setTimeout(() => { btn.textContent = 'Copy'; btn.classList.remove('done'); }, 1800);
    });
  });

  /* ------------------------------------------------------------ the console frame leans with the scroll and the pointer */
  const frame = document.getElementById('consoleFrame');
  if (frame && !reduced && finePointer) {
    let px = 0, py = 0;
    const lean = () => {
      const r = frame.getBoundingClientRect(); const vh = window.innerHeight;
      const c = clamp((r.top + r.height / 2 - vh / 2) / vh, -1, 1);
      frame.style.transform = 'perspective(1400px) rotateX(' + (c * 6 - py * 4) + 'deg) rotateY(' + (px * 5) + 'deg) translateY(' + (c * -10) + 'px)';
    };
    window.addEventListener('scroll', lean, { passive: true }); lean();
    frame.addEventListener('pointermove', (e) => {
      const r = frame.getBoundingClientRect();
      px = (e.clientX - r.left) / r.width * 2 - 1; py = (e.clientY - r.top) / r.height * 2 - 1;
      frame.style.setProperty('--gx', ((px + 1) * 50).toFixed(1) + '%'); frame.style.setProperty('--gy', ((py + 1) * 50).toFixed(1) + '%');
      frame.style.setProperty('--glare', '1'); lean();
    });
    frame.addEventListener('pointerleave', () => { px = 0; py = 0; frame.style.setProperty('--glare', '0'); lean(); });
  }

  /* ------------------------------------------------------------ the console: tabs, and a slow tour while it is on screen */
  const tabsEl = document.getElementById('consoleTabs');
  if (tabsEl) {
    const tabs = Array.from(tabsEl.querySelectorAll('[role="tab"]'));
    const shots = Array.from(document.querySelectorAll('#consoleFrame .shot'));
    const urlEl = document.getElementById('frameUrl');
    let idx = 0, touched = false, tour = 0;
    const show = (i, focus) => {
      idx = (i + tabs.length) % tabs.length;
      tabs.forEach((t, j) => { t.setAttribute('aria-selected', String(j === idx)); t.tabIndex = j === idx ? 0 : -1; });
      shots.forEach((s, j) => s.classList.toggle('on', j === idx));
      if (urlEl) urlEl.textContent = tabs[idx].dataset.url;
      if (focus) tabs[idx].focus();
    };
    tabs.forEach((t, i) => t.addEventListener('click', () => { touched = true; clearInterval(tour); show(i); }));
    tabsEl.addEventListener('keydown', (e) => {
      if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
      e.preventDefault(); touched = true; clearInterval(tour);
      show(idx + (e.key === 'ArrowRight' ? 1 : -1), true);
    });
    if (!reduced && 'IntersectionObserver' in window) {
      new IntersectionObserver((es) => es.forEach(e => {
        clearInterval(tour);
        if (e.isIntersecting && !touched) tour = setInterval(() => show(idx + 1), 4200);
      }), { threshold: 0.4 }).observe(document.getElementById('consoleFrame'));
    }
  }

  /* ------------------------------------------------------------ depth: the hero leans toward the pointer */
  const heroSection = document.querySelector('.hero');
  if (heroSection && heroStage && !reduced && finePointer) {
    heroSection.addEventListener('pointermove', (e) => {
      if (driving) return;
      const r = heroStage.getBoundingClientRect();
      const x = clamp((e.clientX - (r.left + r.width / 2)) / (r.width / 2), -1.4, 1.4);
      const y = clamp((e.clientY - (r.top + r.height / 2)) / (r.height / 2), -1.4, 1.4);
      heroStage.style.setProperty('--ry', (-4 + x * 7).toFixed(2) + 'deg');
      heroStage.style.setProperty('--rx', (2 - y * 5).toFixed(2) + 'deg');
    });
    heroSection.addEventListener('pointerleave', () => { heroStage.style.removeProperty('--ry'); heroStage.style.removeProperty('--rx'); });
  }

  /* ------------------------------------------------------------ the org chart turns a little with the pointer */
  const orgScene = document.getElementById('orgScene');
  if (orgScene && !reduced && finePointer) {
    const tree = orgScene.querySelector('.orgtree');
    orgScene.addEventListener('pointermove', (e) => {
      const r = orgScene.getBoundingClientRect();
      const x = clamp((e.clientX - (r.left + r.width / 2)) / (r.width / 2), -1, 1);
      tree.style.setProperty('--ory', (x * 12).toFixed(2) + 'deg');
    });
    orgScene.addEventListener('pointerleave', () => tree.style.removeProperty('--ory'));
  }

  /* ------------------------------------------------------------ your agents: a ring that turns, and can be turned */
  const ringScene = document.getElementById('agentRing');
  const carousel = document.getElementById('agentCarousel');
  if (ringScene && carousel) {
    const cards = Array.from(carousel.children);
    const step = 360 / cards.length;
    if (reduced) {
      ringScene.classList.add('flat');
    } else {
      let angle = 0, vel = 0, dragging = false, lastX = 0, visible = false, snapTo = null, idleT = 0;
      let spacing = 150, depth = 260, halfW = 600, cardW = 176;
      // A loop of cards: the one in front faces you, the rest step back and
      // turn away on either side, and the one going round the far end fades
      // out and comes back in on the other side.
      const shade = () => {
        cards.forEach((c, i) => {
          const th = ((i * step + angle) % 360 + 540) % 360 - 180; // -180..180, 0 in front
          const t = th / step;                                     // cards from the front
          const x = t * spacing, a = Math.abs(t);
          const z = -Math.min(a, 2.6) * depth / 2.6;
          const edge = clamp((halfW - Math.abs(x) + cardW * 0.25) / (cardW * 0.5), 0, 1);
          const wrap = clamp((180 - Math.abs(th)) / (step * 0.55), 0, 1);
          c.style.transform = 'translate3d(' + x.toFixed(1) + 'px,0,' + z.toFixed(1) + 'px) rotateY(' + (-clamp(t, -1.6, 1.6) * 28).toFixed(2) + 'deg)';
          c.style.opacity = (edge * wrap).toFixed(3);
          c.style.setProperty('--dim', clamp(a * 0.3, 0, 0.62).toFixed(3));
          c.style.zIndex = String(100 - Math.round(a * 10));
        });
      };
      const frame = () => {
        if (!visible) { raf2 = 0; return; }
        if (!dragging) {
          if (snapTo !== null) {
            const d = snapTo - angle; angle += d * 0.12;
            if (Math.abs(d) < 0.05) { angle = snapTo; snapTo = null; }
          } else if (Math.abs(vel) > 0.02) { angle += vel; vel *= 0.94; }
          else if (performance.now() > idleT) { angle -= 0.14; }
        }
        carousel.style.setProperty('--spin', angle.toFixed(3) + 'deg');
        shade();
        raf2 = requestAnimationFrame(frame);
      };
      let raf2 = 0;
      const wake = () => { if (!raf2 && visible) raf2 = requestAnimationFrame(frame); };
      ringScene.addEventListener('pointerdown', (e) => {
        dragging = true; lastX = e.clientX; vel = 0; snapTo = null; ringScene.setPointerCapture(e.pointerId);
      });
      ringScene.addEventListener('pointermove', (e) => {
        if (!dragging) return;
        const dx = e.clientX - lastX; lastX = e.clientX;
        angle += dx * 0.35; vel = dx * 0.35; carousel.style.setProperty('--spin', angle + 'deg'); shade();
      });
      const release = () => { if (!dragging) return; dragging = false; idleT = performance.now() + 2500; };
      ringScene.addEventListener('pointerup', release);
      ringScene.addEventListener('pointercancel', release);
      ringScene.addEventListener('keydown', (e) => {
        if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
        e.preventDefault();
        const base = Math.round(angle / step) * step;
        snapTo = base + (e.key === 'ArrowLeft' ? step : -step); vel = 0; idleT = performance.now() + 4000; wake();
      });
      const setRadius = () => {
        const w = ringScene.clientWidth;
        const cw = parseFloat(getComputedStyle(ringScene).getPropertyValue('--card-w')) || 176;
        halfW = w / 2; cardW = cw;
        spacing = w < 600 ? cw * 0.62 : clamp(w * 0.13, cw * 0.72, cw * 0.95);
        depth = clamp(w * 0.28, 150, 320);
        shade();
      };
      setRadius(); window.addEventListener('resize', setRadius);
      shade();
      if ('IntersectionObserver' in window) {
        new IntersectionObserver((es) => es.forEach(e => { visible = e.isIntersecting; if (visible) wake(); })).observe(ringScene);
      } else { visible = true; wake(); }
    }
  }

  /* ------------------------------------------------------------ your agents: the terminal types itself */
  const termText = document.getElementById('agentTermText');
  if (termText) {
    const LINES = [
      ['cmd', 'fleetctl host --root ~/code/app'],
      ['out', '<span class="dim">[host]</span> Dev PC registered — folders: ~/code/app'],
      ['out', '<span class="dim">[host]</span> agent CLIs found: Claude Code, Codex, Hermes'],
      ['cmd', 'fleetctl agent add Claude --kind claude_code --reports-to Builder'],
      ['out', '<span class="ok">✓</span> Claude added (claude_code in ~/code/app)'],
      ['cmd', 'fleetctl ticket new "Fix the login test" --to Claude --review-by Checker'],
      ['out', '<span class="ok">✓</span> T-21 filed → Claude'],
      ['out', '<span class="dim">[host]</span> Claude: Edit: src/auth/login.test.ts'],
      ['out', '<span class="dim">[host]</span> Claude: done after 41s, $0.08 · shared src/auth/login.test.ts'],
    ];
    const P = '<span class="p">$</span> ';
    const full = LINES.map(([k, t]) => (k === 'cmd' ? P + t : t)).join('\n');
    if (reduced) { termText.innerHTML = full; }
    else {
      let started = false;
      const run = async () => {
        if (started) return; started = true;
        const sleep = (ms) => new Promise(r => setTimeout(r, ms));
        for (;;) {
          let done = '';
          for (const [k, t] of LINES) {
            if (k === 'cmd') {
              for (let i = 1; i <= t.length; i++) { termText.innerHTML = done + P + t.slice(0, i) + '<span class="cur"></span>'; await sleep(26); }
              done += P + t + '\n'; await sleep(420);
            } else { done += t + '\n'; termText.innerHTML = done + '<span class="cur"></span>'; await sleep(520); }
          }
          await sleep(5200);
        }
      };
      if ('IntersectionObserver' in window) {
        const io2 = new IntersectionObserver((es) => es.forEach(e => { if (e.isIntersecting) { run(); io2.disconnect(); } }), { threshold: 0.3 });
        io2.observe(termText);
      } else run();
    }
  }

})();
