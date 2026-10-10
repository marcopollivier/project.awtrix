import { glyph, normalize, textWidth } from './font.js';
import { playRTTTL } from './rtttl.js';

const W = 32, H = 8;
const LED = 26;              // canvas px per LED
const SCROLL_MS = 60;        // ms per pixel, close to the clock's default speed
const TEXT_Y = 1;            // first text row (5 rows tall)

const canvas = document.getElementById('matrix');
canvas.width = W * LED;
canvas.height = H * LED;
const ctx = canvas.getContext('2d');

let screen = { kind: 'empty', loop: [] };
let shownAt = performance.now();
let soundOn = false;
const icons = new Map(); // id -> {frames} | 'loading' | 'error'

// ---------- pixel buffer ----------
const buf = new Array(W * H);
function clear() { buf.fill(null); }
function set(x, y, color) {
  if (x >= 0 && x < W && y >= 0 && y < H) buf[y * W + x] = color;
}

function paint() {
  ctx.fillStyle = '#050505';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      const c = buf[y * W + x];
      const cx = x * LED + LED / 2, cy = y * LED + LED / 2;
      ctx.beginPath();
      ctx.arc(cx, cy, LED * 0.38, 0, Math.PI * 2);
      if (c) {
        ctx.shadowColor = c;
        ctx.shadowBlur = LED * 0.6;
        ctx.fillStyle = c;
      } else {
        ctx.shadowBlur = 0;
        ctx.fillStyle = '#141414';
      }
      ctx.fill();
    }
  }
  ctx.shadowBlur = 0;
}

// ---------- drawing ----------
function hue(h) { return `hsl(${h % 360} 100% 55%)`; }

function fragments(app) {
  const t = app.text;
  if (Array.isArray(t)) return t.map(f => ({ t: f.t, c: f.c || app.color || '#FFFFFF' }));
  return [{ t: t || '', c: app.color || '#FFFFFF' }];
}

function drawText(app, x0, areaX, areaW, now) {
  const frags = fragments(app);
  let x = x0, i = 0;
  for (const f of frags) {
    for (const ch of normalize(f.t)) {
      const g = glyph(ch);
      const color = app.rainbow ? hue(i * 30 + now / 8) : f.c;
      for (let r = 0; r < 5; r++) {
        for (let c = 0; c < g[r].length; c++) {
          const px = x + c;
          if (g[r][c] === '#' && px >= areaX && px < areaX + areaW) set(px, TEXT_Y + r, color);
        }
      }
      x += g[0].length + 1;
      i++;
    }
  }
}

function drawIcon(id, now) {
  const ic = loadIcon(id);
  if (!ic || ic === 'loading' || ic === 'error') {
    for (let y = 0; y < 8; y++) for (let x = 0; x < 8; x++) if ((x + y) % 7 === 0) set(x, y, '#333');
    return;
  }
  const total = ic.frames.reduce((s, f) => s + (f.delay || 100), 0);
  let t = ic.frames.length > 1 ? (now - shownAt) % total : 0;
  let frame = ic.frames[0];
  for (const f of ic.frames) {
    t -= f.delay || 100;
    frame = f;
    if (t < 0) break;
  }
  for (let y = 0; y < Math.min(8, ic.h); y++)
    for (let x = 0; x < Math.min(8, ic.w); x++) {
      const c = frame.pixels[y * ic.w + x];
      if (c) set(x, y, c);
    }
}

function loadIcon(id) {
  if (!icons.has(id)) {
    icons.set(id, 'loading');
    fetch(`/icons/${encodeURIComponent(id)}`)
      .then(r => (r.ok ? r.json() : Promise.reject(r.status)))
      .then(ic => icons.set(id, ic))
      .catch(() => icons.set(id, 'error'));
  }
  return icons.get(id);
}

function drawProgress(app, areaX) {
  if (app.progress === undefined || app.progress === null || app.progress < 0) return;
  const w = W - areaX;
  const filled = Math.round((w * Math.min(100, app.progress)) / 100);
  for (let x = 0; x < w; x++) set(areaX + x, 7, x < filled ? (app.progressC || '#00FF00') : (app.progressBC || '#202020'));
}

function render(now) {
  clear();
  const app = screen.app;
  if (app) {
    const hasIcon = !!app.icon;
    const areaX = hasIcon ? 9 : 0;
    const areaW = W - areaX;
    if (hasIcon) drawIcon(app.icon, now);

    const tw = textWidth(typeof app.text === 'string' ? app.text : fragments(app).map(f => f.t).join(''));
    const blinkOff = app.blinkText && Math.floor((now - shownAt) / app.blinkText) % 2 === 1;
    if (!blinkOff) {
      let x;
      if (tw <= areaW || app.noScroll) {
        x = app.center === false ? areaX : areaX + Math.floor((areaW - tw) / 2);
      } else {
        const span = tw + areaW;
        x = areaX + areaW - (Math.floor((now - shownAt) / SCROLL_MS) % span);
      }
      drawText(app, x, areaX, areaW, now);
    }
    drawProgress(app, areaX);
  } else {
    // idle clock, like the built-in time app
    const d = new Date();
    const t = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
    drawText({ text: t, color: '#555' }, Math.floor((W - textWidth(t)) / 2), 0, W, now);
  }
  paint();
  requestAnimationFrame(render);
}

// ---------- panel ----------
const $ = sel => document.querySelector(sel);

function age(iso) {
  const s = Math.round((Date.now() - new Date(iso)) / 1000);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}min`;
  return `${Math.round(s / 3600)}h`;
}

function updatePanel() {
  const label = screen.kind === 'notify' ? `notificação${screen.hold ? ' (hold)' : ''}`
    : screen.kind === 'app' ? `app: ${screen.name}` : 'vazio';
  $('#now').textContent = label + (screen.queue ? ` · ${screen.queue} na fila` : '');
  $('#payload').textContent = screen.app ? JSON.stringify(screen.app, null, 2) : '—';
  const list = $('#loop');
  list.replaceChildren(...(screen.loop || []).map(it => {
    const li = document.createElement('li');
    li.className = it.current ? 'current' : '';
    li.innerHTML = `<span class="name"></span><span class="text"></span><span class="age"></span>`;
    li.querySelector('.name').textContent = it.name;
    li.querySelector('.text').textContent = it.text;
    li.querySelector('.age').textContent = age(it.updatedAt);
    return li;
  }));
  if (!screen.loop?.length) list.innerHTML = '<li class="muted">Nenhum app ainda. Rode <code>awtrix-hub run</code> ou use o botão de teste.</li>';
}

function log(entry) {
  const li = document.createElement('li');
  li.innerHTML = `<span class="time"></span><span class="kind"></span><code></code>`;
  li.querySelector('.time').textContent = entry.time;
  li.querySelector('.kind').textContent = entry.kind + (entry.name ? ` ${entry.name}` : '');
  li.querySelector('code').textContent = entry.body;
  const ul = $('#log');
  ul.prepend(li);
  while (ul.children.length > 50) ul.lastChild.remove();
}

// ---------- events ----------
function connect() {
  const es = new EventSource('/events');
  es.onopen = () => $('#status').classList.add('ok');
  es.onerror = () => $('#status').classList.remove('ok');
  es.onmessage = ev => {
    const msg = JSON.parse(ev.data);
    if (msg.type === 'screen') {
      const changed = msg.data.kind !== screen.kind || msg.data.name !== screen.name ||
        JSON.stringify(msg.data.app) !== JSON.stringify(screen.app);
      const wasNotify = msg.data.kind === 'notify' && changed;
      screen = msg.data;
      if (changed) shownAt = performance.now();
      if (wasNotify && screen.sound && soundOn) playRTTTL(screen.sound);
      updatePanel();
    } else if (msg.type === 'loop') {
      screen.loop = msg.data.loop;
      screen.queue = msg.data.queue;
      updatePanel();
    } else if (msg.type === 'log') {
      log(msg.data);
    }
  };
}

const post = (path, body) => fetch(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
$('#next').onclick = () => post('/api/nextapp');
$('#dismiss').onclick = () => post('/api/notify/dismiss');
$('#test').onclick = () => post('/api/notify', {
  text: 'GOOOL DO VASCO!', icon: '8530', rainbow: true, repeat: 1,
  rtttl: 'gol:d=8,o=5,b=200:c6,c6,c6,4g,a,4c6,p,c6,c6,c6,4g,a,2c6',
});
$('#sound').onchange = e => { soundOn = e.target.checked; };
$('#send').onclick = async () => {
  const name = $('#appname').value.trim();
  const body = $('#appjson').value;
  const r = name ? await fetch(`/api/custom?name=${encodeURIComponent(name)}`, { method: 'POST', body })
    : await fetch('/api/notify', { method: 'POST', body });
  $('#sendstatus').textContent = r.ok ? 'ok' : await r.text();
};

setInterval(updatePanel, 5000);
connect();
requestAnimationFrame(render);
