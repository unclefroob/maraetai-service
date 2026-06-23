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

// Monochrome button glyphs (respect currentColor) — no coloured emoji.
const SVG_PLAY = '<svg class="bi" viewBox="0 0 24 24" fill="currentColor"><polygon points="6 4 20 12 6 20 6 4"/></svg>';
const SVG_SHUFFLE = '<svg class="bi" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 3 21 3 21 8"/><line x1="4" y1="20" x2="21" y2="3"/><polyline points="21 16 21 21 16 21"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/></svg>';

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
      <div class="art-wrap">
        ${artHTML(a.coverArt, 200)}
        <button class="card-play" data-play-album="${esc(a.id)}" title="Play">${SVG_PLAY}</button>
      </div>
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

function greeting() {
  const h = new Date().getHours();
  if (h < 12) return 'Good morning';
  if (h < 17) return 'Good afternoon';
  return 'Good evening';
}

// --- Home hero gradient (dominant colour sampled from the cover) ------------

let heroTimer = null;
const gradCache = {};

function clearHero() { if (heroTimer) { clearInterval(heroTimer); heroTimer = null; } }

function setHeroGradient(el, coverArt) {
  if (!coverArt) { el.style.background = 'linear-gradient(160deg, #2a2a2a, #0a0a0a)'; return; }
  if (gradCache[coverArt]) { el.style.background = gradCache[coverArt]; return; }
  const img = new Image();
  img.onload = () => {
    try {
      const c = document.createElement('canvas');
      c.width = c.height = 16;
      const ctx = c.getContext('2d');
      ctx.drawImage(img, 0, 0, 16, 16);
      const d = ctx.getImageData(0, 0, 16, 16).data;
      let r = 0, g = 0, b = 0, n = 0;
      for (let p = 0; p < d.length; p += 4) { r += d[p]; g += d[p + 1]; b += d[p + 2]; n++; }
      const dk = (v, amt) => Math.round((v / n) * (1 - amt)); // average, mixed toward black
      const grad = `linear-gradient(160deg, rgb(${dk(r, .42)},${dk(g, .42)},${dk(b, .42)}), rgb(${dk(r, .78)},${dk(g, .78)},${dk(b, .78)}))`;
      gradCache[coverArt] = grad;
      el.style.background = grad;
    } catch { el.style.background = 'linear-gradient(160deg, #2a2a2a, #0a0a0a)'; }
  };
  img.src = api.coverArtURL(coverArt, 160); // same-origin → canvas readable
}

const heroShell = `
  <section class="hero" id="hero">
    <div class="hero-main">
      <div class="hero-text">
        <div class="hero-kicker">ON REPEAT</div>
        <div class="hero-title" id="hero-title"></div>
        <div class="hero-sub" id="hero-sub"></div>
        <div class="hero-note" id="hero-note">The songs you keep coming back to</div>
      </div>
      <img class="hero-art" id="hero-art" alt="" />
    </div>
    <div class="hero-actions">
      <button class="hero-pill play" id="hero-play">${SVG_PLAY}Play</button>
      <button class="hero-pill" id="hero-shuffle">${SVG_SHUFFLE}Shuffle</button>
    </div>
  </section>`;

// Song-level On Repeat hero (maraetai getOnRepeat): rotates the spotlight through
// the list; Play starts from the spotlighted song. Tap opens the full list.
function renderSongHero(songs) {
  const slot = $('#hero-slot');
  if (!slot) return;
  slot.innerHTML = heroShell;
  const hero = $('#hero'), titleEl = $('#hero-title'), subEl = $('#hero-sub'), artEl = $('#hero-art');
  let i = 0;
  const show = () => {
    if (!document.body.contains(hero)) { clearHero(); return; }
    const s = songs[i];
    titleEl.textContent = s.title || 'On Repeat';
    subEl.textContent = s.artist || '';
    artEl.src = s.coverArt ? api.coverArtURL(s.coverArt, 160) : '';
    setHeroGradient(hero, s.coverArt);
  };
  show();
  hero.addEventListener('click', (e) => { if (!e.target.closest('.hero-pill')) location.hash = '#/onrepeat'; });
  $('#hero-play').addEventListener('click', (e) => { e.stopPropagation(); player.play(songs, i); });
  $('#hero-shuffle').addEventListener('click', (e) => { e.stopPropagation(); player.play(shuffle(songs), 0); });
  clearHero();
  if (songs.length > 1) heroTimer = setInterval(() => { i = (i + 1) % songs.length; show(); }, 6000);
}

// Fallback when there's no replay history yet: spotlight the top frequent album.
function renderAlbumHero(a) {
  const slot = $('#hero-slot');
  if (!slot) return;
  slot.innerHTML = heroShell;
  const stat = a.playCount ? `Played ${a.playCount} times` : (a.year ? String(a.year) : `${a.songCount || 0} songs`);
  $('#hero-title').textContent = a.name;
  $('#hero-sub').textContent = a.artist || '';
  $('#hero-note').textContent = stat;
  $('#hero-art').src = a.coverArt ? api.coverArtURL(a.coverArt, 160) : '';
  setHeroGradient($('#hero'), a.coverArt);
  $('#hero').addEventListener('click', (e) => { if (!e.target.closest('.hero-pill')) location.hash = `#/album/${encodeURIComponent(a.id)}`; });
  const playAlbum = async (sh) => { const al = await api.album(a.id); if (al && al.song) player.play(sh ? shuffle(al.song) : al.song, 0); };
  $('#hero-play').addEventListener('click', (e) => { e.stopPropagation(); playAlbum(false); });
  $('#hero-shuffle').addEventListener('click', (e) => { e.stopPropagation(); playAlbum(true); });
}

// Songs for You as one "Daily Mix" tile (maraetai getSongsForYou).
function renderDailyMix(tracks) {
  const slot = $('#mix-slot');
  if (!slot) return;
  const ids = [...new Set(tracks.map((t) => t.coverArt).filter(Boolean))].slice(0, 4);
  const collage = ids.length >= 4
    ? `<div class="mix-collage">${ids.map((id) => `<img src="${api.coverArtURL(id, 96)}" alt="" />`).join('')}</div>`
    : `<div class="mix-collage one">${ids[0] ? `<img src="${api.coverArtURL(ids[0], 160)}" alt="" />` : ''}</div>`;
  slot.innerHTML = `
    <section class="mix-tile">
      <a class="mix-info" href="#/songsforyou">
        ${collage}
        <div>
          <div class="mix-kicker">SONGS FOR YOU</div>
          <div class="mix-title">Updated daily</div>
          <div class="mix-sub muted">${tracks.length} hand-picked tracks</div>
        </div>
      </a>
      <button class="mix-play" id="mix-play" title="Play">${SVG_PLAY}</button>
    </section>`;
  $('#mix-play').addEventListener('click', () => player.play(tracks, 0));
}

async function renderHome() {
  loading();
  const [onRep, forYou, frequent, recent, newest, random] = await Promise.all([
    api.onRepeat(30).catch(() => []),
    api.songsForYou(24).catch(() => []),
    api.albumList('frequent', 20).catch(() => []),
    api.albumList('recent', 20).catch(() => []),
    api.albumList('newest', 20).catch(() => []),
    api.albumList('random', 10).catch(() => []),
  ]);
  const recentIds = new Set(recent.map((a) => a.id));
  const unplayed = newest.filter((a) => !(a.playCount > 0)).slice(0, 10);   // New in your library
  const jumpBack = frequent.filter((a) => !recentIds.has(a.id)).slice(0, 10);

  const parts = [`<h1 class="page-title">${greeting()}</h1>`, '<div id="hero-slot"></div>'];
  if (forYou.length) parts.push('<div id="mix-slot"></div>');
  if (unplayed.length) parts.push(shelfHTML('New in your library', albumCardsHTML(unplayed)));
  if (jumpBack.length) parts.push(shelfHTML('Jump back in', albumCardsHTML(jumpBack)));
  if (random.length) parts.push(shelfHTML('Random mix', albumCardsHTML(random)));
  if (newest.length) parts.push(shelfHTML('Recently Added', albumCardsHTML(newest)));
  view().innerHTML = parts.join('');

  // On Repeat: song hero on maraetai (with replay history), else album fallback.
  if (onRep.length) renderSongHero(onRep);
  else if (frequent.length) renderAlbumHero(frequent[0]);
  if (forYou.length) renderDailyMix(forYou);
}

// Full lists behind the hero / mix tile.
async function renderSongCollection(title, fetcher, note) {
  loading();
  const songs = await fetcher();
  view().innerHTML = `<h1 class="page-title">${esc(title)}</h1>`
    + (note ? `<p class="bio muted">${esc(note)}</p>` : '')
    + (songs.length ? `
      <div class="dh-actions list-actions">
        <button id="col-play" class="primary">${SVG_PLAY}Play</button>
        <button id="col-shuffle" class="ghost">${SVG_SHUFFLE}Shuffle</button>
      </div>
      <div class="tracklist">${songRowsHTML(songs)}</div>`
      : '<div class="empty muted">Nothing here yet.</div>');
  wireSongRows(view(), songs);
  if (songs.length) {
    $('#col-play').addEventListener('click', () => player.play(songs, 0));
    $('#col-shuffle').addEventListener('click', () => player.play(shuffle(songs), 0));
  }
}
const renderOnRepeat = () => renderSongCollection('On Repeat', () => api.onRepeat(100), 'The songs you keep coming back to.');
const renderSongsForYou = () => renderSongCollection('Songs for You', () => api.songsForYou(50), 'A fresh mix, updated daily.');

const LIB_TABS = [['albums', 'Albums'], ['artists', 'Artists'], ['genres', 'Genres']];
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
    } else if (libTab === 'artists') {
      const artists = await api.artistsIndex();
      body.innerHTML = `<div class="list">${artists.map((a) => `
        <a class="lrow" href="#/artist/${encodeURIComponent(a.id)}">
          ${artHTML(a.coverArt, 80)}
          <div class="tmeta"><div class="ttitle">${esc(a.name)}</div>
          <div class="tsub muted">${a.albumCount || 0} albums</div></div>
        </a>`).join('')}</div>`;
    } else {
      const gs = await api.genres();
      gs.sort((a, b) => (b.albumCount || 0) - (a.albumCount || 0));
      body.innerHTML = gs.length
        ? `<div class="list">${gs.map((g) => `
            <a class="lrow genre" href="#/genre/${encodeURIComponent(g.value)}">
              <div class="genre-ic">♫</div>
              <div class="tmeta"><div class="ttitle">${esc(g.value)}</div>
              <div class="tsub muted">${g.songCount || 0} songs · ${g.albumCount || 0} albums</div></div>
            </a>`).join('')}</div>`
        : '<div class="empty muted">No genres.</div>';
    }
  } catch (e) { body.innerHTML = `<div class="error">${esc(e.message)}</div>`; }
}

let genreTab = 'albums';
async function renderGenre(name) {
  view().innerHTML = `
    <h1 class="page-title">${esc(name)}</h1>
    <div class="subtabs">
      <button data-gt="albums" class="${genreTab === 'albums' ? 'active' : ''}">Albums</button>
      <button data-gt="songs" class="${genreTab === 'songs' ? 'active' : ''}">Songs</button>
    </div>
    <div id="genre-body"></div>`;
  view().querySelectorAll('[data-gt]').forEach((b) =>
    b.addEventListener('click', () => { genreTab = b.dataset.gt; renderGenre(name); }));
  const body = $('#genre-body');
  body.innerHTML = '<div class="loading">Loading…</div>';
  try {
    if (genreTab === 'albums') {
      const albums = await api.albumsByGenre(name);
      body.innerHTML = albums.length ? `<div class="grid">${albumCardsHTML(albums)}</div>`
        : '<div class="empty muted">No albums.</div>';
    } else {
      const songs = await api.songsByGenre(name);
      body.innerHTML = songs.length ? `<div class="tracklist">${songRowsHTML(songs)}</div>`
        : '<div class="empty muted">No songs.</div>';
      wireSongRows(body, songs);
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
        <button id="mm-play" class="primary">${SVG_PLAY}Play</button>
        <button id="mm-shuffle" class="ghost">${SVG_SHUFFLE}Shuffle</button>
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
          <button id="play-all" class="primary">${SVG_PLAY}Play</button>
          <button id="shuffle-all" class="ghost">${SVG_SHUFFLE}Shuffle</button>
          <button id="album-love" class="ghost love ${a.starred ? 'on' : ''}">${a.starred ? '♥' : '♡'} Love</button>
        </div>
      </div>
    </div>
    <div class="tracklist">${songRowsHTML(songs)}</div>`;
  wireSongRows(view(), songs);
  $('#play-all').addEventListener('click', () => player.play(songs, 0));
  $('#shuffle-all').addEventListener('click', () => player.play(shuffle(songs), 0));
  wireLove($('#album-love'), id, !!a.starred);
}

// A Love toggle button: optimistic star/unstar of an item by id.
function wireLove(btn, id, starred) {
  if (!btn) return;
  let on = starred;
  btn.addEventListener('click', async () => {
    on = !on;
    btn.classList.toggle('on', on);
    btn.textContent = (on ? '♥' : '♡') + ' Love';
    try { await (on ? api.star(id) : api.unstar(id)); }
    catch { on = !on; btn.classList.toggle('on', on); btn.textContent = (on ? '♥' : '♡') + ' Love'; }
  });
}

async function renderArtist(id) {
  loading();
  const a = await api.artist(id);
  if (!a) return fail(new Error('Artist not found'));
  const albums = a.album || [];
  const [info, popular] = await Promise.all([
    api.artistInfo(id),
    api.topSongs(a.name, 10),
  ]);
  const bio = info && info.biography ? info.biography.replace(/<[^>]*>/g, '').trim() : '';
  const similar = (info && info.similarArtist) || [];

  const parts = [`
    <div class="detail-head">
      ${artHTML(a.coverArt, 300)}
      <div class="dh-meta">
        <div class="dh-kind muted">ARTIST</div>
        <h1>${esc(a.name)}</h1>
        <div class="muted">${albums.length} albums</div>
        <div class="dh-actions"><button id="play-pop" class="primary">${SVG_PLAY}Play</button></div>
      </div>
    </div>`];
  if (bio) parts.push(`<p class="bio muted">${esc(bio)}</p>`);
  if (popular.length) parts.push(`<section class="shelf"><h2>Popular</h2><div class="tracklist" id="pop-list">${songRowsHTML(popular)}</div></section>`);
  if (albums.length) parts.push(shelfHTML('Albums', albumCardsHTML(albums)));
  if (similar.length) {
    parts.push(shelfHTML('Similar Artists', similar.map((s) => `
      <a class="card album" href="#/artist/${encodeURIComponent(s.id)}">
        ${artHTML(s.coverArt, 160)}
        <div class="cname">${esc(s.name)}</div>
      </a>`).join('')));
  }
  view().innerHTML = parts.join('');
  if (popular.length) {
    wireSongRows($('#pop-list'), popular);
    $('#play-pop').addEventListener('click', () => player.play(popular, 0));
  }
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
          <button id="play-all" class="primary">${SVG_PLAY}Play</button>
          <button id="shuffle-all" class="ghost">${SVG_SHUFFLE}Shuffle</button>
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
  clearHero(); // stop the Home hero rotation when leaving Home
  const hash = location.hash.replace(/^#\/?/, '') || 'home';
  const [name, arg] = hash.split('/');
  for (const b of document.querySelectorAll('[data-route]')) {
    b.classList.toggle('active', b.dataset.route === name);
  }
  const run = {
    home: renderHome, library: renderLibrary, search: renderSearch,
    mymusic: renderMyMusic, playlists: renderPlaylists, admin: renderAdmin,
    onrepeat: renderOnRepeat, songsforyou: renderSongsForYou,
    album: () => renderAlbum(decodeURIComponent(arg)),
    artist: () => renderArtist(decodeURIComponent(arg)),
    playlist: () => renderPlaylist(decodeURIComponent(arg)),
    genre: () => renderGenre(decodeURIComponent(arg)),
  }[name] || renderHome;
  Promise.resolve(run()).catch(fail);
}

// --- auth / boot --------------------------------------------------------

async function enterApp() {
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  $('#who').textContent = api.currentUsername();
  player.init();

  // Delegated hover-play on album cards: fetch the album and play it in place,
  // without navigating. Survives per-route innerHTML swaps (listener on #view).
  view().addEventListener('click', async (e) => {
    const pb = e.target.closest('[data-play-album]');
    if (!pb) return;
    e.preventDefault();
    e.stopPropagation();
    const a = await api.album(pb.dataset.playAlbum);
    if (a && a.song && a.song.length) player.play(a.song, 0);
  });

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
