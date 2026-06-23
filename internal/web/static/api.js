// Subsonic / maraetai-service API client. Auth uses salt+token so the password
// never goes on the wire (same scheme as the mobile apps).
import { md5 } from './md5.js';

const CLIENT = 'maraetai-web';
const API_VERSION = '1.16.1';
const SESSION_KEY = 'maraetai.creds';

let creds = null; // { username, password }

export function loadCreds() {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    creds = raw ? JSON.parse(raw) : null;
  } catch {
    creds = null;
  }
  return creds;
}

export function saveCreds(c) {
  creds = c;
  sessionStorage.setItem(SESSION_KEY, JSON.stringify(c));
}

export function clearCreds() {
  creds = null;
  sessionStorage.removeItem(SESSION_KEY);
}

export function currentUsername() {
  return creds ? creds.username : '';
}

function randomSalt() {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}

// authParams returns a fresh Subsonic auth query (random salt + token).
export function authParams() {
  const salt = randomSalt();
  return new URLSearchParams({
    u: creds.username,
    t: md5(creds.password + salt),
    s: salt,
    c: CLIENT,
    v: API_VERSION,
    f: 'json',
  });
}

// get calls a Subsonic endpoint and returns its parsed body, throwing on failure.
export async function get(path, extra = {}) {
  const params = authParams();
  for (const [k, v] of Object.entries(extra)) {
    if (v === undefined || v === null) continue;
    params.set(k, String(v));
  }
  const res = await fetch(`${path}?${params.toString()}`);
  const body = (await res.json())['subsonic-response'];
  if (!body || body.status !== 'ok') {
    const msg = body && body.error ? body.error.message : `request failed (${res.status})`;
    throw new Error(msg);
  }
  return body;
}

// coverArtURL builds an authenticated cover-art URL for an <img>.
export function coverArtURL(id, size = 160) {
  if (!id) return '';
  const params = authParams();
  params.set('id', id);
  params.set('size', String(size));
  return `/rest/getCoverArt.view?${params.toString()}`;
}

// streamURL builds an authenticated audio stream URL for <audio>.
export function streamURL(id) {
  const params = authParams();
  params.set('id', id);
  return `/rest/stream.view?${params.toString()}`;
}

// --- endpoint helpers ---------------------------------------------------

export const ping = () => get('/rest/ping.view');

export async function getUser(username) {
  const b = await get('/rest/getUser.view', { username });
  return b.user || {};
}

export async function albumList(type, size = 30, offset = 0) {
  const b = await get('/rest/getAlbumList2.view', { type, size, offset });
  return (b.albumList2 && b.albumList2.album) || [];
}

export async function album(id) {
  const b = await get('/rest/getAlbum.view', { id });
  return b.album || null;
}

export async function artistsIndex() {
  const b = await get('/rest/getArtists.view');
  const index = (b.artists && b.artists.index) || [];
  return index.flatMap((g) => g.artist || []);
}

export async function artist(id) {
  const b = await get('/rest/getArtist.view', { id });
  return b.artist || null;
}

export async function playlists() {
  const b = await get('/rest/getPlaylists.view');
  return (b.playlists && b.playlists.playlist) || [];
}

export async function playlist(id) {
  const b = await get('/rest/getPlaylist.view', { id });
  return b.playlist || null;
}

export async function starred() {
  const b = await get('/rest/getStarred2.view');
  return b.starred2 || { song: [], album: [], artist: [] };
}

export async function search(query) {
  const b = await get('/rest/search3.view', { query, artistCount: 20, albumCount: 30, songCount: 50 });
  return b.searchResult3 || { artist: [], album: [], song: [] };
}

export const star = (id) => get('/rest/star.view', { id });
export const unstar = (id) => get('/rest/unstar.view', { id });

export async function genres() {
  const b = await get('/rest/getGenres.view');
  return (b.genres && b.genres.genre) || []; // [{ value, songCount, albumCount }]
}
export async function albumsByGenre(genre, size = 100) {
  const b = await get('/rest/getAlbumList2.view', { type: 'byGenre', genre, size });
  return (b.albumList2 && b.albumList2.album) || [];
}
export async function songsByGenre(genre, count = 100) {
  const b = await get('/rest/getSongsByGenre.view', { genre, count });
  return (b.songsByGenre && b.songsByGenre.song) || [];
}

// Artist bio + similar artists (getArtistInfo2). Best-effort → null.
export async function artistInfo(id) {
  try {
    const b = await get('/rest/getArtistInfo2.view', { id, count: 12 });
    return b.artistInfo2 || null;
  } catch {
    return null;
  }
}
export async function topSongs(artistName, count = 10) {
  try {
    const b = await get('/rest/getTopSongs.view', { artist: artistName, count });
    return (b.topSongs && b.topSongs.song) || [];
  } catch {
    return [];
  }
}

// maraetai-service extensions
export async function onRepeat(count = 30) {
  const b = await get('/rest/getOnRepeat.view', { count });
  return (b.onRepeat && b.onRepeat.song) || [];
}
export async function songsForYou(count = 20) {
  const b = await get('/rest/getSongsForYou.view', { count });
  return (b.songsForYou && b.songsForYou.song) || [];
}
export async function recentlyPlayed(count = 50) {
  const b = await get('/rest/getRecentlyPlayed.view', { count });
  return (b.recentlyPlayed && b.recentlyPlayed.song) || [];
}

// scrobble: submission=true records a completed play (tee'd into the store);
// submission=false is a "now playing" ping. timeMs stamps the play start.
export function scrobble(id, { submission = true, timeMs } = {}) {
  const extra = { id, submission: submission ? 'true' : 'false' };
  if (timeMs) extra.time = String(timeMs);
  return get('/rest/scrobble.view', extra).catch(() => {}); // best-effort
}

// Lyrics (OpenSubsonic getLyricsBySongId). Returns the first structured set
// ({ synced, line: [{ start, value }] }) or null. Best-effort.
export async function lyrics(id) {
  try {
    const b = await get('/rest/getLyricsBySongId.view', { id });
    const list = (b.lyricsList && b.lyricsList.structuredLyrics) || [];
    return list[0] || null;
  } catch {
    return null;
  }
}

export async function stats(days) {
  const res = await fetch(`/api/stats?${authParams().toString()}&days=${days}`);
  if (!res.ok) throw new Error(`stats failed (${res.status})`);
  return res.json();
}

export async function appConfig() {
  try {
    const res = await fetch('/api/config');
    return res.ok ? await res.json() : {};
  } catch {
    return {};
  }
}
