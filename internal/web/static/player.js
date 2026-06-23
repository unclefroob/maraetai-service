// Audio engine: queue, playback controls, the now-playing bar, and scrobbling.
// Scrobbles go through the proxy's tee, so web plays land in the play store too.
import * as api from './api.js';

const $ = (sel) => document.querySelector(sel);

let queue = [];
let index = -1;
let startedAtMs = 0;   // play-start time for the scrobble `time` stamp
let scrobbled = false; // crossed the listen threshold this track?

let audio, art, titleEl, artistEl, starBtn, playBtn, progress, curEl, durEl, volEl, bar;

function fmt(sec) {
  if (!isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

export function current() {
  return index >= 0 && index < queue.length ? queue[index] : null;
}

export function play(tracks, startAt = 0) {
  if (!tracks || tracks.length === 0) return;
  queue = tracks.slice();
  index = Math.max(0, Math.min(startAt, queue.length - 1));
  loadCurrent();
}

function loadCurrent() {
  const track = current();
  if (!track) return;
  audio.src = api.streamURL(track.id);
  audio.play().catch(() => {});
  startedAtMs = Date.now();
  scrobbled = false;
  renderBar(track);
  bar.classList.remove('hidden');
  api.scrobble(track.id, { submission: false }); // now-playing ping
}

export function togglePlay() {
  if (!current()) return;
  if (audio.paused) audio.play().catch(() => {});
  else audio.pause();
}

export function next() {
  if (index < queue.length - 1) {
    index += 1;
    loadCurrent();
  }
}

export function prev() {
  // Restart the track if we're more than 3s in; otherwise go back one.
  if (audio.currentTime > 3 || index === 0) {
    audio.currentTime = 0;
    return;
  }
  index -= 1;
  loadCurrent();
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

function onTimeUpdate() {
  const d = audio.duration;
  if (isFinite(d) && d > 0) {
    progress.value = String(Math.round((audio.currentTime / d) * 1000));
    curEl.textContent = fmt(audio.currentTime);
    durEl.textContent = fmt(d);
    // Last.fm convention: scrobble once past ~half the track or 4 minutes.
    const threshold = Math.min(d * 0.5, 240);
    if (!scrobbled && audio.currentTime >= threshold) {
      scrobbled = true;
      const track = current();
      if (track) api.scrobble(track.id, { submission: true, timeMs: startedAtMs });
    }
  }
}

export function init() {
  audio = $('#audio');
  bar = $('#player');
  art = $('#np-art');
  titleEl = $('#np-title');
  artistEl = $('#np-artist');
  starBtn = $('#np-star');
  playBtn = $('#np-play');
  progress = $('#np-progress');
  curEl = $('#np-cur');
  durEl = $('#np-dur');
  volEl = $('#np-vol');

  playBtn.addEventListener('click', togglePlay);
  $('#np-prev').addEventListener('click', prev);
  $('#np-next').addEventListener('click', next);
  starBtn.addEventListener('click', toggleStar);

  audio.addEventListener('play', () => { playBtn.textContent = '⏸'; });
  audio.addEventListener('pause', () => { playBtn.textContent = '▶'; });
  audio.addEventListener('ended', next);
  audio.addEventListener('timeupdate', onTimeUpdate);

  progress.addEventListener('input', () => {
    const d = audio.duration;
    if (isFinite(d) && d > 0) audio.currentTime = (Number(progress.value) / 1000) * d;
  });
  volEl.addEventListener('input', () => { audio.volume = Number(volEl.value) / 100; });
  audio.volume = 1;
}
