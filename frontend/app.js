"use strict";

/* =========================================================
   Кадр — фронтенд видеохостинга.
   Одностраничное приложение без фреймворков: hash-роутинг,
   все запросы идут в api-gateway на том же адресе.
   ========================================================= */

const app = document.getElementById("app");
const userbar = document.getElementById("userbar");
const toastEl = document.getElementById("toast");

/* ---------- Сессия (JWT в localStorage) ---------- */

const session = {
  get token() {
    try { return localStorage.getItem("token"); } catch { return null; }
  },
  get user() {
    try { return JSON.parse(localStorage.getItem("user")); } catch { return null; }
  },
  save(token, user) {
    try {
      localStorage.setItem("token", token);
      localStorage.setItem("user", JSON.stringify(user));
    } catch { /* приватный режим — сессия проживёт до перезагрузки */ }
  },
  updateUser(user) {
    try { localStorage.setItem("user", JSON.stringify(user)); } catch {}
  },
  clear() {
    try { localStorage.removeItem("token"); localStorage.removeItem("user"); } catch {}
  },
};

/* ---------- Работа с API ---------- */

class ApiError extends Error {
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
  "file is too large": "Файл больше 500 МБ",
  "video not found": "Видео не найдено — возможно, его удалили",
  "you can delete only your own videos": "Удалять можно только свои видео",
  "you can delete only your own comments": "Удалять можно только свои комментарии",
  "too many requests, try again later": "Слишком много запросов. Подождите минуту",
  "invalid or expired token": "Сессия истекла. Войдите снова",
};
const translate = (msg) => errorText[msg] || msg || "Что-то пошло не так. Попробуйте ещё раз";

async function api(path, { method = "GET", body } = {}) {
  const headers = {};
  if (session.token) headers.Authorization = `Bearer ${session.token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  let res;
  try {
    res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new ApiError(0, "Нет связи с сервером. Проверьте подключение");
  }

  if (res.status === 401 && session.token) {
    session.clear();
    renderUserbar();
  }
  if (res.status === 204) return null;

  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, translate(data && data.error));
  return data;
}

/* ---------- Утилиты ---------- */

const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
}[c]));

function plural(n, [one, few, many]) {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

function compact(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(".0", "") + " млн";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(".0", "") + " тыс.";
  return String(n);
}

const views = (n) => `${compact(n)} ${plural(n, ["просмотр", "просмотра", "просмотров"])}`;

function timeAgo(iso) {
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

function fileSize(bytes) {
  if (bytes >= 1 << 30) return (bytes / (1 << 30)).toFixed(1) + " ГБ";
  if (bytes >= 1 << 20) return (bytes / (1 << 20)).toFixed(1) + " МБ";
  return Math.ceil(bytes / 1024) + " КБ";
}

// Цвет постера и аватарки выбирается по ID, чтобы у каждого видео и автора он был свой и постоянный.
const POSTERS = ["#16213A", "#C2185B", "#0F766E", "#9A6212", "#5B21B6", "#334155", "#1D4ED8", "#7C2D12"];
const posterColor = (id) => POSTERS[Math.abs(Number(id) * 7 + 3) % POSTERS.length];

function frame(video) {
  return `
    <div class="frame" style="--poster:${posterColor(video.id)}">
      <span class="frame-badge">${esc(fileSize(video.size_bytes))}</span>
      <span class="frame-title">${esc(video.title)}</span>
    </div>`;
}

function avatar(id, name, big = false) {
  const letter = (name || "?").trim().charAt(0).toUpperCase();
  return `<span class="avatar${big ? " big" : ""}" style="--poster:${posterColor(id + 3)}" aria-hidden="true">${esc(letter)}</span>`;
}

function videoCard(v) {
  return `
    <a class="card" href="#/video/${v.id}">
      ${frame(v)}
      <span class="card-meta">
        <span class="card-title">${esc(v.title)}</span>
        <span class="card-sub">${esc(v.author_name)}</span>
        <span class="card-sub">${views(v.views)}, ${timeAgo(v.created_at)}</span>
      </span>
    </a>`;
}

function skeletonGrid(n = 8) {
  return `<div class="grid">${'<div class="skeleton"><div class="frame"></div><i></i><i style="width:40%"></i></div>'.repeat(n)}</div>`;
}

function emptyState(text, action = "") {
  return `<div class="empty"><p>${text}</p>${action}</div>`;
}

let toastTimer;
function toast(text) {
  toastEl.textContent = text;
  toastEl.classList.add("is-visible");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastEl.classList.remove("is-visible"), 2600);
}

function requireLogin() {
  if (session.token) return true;
  location.hash = `#/login?next=${encodeURIComponent(location.hash || "#/")}`;
  return false;
}

/* ---------- Шапка ---------- */

function renderUserbar() {
  const user = session.user;
  if (!session.token || !user) {
    userbar.innerHTML = `<a class="btn btn-primary" href="#/login">Войти</a>`;
    return;
  }
  userbar.innerHTML = `
    <a class="btn btn-primary" href="#/upload">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>
      <span class="label-wide">Загрузить</span>
    </a>
    <a class="btn btn-ghost label-wide" href="#/subscriptions">Подписки</a>
    <a class="btn btn-ghost" href="#/channel/${user.id}" title="Мой канал">${avatar(user.id, user.username)}</a>
    <button class="btn btn-ghost" id="logout-btn" title="Выйти">Выйти</button>`;
  document.getElementById("logout-btn").onclick = () => {
    session.clear();
    renderUserbar();
    toast("Вы вышли из аккаунта");
    location.hash = "#/";
  };
}

/* ---------- Роутер ---------- */

let renderId = 0; // защищает от гонки, если пользователь быстро переключает страницы

function parseHash() {
  const raw = location.hash.replace(/^#/, "") || "/";
  const [path, query = ""] = raw.split("?");
  return { parts: path.split("/").filter(Boolean), params: new URLSearchParams(query) };
}

async function router() {
  const id = ++renderId;
  const isCurrent = () => id === renderId;
  const { parts, params } = parseHash();
  const [page, arg] = parts;

  renderUserbar(); // сессия могла измениться в другой вкладке
  document.getElementById("search-input").value = page === "search" ? params.get("q") || "" : "";
  window.scrollTo(0, 0);

  try {
    switch (page) {
      case undefined:       return await homePage("new", isCurrent);
      case "popular":       return await homePage("popular", isCurrent);
      case "search":        return await searchPage(params.get("q") || "", isCurrent);
      case "video":         return await videoPage(Number(arg), isCurrent);
      case "channel":       return await channelPage(Number(arg), isCurrent);
      case "subscriptions": return await subscriptionsPage(isCurrent);
      case "upload":        return uploadPage();
      case "login":         return authPage("login", params.get("next"));
      case "register":      return authPage("register", params.get("next"));
      default:
        app.innerHTML = emptyState("Такой страницы нет.", `<a class="btn" href="#/">На главную</a>`);
    }
  } catch (err) {
    if (!isCurrent()) return;
    const msg = err.status === 404 ? err.message : err.message || "Не удалось загрузить страницу";
    app.innerHTML = emptyState(esc(msg), `<a class="btn" href="#/">На главную</a>`);
  }
}

/* ---------- Главная: новое и популярное ---------- */

async function homePage(tab, isCurrent) {
  document.title = "Кадр — видеохостинг";
  app.innerHTML = `
    <div class="page-head">
      <h1>${tab === "new" ? "Новые видео" : "Популярное"}</h1>
      <nav class="tabs" aria-label="Разделы">
        <a href="#/" ${tab === "new" ? 'aria-current="page"' : ""}>Новые</a>
        <a href="#/popular" ${tab === "popular" ? 'aria-current="page"' : ""}>Популярное</a>
      </nav>
    </div>
    <div id="list">${skeletonGrid()}</div>
    <div class="more" id="more"></div>`;

  const list = document.getElementById("list");

  if (tab === "popular") {
    const data = await api("/videos/popular");
    if (!isCurrent()) return;
    list.innerHTML = data.items.length
      ? `<div class="grid">${data.items.map(videoCard).join("")}</div>`
      : emptyState("Пока никто ничего не смотрел. Загляните во вкладку «Новые».");
    return;
  }

  await pagedGrid(list, document.getElementById("more"), (page) => `/videos?page=${page}&limit=24`, isCurrent,
    emptyState("Здесь появятся видео, как только их кто-нибудь загрузит.",
      `<a class="btn btn-primary" href="#/upload">Загрузить первое видео</a>`));
}

// pagedGrid показывает сетку видео с кнопкой «Показать ещё».
async function pagedGrid(listEl, moreEl, urlFor, isCurrent, emptyHTML) {
  let page = 1;
  let loaded = 0;
  const grid = document.createElement("div");
  grid.className = "grid";

  async function load() {
    const data = await api(urlFor(page));
    if (!isCurrent()) return;
    if (page === 1) {
      listEl.innerHTML = "";
      if (!data.items.length) { listEl.innerHTML = emptyHTML; return; }
      listEl.append(grid);
    }
    grid.insertAdjacentHTML("beforeend", data.items.map(videoCard).join(""));
    loaded += data.items.length;
    moreEl.innerHTML = loaded < data.total ? `<button class="btn">Показать ещё</button>` : "";
    const btn = moreEl.querySelector("button");
    if (btn) btn.onclick = async () => {
      btn.disabled = true;
      page++;
      try { await load(); } catch (e) { toast(e.message); btn.disabled = false; }
    };
  }
  await load();
}

/* ---------- Поиск ---------- */

async function searchPage(q, isCurrent) {
  document.title = `${q} — поиск в Кадре`;
  app.innerHTML = `
    <div class="page-head"><h1>Результаты по запросу «${esc(q)}»</h1></div>
    <div id="list">${skeletonGrid(4)}</div>
    <div class="more" id="more"></div>`;
  await pagedGrid(document.getElementById("list"), document.getElementById("more"),
    (page) => `/videos?search=${encodeURIComponent(q)}&page=${page}&limit=24`, isCurrent,
    emptyState("Ничего не нашлось. Попробуйте другое слово или более короткий запрос."));
}

/* ---------- Страница видео ---------- */

async function videoPage(id, isCurrent) {
  const video = await api(`/videos/${id}`);
  if (!isCurrent()) return;
  document.title = `${video.title} — Кадр`;

  const me = session.user;
  const isOwner = me && me.id === video.author_id;

  app.innerHTML = `
    <div class="watch">
      <article>
        <video class="player" controls autoplay playsinline preload="metadata"
               src="/videos/${video.id}/stream" type="${esc(video.content_type)}"></video>
        <h1 class="watch-title">${esc(video.title)}</h1>

        <div class="watch-bar">
          <div class="author">
            ${avatar(video.author_id, video.author_name)}
            <div>
              <a href="#/channel/${video.author_id}">${esc(video.author_name)}</a>
              <div class="muted" id="subs-count">&nbsp;</div>
            </div>
            ${isOwner ? "" : `<button class="btn" id="sub-btn" hidden>Подписаться</button>`}
          </div>
          <div class="reactions">
            <button class="btn" id="like-btn" aria-label="Нравится">👍 <span>0</span></button>
            <button class="btn" id="dislike-btn" aria-label="Не нравится">👎 <span>0</span></button>
            ${isOwner ? `<button class="btn btn-danger" id="delete-btn">Удалить</button>` : ""}
          </div>
        </div>

        <div class="description">
          <span class="stats">${views(video.views)}, ${timeAgo(video.created_at)}</span>
          ${video.description ? `<p class="desc-text">${esc(video.description)}</p>` : '<p class="muted">Автор не добавил описание.</p>'}
        </div>

        <section class="comments" aria-labelledby="comments-title">
          <h2 id="comments-title">Комментарии</h2>
          <div id="comment-form-slot"></div>
          <div id="comment-list"></div>
          <div class="more" id="comment-more"></div>
        </section>
      </article>

      <aside class="side">
        <h2>Ещё от ${esc(video.author_name)}</h2>
        <div class="side-list" id="side-list"></div>
      </aside>
    </div>`;

  // Всё остальное грузим параллельно — страница уже видна.
  setupReactions(video, isCurrent);
  setupSubscription(video, isCurrent);
  setupComments(video, isCurrent);
  loadSideList(video, isCurrent);

  if (isOwner) {
    document.getElementById("delete-btn").onclick = async () => {
      if (!confirm("Удалить видео? Комментарии и лайки тоже удалятся.")) return;
      try {
        await api(`/videos/${video.id}`, { method: "DELETE" });
        toast("Видео удалено");
        location.hash = `#/channel/${video.author_id}`;
      } catch (e) { toast(e.message); }
    };
  }
}

async function setupReactions(video, isCurrent) {
  const likeBtn = document.getElementById("like-btn");
  const dislikeBtn = document.getElementById("dislike-btn");

  const paint = (s) => {
    likeBtn.querySelector("span").textContent = compact(s.likes);
    dislikeBtn.querySelector("span").textContent = compact(s.dislikes);
    likeBtn.classList.toggle("is-on", s.my_reaction === "like");
    dislikeBtn.classList.toggle("is-on", s.my_reaction === "dislike");
    likeBtn.setAttribute("aria-pressed", s.my_reaction === "like");
    dislikeBtn.setAttribute("aria-pressed", s.my_reaction === "dislike");
  };

  const react = (kind) => async () => {
    if (!requireLogin()) return;
    likeBtn.disabled = dislikeBtn.disabled = true;
    try { paint(await api(`/videos/${video.id}/${kind}`, { method: "POST" })); }
    catch (e) { toast(e.message); }
    finally { likeBtn.disabled = dislikeBtn.disabled = false; }
  };
  likeBtn.onclick = react("like");
  dislikeBtn.onclick = react("dislike");

  try {
    const stats = await api(`/videos/${video.id}/reactions`);
    if (isCurrent()) paint(stats);
  } catch { /* счётчики не критичны */ }
}

async function setupSubscription(video, isCurrent) {
  const countEl = document.getElementById("subs-count");
  const btn = document.getElementById("sub-btn");
  let count = 0;
  let subscribed = false;

  const paint = () => {
    countEl.textContent = `${compact(count)} ${plural(count, ["подписчик", "подписчика", "подписчиков"])}`;
    if (!btn) return;
    btn.hidden = false;
    btn.textContent = subscribed ? "Вы подписаны" : "Подписаться";
    btn.classList.toggle("is-on", subscribed);
  };

  try {
    const [subs, mine] = await Promise.all([
      api(`/channels/${video.author_id}/subscribers`),
      session.token ? api("/users/me/subscriptions") : Promise.resolve({ items: [] }),
    ]);
    if (!isCurrent()) return;
    count = subs.subscribers;
    subscribed = mine.items.some((s) => s.channel_id === video.author_id);
    paint();
  } catch { return; }

  if (btn) btn.onclick = async () => {
    if (!requireLogin()) return;
    btn.disabled = true;
    try {
      await api(`/channels/${video.author_id}/subscribe`, { method: subscribed ? "DELETE" : "POST" });
      subscribed = !subscribed;
      count += subscribed ? 1 : -1;
      paint();
      toast(subscribed ? "Вы подписались на канал" : "Вы отписались от канала");
    } catch (e) { toast(e.message); }
    finally { btn.disabled = false; }
  };
}

async function loadSideList(video, isCurrent) {
  const el = document.getElementById("side-list");
  try {
    const data = await api(`/videos?author_id=${video.author_id}&limit=8`);
    if (!isCurrent()) return;
    const others = data.items.filter((v) => v.id !== video.id);
    el.innerHTML = others.length
      ? others.map((v) => `
          <a class="side-item" href="#/video/${v.id}">
            ${frame(v)}
            <span class="card-meta">
              <span class="card-title">${esc(v.title)}</span>
              <span class="card-sub">${views(v.views)}</span>
            </span>
          </a>`).join("")
      : `<p class="muted">Других видео у автора пока нет.</p>`;
  } catch {
    el.innerHTML = "";
  }
}

function setupComments(video, isCurrent) {
  const slot = document.getElementById("comment-form-slot");
  const list = document.getElementById("comment-list");
  const moreEl = document.getElementById("comment-more");
  const me = session.user;
  let page = 1;

  if (me && session.token) {
    slot.innerHTML = `
      <form class="comment-form" id="comment-form">
        <textarea class="input" name="text" maxlength="1000" placeholder="Напишите комментарий" aria-label="Текст комментария" required></textarea>
        <div class="row"><button class="btn btn-primary" type="submit">Отправить</button></div>
      </form>`;
    const form = document.getElementById("comment-form");
    form.onsubmit = async (e) => {
      e.preventDefault();
      const text = form.text.value.trim();
      if (!text) return;
      const btn = form.querySelector("button");
      btn.disabled = true;
      try {
        const c = await api(`/videos/${video.id}/comments`, { method: "POST", body: { text } });
        list.querySelector(".empty")?.remove();
        list.insertAdjacentHTML("afterbegin", commentHTML(c));
        form.reset();
        toast("Комментарий отправлен");
      } catch (err) { toast(err.message); }
      finally { btn.disabled = false; }
    };
  } else {
    slot.innerHTML = `<p class="muted"><a href="#/login?next=${encodeURIComponent(location.hash)}">Войдите</a>, чтобы оставить комментарий.</p>`;
  }

  // Удаление своего комментария — делегирование события на весь список.
  list.onclick = async (e) => {
    const btn = e.target.closest("[data-delete-comment]");
    if (!btn) return;
    if (!confirm("Удалить комментарий?")) return;
    try {
      await api(`/comments/${btn.dataset.deleteComment}`, { method: "DELETE" });
      btn.closest(".comment").remove();
      toast("Комментарий удалён");
    } catch (err) { toast(err.message); }
  };

  async function load() {
    const data = await api(`/videos/${video.id}/comments?page=${page}&limit=20`);
    if (!isCurrent()) return;
    if (page === 1 && !data.items.length) {
      list.innerHTML = emptyState("Комментариев пока нет. Будьте первым.");
    } else {
      list.insertAdjacentHTML("beforeend", data.items.map(commentHTML).join(""));
    }
    moreEl.innerHTML = page * data.limit < data.total ? `<button class="btn">Показать ещё комментарии</button>` : "";
    const btn = moreEl.querySelector("button");
    if (btn) btn.onclick = () => { page++; btn.disabled = true; load().catch((e) => toast(e.message)); };
  }
  load().catch(() => { list.innerHTML = `<p class="error">Не удалось загрузить комментарии.</p>`; });
}

function commentHTML(c) {
  const me = session.user;
  const own = me && me.id === c.user_id;
  return `
    <div class="comment">
      ${avatar(c.user_id, c.username)}
      <div>
        <div class="comment-head">
          <a href="#/channel/${c.user_id}">${esc(c.username)}</a>
          <span class="muted">${timeAgo(c.created_at)}</span>
          ${own ? `<button class="btn btn-ghost btn-danger" data-delete-comment="${c.id}">Удалить</button>` : ""}
        </div>
        <p class="comment-text">${esc(c.text)}</p>
      </div>
    </div>`;
}

/* ---------- Канал пользователя ---------- */

async function channelPage(id, isCurrent) {
  const [user, subs] = await Promise.all([
    api(`/users/${id}`),
    api(`/channels/${id}/subscribers`),
  ]);
  if (!isCurrent()) return;
  document.title = `${user.username} — Кадр`;

  const me = session.user;
  const isMe = me && me.id === user.id;

  app.innerHTML = `
    <section class="channel-head">
      ${avatar(user.id, user.username, true)}
      <div class="info">
        <h1>${esc(user.username)}</h1>
        <span class="muted">${compact(subs.subscribers)} ${plural(subs.subscribers, ["подписчик", "подписчика", "подписчиков"])}, на Кадре с ${new Date(user.created_at).toLocaleDateString("ru-RU", { month: "long", year: "numeric" })}</span>
        <div class="bio" id="bio">${user.bio ? esc(user.bio) : `<span class="muted">${isMe ? "Расскажите о себе и своём канале." : "Автор пока ничего о себе не рассказал."}</span>`}</div>
      </div>
      ${isMe ? `<button class="btn" id="edit-bio">Изменить описание</button>` : ""}
    </section>
    <div class="page-head"><h2>${isMe ? "Мои видео" : "Видео"}</h2></div>
    <div id="list">${skeletonGrid(4)}</div>
    <div class="more" id="more"></div>`;

  if (isMe) setupBioEditor(user);

  await pagedGrid(document.getElementById("list"), document.getElementById("more"),
    (page) => `/videos?author_id=${id}&page=${page}&limit=24`, isCurrent,
    isMe
      ? emptyState("Вы ещё ничего не загрузили.", `<a class="btn btn-primary" href="#/upload">Загрузить видео</a>`)
      : emptyState("На канале пока нет видео."));
}

function setupBioEditor(user) {
  const btn = document.getElementById("edit-bio");
  const bio = document.getElementById("bio");
  btn.onclick = () => {
    btn.hidden = true;
    bio.innerHTML = `
      <form class="comment-form" id="bio-form">
        <textarea class="input" name="bio" maxlength="500" aria-label="Описание канала">${esc(user.bio)}</textarea>
        <div class="row">
          <button class="btn" type="button" id="bio-cancel">Отмена</button>
          <button class="btn btn-primary" type="submit">Сохранить</button>
        </div>
      </form>`;
    document.getElementById("bio-cancel").onclick = () => router();
    document.getElementById("bio-form").onsubmit = async (e) => {
      e.preventDefault();
      try {
        const updated = await api("/users/me", { method: "PATCH", body: { bio: e.target.bio.value } });
        session.updateUser(updated);
        toast("Описание сохранено");
        router();
      } catch (err) { toast(err.message); }
    };
  };
}

/* ---------- Подписки: лента новых видео с каналов ---------- */

async function subscriptionsPage(isCurrent) {
  if (!requireLogin()) return;
  document.title = "Подписки — Кадр";
  app.innerHTML = `<div class="page-head"><h1>Подписки</h1></div><div id="list">${skeletonGrid(4)}</div>`;

  const { items: channels } = await api("/users/me/subscriptions");
  if (!isCurrent()) return;
  const list = document.getElementById("list");

  if (!channels.length) {
    list.innerHTML = emptyState("Вы ещё ни на кого не подписаны. Откройте любое видео и нажмите «Подписаться» рядом с автором.",
      `<a class="btn" href="#/popular">Смотреть популярное</a>`);
    return;
  }

  // Берём по несколько последних видео с каждого канала и сортируем по дате.
  const feeds = await Promise.all(channels.slice(0, 20).map((c) =>
    api(`/videos?author_id=${c.channel_id}&limit=6`).catch(() => ({ items: [] }))));
  if (!isCurrent()) return;
  const videos = feeds.flatMap((f) => f.items)
    .sort((a, b) => new Date(b.created_at) - new Date(a.created_at));

  const chips = channels.map((c) => `
    <a class="btn" href="#/channel/${c.channel_id}">${avatar(c.channel_id, c.channel_name)} ${esc(c.channel_name)}</a>`).join("");

  list.innerHTML = `
    <div class="tabs" style="flex-wrap:wrap;gap:8px;margin-bottom:28px">${chips}</div>
    ${videos.length
      ? `<div class="grid">${videos.map(videoCard).join("")}</div>`
      : emptyState("На ваших каналах пока нет новых видео.")}`;
}

/* ---------- Загрузка видео ---------- */

function uploadPage() {
  if (!requireLogin()) return;
  document.title = "Загрузить видео — Кадр";

  app.innerHTML = `
    <form class="panel" id="upload-form" novalidate>
      <h1>Новое видео</h1>
      <label class="dropzone" id="dropzone">
        <input type="file" name="file" accept=".mp4,.webm,.mov,.mkv,video/*" hidden>
        <strong id="file-name">Выберите файл или перетащите его сюда</strong>
        <span class="muted" id="file-hint">mp4, webm, mov или mkv, до 500 МБ</span>
      </label>
      <div class="field">
        <label for="title">Название</label>
        <input class="input" id="title" name="title" maxlength="200" required>
      </div>
      <div class="field">
        <label for="description">Описание</label>
        <textarea class="input" id="description" name="description" maxlength="5000"></textarea>
        <small>Необязательно</small>
      </div>
      <div class="progress" hidden id="progress"><span></span></div>
      <p class="error" id="upload-error" hidden></p>
      <button class="btn btn-primary" type="submit">Опубликовать</button>
    </form>`;

  const form = document.getElementById("upload-form");
  const dropzone = document.getElementById("dropzone");
  const fileInput = form.file;
  const errorEl = document.getElementById("upload-error");
  let file = null;

  const pickFile = (f) => {
    if (!f) return;
    file = f;
    document.getElementById("file-name").textContent = f.name;
    document.getElementById("file-hint").textContent = fileSize(f.size);
    if (!form.title.value) form.title.value = f.name.replace(/\.[^.]+$/, "").replace(/[_-]+/g, " ");
  };
  fileInput.onchange = () => pickFile(fileInput.files[0]);
  dropzone.ondragover = (e) => { e.preventDefault(); dropzone.classList.add("is-over"); };
  dropzone.ondragleave = () => dropzone.classList.remove("is-over");
  dropzone.ondrop = (e) => {
    e.preventDefault();
    dropzone.classList.remove("is-over");
    pickFile(e.dataTransfer.files[0]);
  };

  const showError = (msg) => { errorEl.textContent = msg; errorEl.hidden = !msg; };

  form.onsubmit = (e) => {
    e.preventDefault();
    showError("");
    if (!file) return showError("Выберите файл с видео");
    if (!form.title.value.trim()) return showError("Добавьте название");
    if (file.size > 500 * 1024 * 1024) return showError("Файл больше 500 МБ");

    const data = new FormData();
    data.append("title", form.title.value);
    data.append("description", form.description.value);
    data.append("file", file);

    // XMLHttpRequest вместо fetch — ради индикатора прогресса загрузки.
    const xhr = new XMLHttpRequest();
    const progress = document.getElementById("progress");
    const bar = progress.querySelector("span");
    const submit = form.querySelector('button[type="submit"]');

    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable) bar.style.width = `${(ev.loaded / ev.total) * 100}%`;
    };
    xhr.onload = () => {
      let body = null;
      try { body = JSON.parse(xhr.responseText); } catch {}
      if (xhr.status === 201) {
        toast("Видео опубликовано");
        location.hash = `#/video/${body.id}`;
        return;
      }
      submit.disabled = false;
      submit.textContent = "Опубликовать";
      progress.hidden = true;
      showError(translate(body && body.error));
    };
    xhr.onerror = () => {
      submit.disabled = false;
      submit.textContent = "Опубликовать";
      showError("Нет связи с сервером. Проверьте подключение");
    };

    xhr.open("POST", "/videos");
    xhr.setRequestHeader("Authorization", `Bearer ${session.token}`);
    xhr.send(data);

    submit.disabled = true;
    submit.textContent = "Публикуем…";
    progress.hidden = false;
  };
}

/* ---------- Вход и регистрация ---------- */

function authPage(mode, next) {
  const isLogin = mode === "login";
  document.title = `${isLogin ? "Вход" : "Регистрация"} — Кадр`;
  const nextQuery = next ? `?next=${encodeURIComponent(next)}` : "";

  app.innerHTML = `
    <form class="panel" id="auth-form" novalidate>
      <h1>${isLogin ? "Вход" : "Регистрация"}</h1>
      ${isLogin ? "" : `
        <div class="field">
          <label for="username">Имя канала</label>
          <input class="input" id="username" name="username" autocomplete="username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_]+">
          <small>Латиница, цифры и _, от 3 до 32 символов</small>
        </div>`}
      <div class="field">
        <label for="email">Email</label>
        <input class="input" id="email" name="email" type="email" autocomplete="email" required>
      </div>
      <div class="field">
        <label for="password">Пароль</label>
        <input class="input" id="password" name="password" type="password" autocomplete="${isLogin ? "current-password" : "new-password"}" required minlength="6">
        ${isLogin ? "" : "<small>Не меньше 6 символов</small>"}
      </div>
      <p class="error" id="auth-error" hidden></p>
      <button class="btn btn-primary" type="submit">${isLogin ? "Войти" : "Создать аккаунт"}</button>
      <p class="switch">
        ${isLogin
          ? `Нет аккаунта? <a href="#/register${nextQuery}">Зарегистрируйтесь</a>`
          : `Уже есть аккаунт? <a href="#/login${nextQuery}">Войдите</a>`}
      </p>
    </form>`;

  const form = document.getElementById("auth-form");
  const errorEl = document.getElementById("auth-error");
  form.querySelector("input").focus();

  form.onsubmit = async (e) => {
    e.preventDefault();
    errorEl.hidden = true;
    const btn = form.querySelector('button[type="submit"]');
    btn.disabled = true;

    const email = form.email.value.trim();
    const password = form.password.value;
    try {
      if (!isLogin) {
        await api("/auth/register", { method: "POST", body: { username: form.username.value.trim(), email, password } });
      }
      const res = await api("/auth/login", { method: "POST", body: { email, password } });
      session.save(res.token, res.user);
      renderUserbar();
      toast(isLogin ? `Привет, ${res.user.username}!` : "Аккаунт создан");
      location.hash = next && next.startsWith("#/") && !next.startsWith("#/login") ? next : "#/";
    } catch (err) {
      errorEl.textContent = err.message;
      errorEl.hidden = false;
      btn.disabled = false;
    }
  };
}

/* ---------- Запуск ---------- */

document.getElementById("search-form").onsubmit = (e) => {
  e.preventDefault();
  const q = e.target.q.value.trim();
  location.hash = q ? `#/search?q=${encodeURIComponent(q)}` : "#/";
};

window.addEventListener("hashchange", router);
renderUserbar();
router();
