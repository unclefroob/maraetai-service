// Audio engine: queue, playback controls, shuffle/repeat, the now-playing bar,
// the queue panel, lyrics, and scrobbling. Scrobbles go through the proxy's tee,
// so web plays land in the play store too.
import * as api from './api.js';

const $ = (sel) => document.querySelector(sel);

// Monochrome inline SVGs (respect currentColor) so controls match the macOS
// app's SF Symbols instead of rendering as coloured emoji.
const S = 'fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"';
const ICONS = {
  shuffle: `<svg viewBox="0 0 24 24" ${S}><polyline points="16 3 21 3 21 8"/><line x1="4" y1="20" x2="21" y2="3"/><polyline points="21 16 21 21 16 21"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/></svg>`,
  prev: `<svg viewBox="0 0 24 24" ${S}><polygon points="19 20 9 12 19 4 19 20" fill="currentColor"/><line x1="5" y1="19" x2="5" y2="5"/></svg>`,
  next: `<svg viewBox="0 0 24 24" ${S}><polygon points="5 4 15 12 5 20 5 4" fill="currentColor"/><line x1="19" y1="5" x2="19" y2="19"/></svg>`,
  play: `<svg viewBox="0 0 24 24" fill="currentColor"><polygon points="6 4 20 12 6 20 6 4"/></svg>`,
  pause: `<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/></svg>`,
  repeat: `<svg viewBox="0 0 24 24" ${S}><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>`,
  repeatOne: `<svg viewBox="0 0 24 24" ${S}><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/><text x="12" y="15.5" font-size="9" font-weight="700" text-anchor="middle" fill="currentColor" stroke="none">1</text></svg>`,
  lyrics: `<svg viewBox="0 0 24 24" ${S}><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>`,
  queue: `<svg viewBox="0 0 24 24" ${S}><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>`,
  volume: `<svg viewBox="0 0 24 24" ${S}><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" fill="currentColor"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>`,
};

let baseQueue = []; // canonical order as queued
let queue = [];     // active play order (== baseQueue unless shuffled)
let index = -1;
let shuffleOn = false;
let repeatMode = 'off'; // 'off' | 'all' | 'one'
let startedAtMs = 0;    // play-start time for the scrobble `time` stamp
let scrobbled = false;  // crossed the listen threshold this track?
let lyricsData = null;  // { synced, line: [{ start, value }] } for the current track

let audio, art, titleEl, artistEl, starBtn, playBtn, progress, curEl, durEl, volEl, bar;
let shuffleBtn, repeatBtn, queueBtn, lyricsBtn, queuePanel, qpList, lyricsModal, lyBody, lyTitle;

function fmt(sec) {
  if (!isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

function shuffleArr(arr) {
  const a = arr.slice();
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
}

export function current() {
  return index >= 0 && index < queue.length ? queue[index] : null;
}

export function play(tracks, startAt = 0) {
  if (!tracks || tracks.length === 0) return;
  baseQueue = tracks.slice();
  const start = Math.max(0, Math.min(startAt, baseQueue.length - 1));
  if (shuffleOn) {
    const cur = baseQueue[start];
    queue = [cur, ...shuffleArr(baseQueue.filter((_, i) => i !== start))];
    index = 0;
  } else {
    queue = baseQueue.slice();
    index = start;
  }
  loadCurrent();
}

function loadCurrent() {
  const track = current();
  if (!track) return;
  audio.src = api.streamURL(track.id);
  audio.play().catch(() => {});
  startedAtMs = Date.now();
  scrobbled = false;
  lyricsData = null;
  renderBar(track);
  bar.classList.remove('hidden');
  api.scrobble(track.id, { submission: false }); // now-playing ping
  renderQueue();
  if (!lyricsModal.classList.contains('hidden')) loadLyrics();
}

export function togglePlay() {
  if (!current()) return;
  if (audio.paused) audio.play().catch(() => {});
  else audio.pause();
}

export function next() {
  if (index < queue.length - 1) { index += 1; loadCurrent(); }
  else if (repeatMode === 'all') { index = 0; loadCurrent(); }
}

export function prev() {
  // Restart the track if we're more than 3s in; otherwise go back one.
  if (audio.currentTime > 3 || index === 0) { audio.currentTime = 0; return; }
  index -= 1;
  loadCurrent();
}

function onEnded() {
  if (repeatMode === 'one') { audio.currentTime = 0; audio.play().catch(() => {}); return; }
  next();
}

function toggleShuffle() {
  const cur = current();
  shuffleOn = !shuffleOn;
  shuffleBtn.classList.toggle('on', shuffleOn);
  if (!cur) return;
  if (shuffleOn) {
    queue = [cur, ...shuffleArr(baseQueue.filter((t) => t !== cur))];
    index = 0;
  } else {
    queue = baseQueue.slice();
    index = Math.max(0, queue.indexOf(cur));
  }
  renderQueue();
}

function cycleRepeat() {
  repeatMode = repeatMode === 'off' ? 'all' : repeatMode === 'all' ? 'one' : 'off';
  repeatBtn.innerHTML = repeatMode === 'one' ? ICONS.repeatOne : ICONS.repeat;
  repeatBtn.classList.toggle('on', repeatMode !== 'off');
}

function renderBar(track) {
  art.src = track.coverArt ? api.coverArtURL(track.coverArt, 96) : '';
  art.style.visibility = track.coverArt ? 'visible' : 'hidden';
  titleEl.textContent = track.title || 'Unknown';
  artistEl.textContent = [track.artist, track.album].filter(Boolean).join(' • ');
  setStarred(!!track.starred);
}

function setStarred(on) {
  starBtn.textContent = on ? '♥' : '♡';
  starBtn.classList.toggle('on', on);
}

async function toggleStar() {
  const track = current();
  if (!track) return;
  const on = starBtn.classList.contains('on');
  setStarred(!on); // optimistic
  try {
    if (on) await api.unstar(track.id);
    else await api.star(track.id);
    track.starred = on ? undefined : new Date().toISOString();
  } catch {
    setStarred(on); // revert
  }
}

// --- queue panel --------------------------------------------------------

function renderQueue() {
  if (queuePanel.classList.contains('hidden')) return;
  const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  qpList.innerHTML = queue.map((t, i) => `
    <div class="qrow ${i === index ? 'now' : ''}" data-i="${i}">
      <div class="qmeta">
        <div class="qtitle">${esc(t.title || 'Unknown')}</div>
        <div class="qsub muted">${esc(t.artist || '')}</div>
      </div>
      <button class="qrm" data-rm="${i}" title="Remove">✕</button>
    </div>`).join('') || '<div class="empty muted">Queue is empty.</div>';
  qpList.querySelectorAll('.qrow').forEach((row) => {
    row.addEventListener('click', (e) => {
      if (e.target.classList.contains('qrm')) return;
      index = Number(row.dataset.i); loadCurrent();
    });
  });
  qpList.querySelectorAll('.qrm').forEach((b) => {
    b.addEventListener('click', (e) => { e.stopPropagation(); removeAt(Number(b.dataset.rm)); });
  });
}

function removeAt(i) {
  queue.splice(i, 1);
  if (i < index) index -= 1;
  else if (i === index) {
    if (queue.length === 0) { audio.pause(); index = -1; renderQueue(); return; }
    index = Math.min(index, queue.length - 1);
    loadCurrent();
    return;
  }
  renderQueue();
}

function toggleQueue() {
  queuePanel.classList.toggle('hidden');
  queueBtn.classList.toggle('on', !queuePanel.classList.contains('hidden'));
  renderQueue();
}

// --- lyrics -------------------------------------------------------------

async function loadLyrics() {
  const track = current();
  if (!track) return;
  lyTitle.textContent = [track.title, track.artist].filter(Boolean).join(' — ');
  lyBody.innerHTML = '<div class="loading">Loading…</div>';
  lyricsData = await api.lyrics(track.id);
  const lines = (lyricsData && lyricsData.line) || [];
  if (!lines.length) { lyBody.innerHTML = '<div class="empty muted">No lyrics found.</div>'; return; }
  const esc = (s) => String(s ?? '').replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  lyBody.innerHTML = lines.map((l, i) =>
    `<p class="lyline" data-i="${i}">${esc(l.value) || '&nbsp;'}</p>`).join('');
}

function toggleLyrics() {
  lyricsModal.classList.toggle('hidden');
  lyricsBtn.classList.toggle('on', !lyricsModal.classList.contains('hidden'));
  if (!lyricsModal.classList.contains('hidden')) loadLyrics();
}

function syncLyrics() {
  if (!lyricsData || !lyricsData.synced || lyricsModal.classList.contains('hidden')) return;
  const lines = lyricsData.line || [];
  const ms = audio.currentTime * 1000;
  let active = -1;
  for (let i = 0; i < lines.length; i++) {
    if ((lines[i].start ?? 0) <= ms) active = i; else break;
  }
  lyBody.querySelectorAll('.lyline').forEach((el, i) => {
    const on = i === active;
    if (on && !el.classList.contains('active')) el.scrollIntoView({ block: 'center', behavior: 'smooth' });
    el.classList.toggle('active', on);
  });
}

// --- time / scrobble ----------------------------------------------------

function onTimeUpdate() {
  const d = audio.duration;
  if (isFinite(d) && d > 0) {
    progress.value = String(Math.round((audio.currentTime / d) * 1000));
    curEl.textContent = fmt(audio.currentTime);
    durEl.textContent = fmt(d);
    const threshold = Math.min(d * 0.5, 240); // Last.fm convention
    if (!scrobbled && audio.currentTime >= threshold) {
      scrobbled = true;
      const track = current();
      if (track) api.scrobble(track.id, { submission: true, timeMs: startedAtMs });
    }
  }
  syncLyrics();
}

export function init() {
  audio = $('#audio'); bar = $('#player');
  art = $('#np-art'); titleEl = $('#np-title'); artistEl = $('#np-artist'); starBtn = $('#np-star');
  playBtn = $('#np-play'); progress = $('#np-progress'); curEl = $('#np-cur'); durEl = $('#np-dur'); volEl = $('#np-vol');
  shuffleBtn = $('#np-shuffle'); repeatBtn = $('#np-repeat'); queueBtn = $('#np-queue'); lyricsBtn = $('#np-lyrics');
  queuePanel = $('#queue-panel'); qpList = $('#qp-list');
  lyricsModal = $('#lyrics-modal'); lyBody = $('#ly-body'); lyTitle = $('#ly-title');

  // Paint the monochrome SVG icons (replaces the emoji fallbacks in the markup).
  shuffleBtn.innerHTML = ICONS.shuffle;
  $('#np-prev').innerHTML = ICONS.prev;
  playBtn.innerHTML = ICONS.play;
  $('#np-next').innerHTML = ICONS.next;
  repeatBtn.innerHTML = ICONS.repeat;
  lyricsBtn.innerHTML = ICONS.lyrics;
  queueBtn.innerHTML = ICONS.queue;
  $('#np-vol-ic').innerHTML = ICONS.volume;

  playBtn.addEventListener('click', togglePlay);
  $('#np-prev').addEventListener('click', prev);
  $('#np-next').addEventListener('click', next);
  starBtn.addEventListener('click', toggleStar);
  shuffleBtn.addEventListener('click', toggleShuffle);
  repeatBtn.addEventListener('click', cycleRepeat);
  queueBtn.addEventListener('click', toggleQueue);
  lyricsBtn.addEventListener('click', toggleLyrics);
  $('#qp-clear').addEventListener('click', () => { baseQueue = []; queue = []; index = -1; audio.pause(); renderQueue(); });
  $('#ly-close').addEventListener('click', toggleLyrics);

  audio.addEventListener('play', () => { playBtn.innerHTML = ICONS.pause; });
  audio.addEventListener('pause', () => { playBtn.innerHTML = ICONS.play; });
  audio.addEventListener('ended', onEnded);
  audio.addEventListener('timeupdate', onTimeUpdate);

  progress.addEventListener('input', () => {
    const d = audio.duration;
    if (isFinite(d) && d > 0) audio.currentTime = (Number(progress.value) / 1000) * d;
  });
  volEl.addEventListener('input', () => { audio.volume = Number(volEl.value) / 100; });
  audio.volume = 1;
}
