/* =========================================================
   Ядро фронтенда: сессия, запросы к API, форматирование,
   общие HTML-компоненты и работа с картинками.
   ========================================================= */

/* ---------- Сессия (JWT в localStorage) ---------- */

function store(key, value) {
  try {
    if (value === undefined) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch { /* приватный режим — сессия проживёт до перезагрузки */ }
}
function load(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

// Роль берём из самого токена: именно её проверяет сервер.
function tokenPayload(token) {
  try {
    const b64 = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(decodeURIComponent(escape(atob(b64))));
  } catch { return {}; }
}

export const session = {
  get token() { return load("token"); },
  get user() {
    try { return JSON.parse(load("user")); } catch { return null; }
  },
  get isAdmin() {
    const t = this.token;
    return !!t && tokenPayload(t).role === "admin";
  },
  save(token, user) {
    store("token", token);
    store("user", JSON.stringify(user));
  },
  updateUser(user) { store("user", JSON.stringify(user)); },
  clear() { store("token"); store("user"); },
  // Может ли текущий пользователь менять объект с владельцем ownerId.
  canEdit(ownerId) {
    const me = this.user;
    return !!this.token && !!me && (me.id === ownerId || this.isAdmin);
  },
};

/* ---------- Запросы к API ---------- */

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

// Переводим ответы сервера на понятный пользователю язык.
const errorText = {
  "invalid email or password": "Неверный email или пароль",
  "user with this email or username already exists": "Такой email или имя уже заняты",
  "invalid email": "Проверьте email — в нём ошибка",
  "password must be 6-72 characters long": "Пароль должен быть от 6 до 72 символов",
  "username must be 3-32 characters: latin letters, digits or _": "Имя: от 3 до 32 символов, латиница, цифры или _",
  "unsupported file format, allowed: mp4, webm, mov, mkv": "Этот формат не подходит. Загрузите mp4, webm, mov или mkv",
  "file is too large": "Файл слишком большой",
  "video not found": "Видео не найдено — возможно, его удалили",
  "user not found": "Пользователь не найден",
  "comment not found": "Комментарий не найден — возможно, его удалили",
  "you can change only your own videos": "Менять можно только свои видео",
  "you can change only your own comments": "Менять можно только свои комментарии",
  "comment must be 1-1000 characters long": "Комментарий — от 1 до 1000 символов",
  "title is required and must be at most 200 characters": "Добавьте название — до 200 символов",
  "description must be at most 5000 characters": "Описание — до 5000 символов",
  "avatar must be at most 2 MB": "Аватарка должна быть не больше 2 МБ",
  "avatar must be a JPEG, PNG or WebP image": "Аватарка должна быть картинкой JPEG, PNG или WebP",
  "thumbnail must be a JPEG, PNG or WebP image": "Превью должно быть картинкой JPEG, PNG или WebP",
  "thumbnail must be at most 5 MB": "Превью должно быть не больше 5 МБ",
  "you cannot subscribe to yourself": "На свой канал подписаться нельзя",
  "admin rights required": "Нужны права администратора. Если их только что выдали — перезайдите",
  "you cannot delete yourself or remove your own admin role": "Нельзя удалить себя или снять с себя права администратора",
  "too many requests, try again later": "Слишком много действий подряд. Подождите минуту",
  "invalid or expired token": "Сессия истекла. Войдите снова",
};
export const translate = (msg) => errorText[msg] || msg || "Что-то пошло не так. Попробуйте ещё раз";

const listeners = new Set();
export const onSessionExpired = (fn) => listeners.add(fn);

export async function api(path, { method = "GET", body, form } = {}) {
  const headers = {};
  if (session.token) headers.Authorization = `Bearer ${session.token}`;
  let payload;
  if (form) {
    payload = form; // FormData: Content-Type с boundary браузер выставит сам
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(path, { method, headers, body: payload });
  } catch {
    throw new ApiError(0, "Нет связи с сервером. Проверьте подключение");
  }

  if (res.status === 401 && session.token) {
    session.clear();
    listeners.forEach((fn) => fn());
  }
  if (res.status === 204) return null;

  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, translate(data && data.error));
  return data;
}

// Загрузка с индикатором прогресса: у fetch нет события прогресса отправки, поэтому XHR.
export function upload(path, form, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => {
      let data = null;
      try { data = JSON.parse(xhr.responseText); } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else reject(new ApiError(xhr.status, translate(data && data.error)));
    };
    xhr.onerror = () => reject(new ApiError(0, "Нет связи с сервером. Проверьте подключение"));
    xhr.open("POST", path);
    if (session.token) xhr.setRequestHeader("Authorization", `Bearer ${session.token}`);
    xhr.send(form);
  });
}

/* ---------- Форматирование ---------- */

export const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
}[c]));

export function plural(n, [one, few, many]) {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

export function compact(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(".0", "") + " млн";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(".0", "") + " тыс.";
  return String(n);
}

export const views = (n) => `${compact(n)} ${plural(n, ["просмотр", "просмотра", "просмотров"])}`;
export const subscribers = (n) => `${compact(n)} ${plural(n, ["подписчик", "подписчика", "подписчиков"])}`;

export function timeAgo(iso) {
  const sec = Math.max(1, (Date.now() - new Date(iso).getTime()) / 1000);
  const steps = [
    [60, "секунду", "секунды", "секунд", 1],
    [3600, "минуту", "минуты", "минут", 60],
    [86400, "час", "часа", "часов", 3600],
    [2592000, "день", "дня", "дней", 86400],
    [31536000, "месяц", "месяца", "месяцев", 2592000],
    [Infinity, "год", "года", "лет", 31536000],
  ];
  for (const [limit, one, few, many, unit] of steps) {
    if (sec < limit) {
      const n = Math.floor(sec / unit);
      return `${n} ${plural(n, [one, few, many])} назад`;
    }
  }
}

export function clock(seconds) {
  if (!isFinite(seconds) || seconds < 0) seconds = 0;
  const s = Math.floor(seconds % 60), m = Math.floor(seconds / 60) % 60, h = Math.floor(seconds / 3600);
  const ss = String(s).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

export function fileSize(bytes) {
  if (bytes >= 1 << 30) return (bytes / (1 << 30)).toFixed(1) + " ГБ";
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " МБ";
  return Math.ceil(bytes / 1024) + " КБ";
}

/* ---------- HTML-компоненты ---------- */

// Цвет постера для видео без превью — постоянный для каждого ID.
const POSTERS = ["#16213A", "#C2185B", "#0F766E", "#9A6212", "#5B21B6", "#334155", "#1D4ED8", "#7C2D12"];
const posterColor = (id) => POSTERS[Math.abs(Number(id) * 7 + 3) % POSTERS.length];

// Обложка видео: превью, если оно есть, иначе «кадр плёнки» с названием.
export function thumb(v) {
  const duration = v.duration_seconds > 0 ? `<span class="duration">${clock(v.duration_seconds)}</span>` : "";
  if (v.thumbnail_url) {
    return `<div class="thumb">
      <img src="${esc(v.thumbnail_url)}" alt="" loading="lazy" decoding="async">${duration}</div>`;
  }
  return `<div class="thumb frame" style="--poster:${posterColor(v.id)}">
      <span class="frame-title">${esc(v.title)}</span>${duration}</div>`;
}

// Запасная картинка (силуэт), если аватарку не удалось загрузить.
const AVATAR_FALLBACK = "data:image/svg+xml," + encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="#5B6577"/>' +
  '<circle cx="32" cy="25" r="11" fill="#E9EDF2"/><path d="M12 56c3-11 11-16 20-16s17 5 20 16z" fill="#E9EDF2"/></svg>');
const onAvatarError = `onerror="this.onerror=null;this.src='${AVATAR_FALLBACK}'"`;

export function avatar(userId, size = 36, version = "") {
  const v = version ? `?v=${encodeURIComponent(version)}` : "";
  return `<img class="avatar" src="/users/${userId}/avatar${v}" width="${size}" height="${size}" alt="" ${onAvatarError}>`;
}

// Для своего аватара добавляем версию, чтобы после смены картинки не показывался старый кеш.
export function myAvatar(size = 32) {
  const me = session.user;
  if (!me) return "";
  const url = me.avatar_url || `/users/${me.id}/avatar`;
  return `<img class="avatar" src="${esc(url)}" width="${size}" height="${size}" alt="" ${onAvatarError}>`;
}

export function videoCard(v) {
  return `
    <article class="card">
      <a href="#/video/${v.id}" class="card-thumb" aria-label="${esc(v.title)}">${thumb(v)}</a>
      <div class="card-body">
        <a href="#/channel/${v.author_id}" class="card-avatar" aria-label="Канал ${esc(v.author_name)}">${avatar(v.author_id)}</a>
        <div class="card-meta">
          <a href="#/video/${v.id}" class="card-title">${esc(v.title)}</a>
          <a href="#/channel/${v.author_id}" class="card-sub">${esc(v.author_name)}</a>
          <span class="card-sub">${views(v.views)} · ${timeAgo(v.created_at)}</span>
        </div>
      </div>
    </article>`;
}

export function skeletonGrid(n = 8) {
  return `<div class="grid">${'<div class="skeleton"><div class="thumb"></div><i></i><i style="width:40%"></i></div>'.repeat(n)}</div>`;
}

export function emptyState(text, action = "") {
  return `<div class="empty"><p>${text}</p>${action}</div>`;
}

const toastEl = document.getElementById("toast");
let toastTimer;
export function toast(text) {
  toastEl.textContent = text;
  toastEl.classList.add("is-visible");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastEl.classList.remove("is-visible"), 2800);
}

export function requireLogin() {
  if (session.token) return true;
  location.hash = `#/login?next=${encodeURIComponent(location.hash || "#/")}`;
  return false;
}

// Модальное окно на основе нативного <dialog>: фокус и Esc браузер обрабатывает сам.
export function openDialog(html, onReady) {
  const dlg = document.createElement("dialog");
  dlg.className = "dialog";
  dlg.innerHTML = html;
  document.body.append(dlg);
  dlg.addEventListener("close", () => dlg.remove());
  dlg.addEventListener("click", (e) => { if (e.target === dlg) dlg.close(); });
  dlg.querySelectorAll("[data-close]").forEach((b) => b.addEventListener("click", () => dlg.close()));
  dlg.showModal();
  onReady?.(dlg);
  return dlg;
}

/* ---------- Картинки: кадры из видео и уменьшение изображений ---------- */

function waitFor(el, event, timeout = 10000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { cleanup(); reject(new Error("timeout")); }, timeout);
    const ok = () => { cleanup(); resolve(); };
    const fail = () => { cleanup(); reject(new Error("media error")); };
    function cleanup() {
      clearTimeout(timer);
      el.removeEventListener(event, ok);
      el.removeEventListener("error", fail);
    }
    el.addEventListener(event, ok, { once: true });
    el.addEventListener("error", fail, { once: true });
  });
}

// Рисует источник (видео или картинку) в canvas нужного размера с обрезкой «по центру» и отдаёт JPEG.
export function drawToBlob(source, srcW, srcH, outW = 1280, outH = 720, quality = 0.85) {
  const canvas = document.createElement("canvas");
  canvas.width = outW;
  canvas.height = outH;
  const ctx = canvas.getContext("2d");
  const scale = Math.max(outW / srcW, outH / srcH);
  const w = srcW * scale, h = srcH * scale;
  ctx.fillStyle = "#000";
  ctx.fillRect(0, 0, outW, outH);
  ctx.drawImage(source, (outW - w) / 2, (outH - h) / 2, w, h);
  return new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
}

// Достаёт из видеофайла длительность и несколько кадров-кандидатов для превью.
// Всё происходит в браузере — серверу не нужен ffmpeg.
export async function extractFrames(file, positions = [0.15, 0.5, 0.85]) {
  const url = URL.createObjectURL(file);
  const video = document.createElement("video");
  video.muted = true;
  video.playsInline = true;
  video.preload = "auto";
  video.src = url;
  try {
    await waitFor(video, "loadeddata");
    const duration = isFinite(video.duration) ? video.duration : 0;
    const frames = [];
    for (const p of positions) {
      video.currentTime = duration ? Math.min(duration * p, Math.max(duration - 0.1, 0)) : 0;
      await waitFor(video, "seeked");
      const blob = await drawToBlob(video, video.videoWidth, video.videoHeight);
      if (blob) frames.push(blob);
    }
    return { duration, frames };
  } catch {
    // Браузер не умеет декодировать этот формат (часто mkv/mov) — превью можно выбрать картинкой.
    return { duration: 0, frames: [] };
  } finally {
    URL.revokeObjectURL(url);
  }
}

// Уменьшает картинку до нужного размера (аватарка 256×256, превью 1280×720).
export async function resizeImage(file, outW, outH) {
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    return await drawToBlob(img, img.naturalWidth, img.naturalHeight, outW, outH, 0.88);
  } finally {
    URL.revokeObjectURL(url);
  }
}

export function pickFile(accept) {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = accept;
    input.onchange = () => resolve(input.files[0] || null);
    input.click();
  });
}
