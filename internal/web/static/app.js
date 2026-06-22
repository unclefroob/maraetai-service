import { md5 } from './md5.js';

const CLIENT = 'maraetai-web';
const API_VERSION = '1.16.1';
const SESSION_KEY = 'maraetai.creds';

// --- auth ---------------------------------------------------------------

let creds = null; // { username, password }

function loadCreds() {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function saveCreds(c) {
  creds = c;
  sessionStorage.setItem(SESSION_KEY, JSON.stringify(c));
}

function clearCreds() {
  creds = null;
  sessionStorage.removeItem(SESSION_KEY);
}

function randomSalt() {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// authParams returns a fresh Subsonic auth query (random salt + token) so the
// password never goes on the wire.
function authParams() {
  const salt = randomSalt();
  const params = new URLSearchParams({
    u: creds.username,
    t: md5(creds.password + salt),
    s: salt,
    c: CLIENT,
    v: API_VERSION,
    f: 'json',
  });
  return params;
}

// subsonicGet calls a Subsonic endpoint and returns its parsed body, throwing
// on a failed status.
async function subsonicGet(path, extra = {}) {
  const params = authParams();
  for (const [k, v] of Object.entries(extra)) params.set(k, v);
  const res = await fetch(`${path}?${params.toString()}`);
  const body = (await res.json())['subsonic-response'];
  if (!body || body.status !== 'ok') {
    const msg = body && body.error ? body.error.message : `request failed (${res.status})`;
    throw new Error(msg);
  }
  return body;
}

// coverArtURL builds an authenticated cover-art URL for an <img>.
function coverArtURL(id, size = 96) {
  const params = authParams();
  params.set('id', id);
  params.set('size', String(size));
  return `/rest/getCoverArt.view?${params.toString()}`;
}

// --- views --------------------------------------------------------------

const $ = (sel) => document.querySelector(sel);

function show(el) { el.classList.remove('hidden'); }
function hide(el) { el.classList.add('hidden'); }

function relativeTime(unixSeconds) {
  if (!unixSeconds) return '';
  const diff = Date.now() / 1000 - unixSeconds;
  const mins = Math.round(diff / 60);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.round(hrs / 24);
  if (days < 30) return `${days}d ago`;
  return new Date(unixSeconds * 1000).toLocaleDateString();
}

async function loadHistory() {
  const list = $('#history-list');
  const empty = $('#history-empty');
  list.innerHTML = '';
  hide(empty);
  const body = await subsonicGet('/rest/getRecentlyPlayed.view', { count: '100' });
  const songs = (body.recentlyPlayed && body.recentlyPlayed.song) || [];
  if (songs.length === 0) {
    show(empty);
    return;
  }
  for (const s of songs) {
    const row = document.createElement('div');
    row.className = 'row';
    const art = s.coverArt
      ? `<img loading="lazy" src="${coverArtURL(s.coverArt)}" alt="" />`
      : `<div class="noart"></div>`;
    row.innerHTML = `
      ${art}
      <div class="meta">
        <div class="title"></div>
        <div class="sub"></div>
      </div>
      <div class="when">${relativeTime(s.playedAt)}</div>`;
    row.querySelector('.title').textContent = s.title || 'Unknown';
    row.querySelector('.sub').textContent =
      [s.artist, s.album].filter(Boolean).join(' • ');
    list.appendChild(row);
  }
}

async function loadStats() {
  const days = $('#stats-days').value;
  const res = await fetch(`/api/stats?${authParams().toString()}&days=${days}`);
  if (!res.ok) throw new Error(`stats failed (${res.status})`);
  const s = await res.json();

  const minutes = Math.round((s.totalDurationSeconds || 0) / 60);
  $('#stats-totals').innerHTML = `
    ${statCard(s.totalPlays || 0, 'plays')}
    ${statCard(s.distinctSongs || 0, 'unique songs')}
    ${statCard(minutes.toLocaleString(), 'minutes')}`;

  renderRank($('#top-artists'), (s.topArtists || []).map((a) => [a.artist, a.plays]));
  renderRank($('#top-songs'), (s.topSongs || []).map((t) => [`${t.title} — ${t.artist}`, t.plays]));
  renderBars($('#by-day'), s.playsByDay || []);
}

function statCard(n, label) {
  return `<div class="stat"><div class="n">${n}</div><div class="l">${label}</div></div>`;
}

function renderRank(ol, items) {
  ol.innerHTML = '';
  if (items.length === 0) {
    ol.innerHTML = '<li class="muted">No data</li>';
    return;
  }
  for (const [label, count] of items) {
    const li = document.createElement('li');
    const span = document.createElement('span');
    span.textContent = label;
    li.appendChild(span);
    li.insertAdjacentHTML('beforeend', ` <span class="c">${count}</span>`);
    ol.appendChild(li);
  }
}

function renderBars(container, byDay) {
  container.innerHTML = '';
  const max = byDay.reduce((m, d) => Math.max(m, d.plays), 0) || 1;
  for (const d of byDay) {
    const bar = document.createElement('div');
    bar.className = 'bar';
    bar.style.height = `${(d.plays / max) * 100}%`;
    bar.title = `${d.day}: ${d.plays} plays`;
    container.appendChild(bar);
  }
}

// --- wiring -------------------------------------------------------------

function switchTab(name) {
  for (const btn of document.querySelectorAll('.tabs button')) {
    btn.classList.toggle('active', btn.dataset.tab === name);
  }
  $('#tab-history').classList.toggle('hidden', name !== 'history');
  $('#tab-stats').classList.toggle('hidden', name !== 'stats');
  const loader = name === 'history' ? loadHistory : loadStats;
  loader().catch((e) => alert(e.message));
}

function enterApp() {
  hide($('#login'));
  show($('#app'));
  $('#who').textContent = creds.username;
  switchTab('history');
}

async function attemptLogin(c) {
  saveCreds(c);
  await subsonicGet('/rest/ping.view'); // throws if creds are wrong
}

function initEvents() {
  $('#login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const err = $('#login-error');
    hide(err);
    try {
      await attemptLogin({ username: $('#username').value, password: $('#password').value });
      enterApp();
    } catch (ex) {
      clearCreds();
      err.textContent = ex.message;
      show(err);
    }
  });

  $('#logout').addEventListener('click', () => {
    clearCreds();
    location.reload();
  });

  for (const btn of document.querySelectorAll('.tabs button')) {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
  }
  $('#stats-days').addEventListener('change', () => loadStats().catch((e) => alert(e.message)));
}

async function boot() {
  initEvents();
  creds = loadCreds();
  if (creds) {
    try {
      await subsonicGet('/rest/ping.view');
      enterApp();
      return;
    } catch {
      clearCreds();
    }
  }
  show($('#login'));
}

boot();
