// Web player + admin shell. Hash-routed vanilla-JS SPA over the proxy's Subsonic
// + maraetai endpoints. Playback/queue/scrobble live in player.js; API in api.js.
import * as api from './api.js';
import * as player from './player.js';

const $ = (sel) => document.querySelector(sel);
const view = () => $('#view');
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

let isAdmin = false;
let navidromeUrl = '';

// --- shared render helpers ---------------------------------------------

function songRowsHTML(songs) {
  return songs.map((s, i) => `
    <div class="trow" data-idx="${i}">
      <span class="tnum">${i + 1}</span>
      <div class="tmeta">
        <div class="ttitle">${esc(s.title || 'Unknown')}</div>
        <div class="tsub muted">${esc([s.artist, s.album].filter(Boolean).join(' • '))}</div>
      </div>
      <span class="tdur muted">${fmtDur(s.duration)}</span>
    </div>`).join('');
}

// Wire a rendered track list so a row plays the whole list from that point.
function wireSongRows(container, songs) {
  container.querySelectorAll('.trow').forEach((row) => {
    row.addEventListener('click', () => player.play(songs, Number(row.dataset.idx)));
  });
}

function songCardsHTML(songs) {
  return songs.map((s, i) => `
    <button class="card song" data-idx="${i}">
      ${artHTML(s.coverArt, 160)}
      <div class="cname">${esc(s.title || 'Unknown')}</div>
      <div class="csub muted">${esc(s.artist || '')}</div>
      ${s.reason ? `<div class="creason muted">${esc(s.reason)}</div>` : ''}
    </button>`).join('');
}

function albumCardsHTML(albums) {
  return albums.map((a) => `
    <a class="card album" href="#/album/${encodeURIComponent(a.id)}">
      ${artHTML(a.coverArt, 200)}
      <div class="cname">${esc(a.name)}</div>
      <div class="csub muted">${esc(a.artist || '')}</div>
    </a>`).join('');
}

function artHTML(id, size) {
  return id
    ? `<img class="art" loading="lazy" src="${api.coverArtURL(id, size)}" alt="" />`
    : `<div class="art noart"></div>`;
}

function shelfHTML(title, inner) {
  return `<section class="shelf"><h2>${esc(title)}</h2><div class="scroller">${inner}</div></section>`;
}

function fmtDur(sec) {
  if (!sec) return '';
  const m = Math.floor(sec / 60);
  return `${m}:${String(Math.floor(sec % 60)).padStart(2, '0')}`;
}

function loading() { view().innerHTML = '<div class="loading">Loading…</div>'; }
function fail(e) { view().innerHTML = `<div class="error">${esc(e.message || e)}</div>`; }

// --- views --------------------------------------------------------------

async function renderHome() {
  loading();
  const [onRep, forYou, recents, newest, frequent] = await Promise.all([
    api.onRepeat(20).catch(() => []),
    api.songsForYou(20).catch(() => []),
    api.recentlyPlayed(30).catch(() => []),
    api.albumList('newest', 20).catch(() => []),
    api.albumList('frequent', 20).catch(() => []),
  ]);
  const sections = [];
  if (onRep.length) sections.push(['On Repeat', songCardsHTML(onRep), onRep]);
  if (forYou.length) sections.push(['Songs for you', songCardsHTML(forYou), forYou]);
  if (recents.length) sections.push(['Recently played', songCardsHTML(recents), recents]);
  if (newest.length) sections.push(['Recently added', albumCardsHTML(newest), null]);
  if (frequent.length) sections.push(['Most played', albumCardsHTML(frequent), null]);

  view().innerHTML = sections.length
    ? sections.map(([t, html]) => shelfHTML(t, html)).join('')
    : '<div class="empty muted">Nothing here yet — play some music.</div>';

  // Wire song shelves (album shelves are links).
  view().querySelectorAll('.shelf').forEach((sec, i) => {
    const songs = sections[i][2];
    if (!songs) return;
    sec.querySelectorAll('.song').forEach((card) => {
      card.addEventListener('click', () => player.play(songs, Number(card.dataset.idx)));
    });
  });
}

const LIB_TABS = [['albums', 'Albums'], ['artists', 'Artists']];
let libTab = 'albums';

async function renderLibrary() {
  view().innerHTML = `
    <div class="subtabs">
      ${LIB_TABS.map(([k, l]) => `<button data-lib="${k}" class="${k === libTab ? 'active' : ''}">${l}</button>`).join('')}
    </div>
    <div id="lib-body"></div>`;
  view().querySelectorAll('[data-lib]').forEach((b) =>
    b.addEventListener('click', () => { libTab = b.dataset.lib; renderLibrary(); }));
  const body = $('#lib-body');
  body.innerHTML = '<div class="loading">Loading…</div>';
  try {
    if (libTab === 'albums') {
      const albums = await api.albumList('alphabeticalByName', 100);
      body.innerHTML = `<div class="grid">${albumCardsHTML(albums)}</div>`;
    } else {
      const artists = await api.artistsIndex();
      body.innerHTML = `<div class="list">${artists.map((a) => `
        <a class="lrow" href="#/artist/${encodeURIComponent(a.id)}">
          ${artHTML(a.coverArt, 80)}
          <div class="tmeta"><div class="ttitle">${esc(a.name)}</div>
          <div class="tsub muted">${a.albumCount || 0} albums</div></div>
        </a>`).join('')}</div>`;
    }
  } catch (e) { body.innerHTML = `<div class="error">${esc(e.message)}</div>`; }
}

let myMusicTab = 'favourites';
async function renderMyMusic() {
  view().innerHTML = `
    <h1 class="page-title">My Music</h1>
    <div class="subtabs">
      <button data-mm="favourites" class="${myMusicTab === 'favourites' ? 'active' : ''}">Favourites</button>
      <button data-mm="recents" class="${myMusicTab === 'recents' ? 'active' : ''}">Recently played</button>
    </div>
    <div id="mm-body"></div>`;
  view().querySelectorAll('[data-mm]').forEach((b) =>
    b.addEventListener('click', () => { myMusicTab = b.dataset.mm; renderMyMusic(); }));
  const body = $('#mm-body');
  body.innerHTML = '<div class="loading">Loading…</div>';
  try {
    const songs = myMusicTab === 'favourites'
      ? (await api.starred()).song || []
      : await api.recentlyPlayed(100);
    if (!songs.length) { body.innerHTML = '<div class="empty muted">Nothing here yet.</div>'; return; }
    body.innerHTML = `
      <div class="dh-actions list-actions">
        <button id="mm-play" class="primary">▶ Play</button>
        <button id="mm-shuffle" class="ghost">🔀 Shuffle</button>
      </div>
      <div class="tracklist">${songRowsHTML(songs)}</div>`;
    wireSongRows(body, songs);
    $('#mm-play').addEventListener('click', () => player.play(songs, 0));
    $('#mm-shuffle').addEventListener('click', () => player.play(shuffle(songs), 0));
  } catch (e) { body.innerHTML = `<div class="error">${esc(e.message)}</div>`; }
}

async function renderPlaylists() {
  loading();
  const pls = await api.playlists();
  view().innerHTML = `<h1 class="page-title">Playlists</h1>` + (pls.length
    ? `<div class="grid">${pls.map((p) => `
        <a class="card album" href="#/playlist/${encodeURIComponent(p.id)}">
          ${artHTML(p.coverArt, 200)}
          <div class="cname">${esc(p.name)}</div>
          <div class="csub muted">${p.songCount || 0} songs</div>
        </a>`).join('')}</div>`
    : '<div class="empty muted">No playlists yet.</div>');
}

async function renderSidebarPlaylists() {
  const el = $('#sidebar-playlists');
  if (!el) return;
  try {
    const pls = await api.playlists();
    el.innerHTML = pls.map((p) =>
      `<a class="spl" href="#/playlist/${encodeURIComponent(p.id)}">${esc(p.name)}</a>`).join('');
  } catch { el.innerHTML = ''; }
}

async function renderAlbum(id) {
  loading();
  const a = await api.album(id);
  if (!a) return fail(new Error('Album not found'));
  const songs = a.song || [];
  view().innerHTML = `
    <div class="detail-head">
      ${artHTML(a.coverArt, 300)}
      <div class="dh-meta">
        <div class="dh-kind muted">ALBUM</div>
        <h1>${esc(a.name)}</h1>
        <div class="muted">${esc(a.artist || '')}${a.year ? ' • ' + a.year : ''} • ${songs.length} songs</div>
        <div class="dh-actions">
          <button id="play-all" class="primary">▶ Play</button>
          <button id="shuffle-all" class="ghost">🔀 Shuffle</button>
        </div>
      </div>
    </div>
    <div class="tracklist">${songRowsHTML(songs)}</div>`;
  wireSongRows(view(), songs);
  $('#play-all').addEventListener('click', () => player.play(songs, 0));
  $('#shuffle-all').addEventListener('click', () => player.play(shuffle(songs), 0));
}

async function renderArtist(id) {
  loading();
  const a = await api.artist(id);
  if (!a) return fail(new Error('Artist not found'));
  const albums = a.album || [];
  view().innerHTML = `
    <div class="detail-head">
      ${artHTML(a.coverArt, 300)}
      <div class="dh-meta">
        <div class="dh-kind muted">ARTIST</div>
        <h1>${esc(a.name)}</h1>
        <div class="muted">${albums.length} albums</div>
      </div>
    </div>
    <div class="grid">${albumCardsHTML(albums)}</div>`;
}

async function renderPlaylist(id) {
  loading();
  const p = await api.playlist(id);
  if (!p) return fail(new Error('Playlist not found'));
  const songs = p.entry || [];
  view().innerHTML = `
    <div class="detail-head">
      ${artHTML(p.coverArt, 300)}
      <div class="dh-meta">
        <div class="dh-kind muted">PLAYLIST</div>
        <h1>${esc(p.name)}</h1>
        <div class="muted">${songs.length} songs</div>
        <div class="dh-actions">
          <button id="play-all" class="primary">▶ Play</button>
          <button id="shuffle-all" class="ghost">🔀 Shuffle</button>
        </div>
      </div>
    </div>
    <div class="tracklist">${songRowsHTML(songs)}</div>`;
  wireSongRows(view(), songs);
  $('#play-all').addEventListener('click', () => player.play(songs, 0));
  $('#shuffle-all').addEventListener('click', () => player.play(shuffle(songs), 0));
}

function renderSearch() {
  view().innerHTML = `
    <div class="searchbar"><input id="q" type="search" placeholder="Search artists, albums, songs…" autofocus /></div>
    <div id="results"></div>`;
  let timer;
  $('#q').addEventListener('input', (e) => {
    clearTimeout(timer);
    const q = e.target.value.trim();
    timer = setTimeout(() => runSearch(q), 250);
  });
}

async function runSearch(q) {
  const out = $('#results');
  if (!q) { out.innerHTML = ''; return; }
  out.innerHTML = '<div class="loading">Searching…</div>';
  try {
    const r = await api.search(q);
    const parts = [];
    if ((r.artist || []).length) parts.push(shelfHTML('Artists', r.artist.map((a) => `
      <a class="card album" href="#/artist/${encodeURIComponent(a.id)}">${artHTML(a.coverArt, 160)}
      <div class="cname">${esc(a.name)}</div></a>`).join('')));
    if ((r.album || []).length) parts.push(shelfHTML('Albums', albumCardsHTML(r.album)));
    const songs = r.song || [];
    if (songs.length) parts.push(`<section class="shelf"><h2>Songs</h2><div class="tracklist">${songRowsHTML(songs)}</div></section>`);
    out.innerHTML = parts.length ? parts.join('') : '<div class="empty muted">No results.</div>';
    if (songs.length) wireSongRows(out, songs);
  } catch (e) { out.innerHTML = `<div class="error">${esc(e.message)}</div>`; }
}

async function renderAdmin() {
  if (!isAdmin) { view().innerHTML = '<div class="empty muted">Admin access required.</div>'; return; }
  loading();
  const days = 365;
  const [history, s] = await Promise.all([
    api.recentlyPlayed(100).catch(() => []),
    api.stats(days).catch(() => null),
  ]);
  const minutes = s ? Math.round((s.totalDurationSeconds || 0) / 60) : 0;
  const link = navidromeUrl
    ? `<a class="primary" href="${esc(navidromeUrl)}" target="_blank" rel="noopener">Manage users in Navidrome ↗</a>`
    : `<span class="muted">Set <code>NAVIDROME_PUBLIC_URL</code> on the service to link here; manage users in Navidrome's own admin UI.</span>`;
  view().innerHTML = `
    <div class="admin-head"><h1>Admin</h1>${link}</div>
    ${s ? `<div class="totals">
      ${stat(s.totalPlays || 0, 'plays')}
      ${stat(s.distinctSongs || 0, 'unique songs')}
      ${stat(minutes.toLocaleString(), 'minutes')}
    </div>
    <div class="cols">
      <div><h3>Top artists</h3><ol class="rank">${rank((s.topArtists || []).map((a) => [a.artist, a.plays]))}</ol></div>
      <div><h3>Top songs</h3><ol class="rank">${rank((s.topSongs || []).map((t) => [`${t.title} — ${t.artist}`, t.plays]))}</ol></div>
    </div>` : ''}
    <h3>Recently played (all users on this server)</h3>
    <div class="tracklist">${songRowsHTML(history)}</div>`;
  wireSongRows(view(), history);
}

function stat(n, label) { return `<div class="stat"><div class="n">${n}</div><div class="l">${label}</div></div>`; }
function rank(items) {
  if (!items.length) return '<li class="muted">No data</li>';
  return items.map(([label, c]) => `<li><span>${esc(label)}</span><span class="c">${c}</span></li>`).join('');
}

function shuffle(arr) {
  const a = arr.slice();
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

// --- routing ------------------------------------------------------------

function route() {
  const hash = location.hash.replace(/^#\/?/, '') || 'home';
  const [name, arg] = hash.split('/');
  for (const b of document.querySelectorAll('[data-route]')) {
    b.classList.toggle('active', b.dataset.route === name);
  }
  const run = {
    home: renderHome, library: renderLibrary, search: renderSearch,
    mymusic: renderMyMusic, playlists: renderPlaylists, admin: renderAdmin,
    album: () => renderAlbum(decodeURIComponent(arg)),
    artist: () => renderArtist(decodeURIComponent(arg)),
    playlist: () => renderPlaylist(decodeURIComponent(arg)),
  }[name] || renderHome;
  Promise.resolve(run()).catch(fail);
}

// --- auth / boot --------------------------------------------------------

async function enterApp() {
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  $('#who').textContent = api.currentUsername();
  player.init();

  // Admin gate + config link-out (best-effort; failures just hide admin).
  try {
    const [user, cfg] = await Promise.all([
      api.getUser(api.currentUsername()), api.appConfig(),
    ]);
    isAdmin = user.adminRole === true || user.adminRole === 'true';
    navidromeUrl = (cfg && cfg.navidromeUrl) || '';
  } catch { isAdmin = false; }
  $('#nav-admin').classList.toggle('hidden', !isAdmin);
  renderSidebarPlaylists();

  if (!location.hash) location.hash = '#/home';
  else route();
}

function initEvents() {
  $('#login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const err = $('#login-error');
    err.classList.add('hidden');
    try {
      api.saveCreds({ username: $('#username').value, password: $('#password').value });
      await api.ping();
      await enterApp();
    } catch (ex) {
      api.clearCreds();
      err.textContent = ex.message;
      err.classList.remove('hidden');
    }
  });
  $('#logout').addEventListener('click', () => { api.clearCreds(); location.hash = ''; location.reload(); });
  for (const b of document.querySelectorAll('[data-route]')) {
    b.addEventListener('click', () => { location.hash = `#/${b.dataset.route}`; });
  }
  window.addEventListener('hashchange', route);
}

async function boot() {
  initEvents();
  if (api.loadCreds()) {
    try { await api.ping(); await enterApp(); return; } catch { api.clearCreds(); }
  }
  $('#login').classList.remove('hidden');
}

boot();
