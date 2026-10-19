/* =========================================================
   Кадр — точка входа фронтенда: роутер (hash-навигация),
   шапка, главная, поиск, подписки, вход и регистрация.
   ========================================================= */

import {
  api, session, onSessionExpired, esc, toast, requireLogin, videoCard, skeletonGrid,
  emptyState, myAvatar, avatar,
} from "./core.js";
import { videoPage, leaveWatch } from "./watch.js";
import { channelPage } from "./channel.js";
import { uploadPage } from "./upload.js";
import { adminPage } from "./admin.js";

const app = document.getElementById("app");
const userbar = document.getElementById("userbar");

/* ---------- Шапка ---------- */

export function renderUserbar() {
  const user = session.user;
  if (!session.token || !user) {
    userbar.innerHTML = `<a class="btn btn-primary" href="#/login">Войти</a>`;
    return;
  }
  userbar.innerHTML = `
    <a class="btn btn-primary" href="#/upload" aria-label="Загрузить видео">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>
      <span class="label-wide">Загрузить</span>
    </a>
    <a class="btn btn-ghost label-wide" href="#/subscriptions">Подписки</a>
    ${session.isAdmin ? `<a class="btn btn-ghost label-wide" href="#/admin">Админка</a>` : ""}
    <details class="menu">
      <summary class="btn btn-ghost btn-icon" aria-label="Меню профиля">${myAvatar(32)}</summary>
      <div class="menu-list">
        <span class="menu-user">${esc(user.username)}</span>
        <a href="#/channel/${user.id}">Мой канал</a>
        <a href="#/channel/${user.id}/subscribers">Мои подписчики</a>
        <a href="#/subscriptions">Подписки</a>
        ${session.isAdmin ? `<a href="#/admin">Админ-панель</a>` : ""}
        <button type="button" id="logout-btn">Выйти</button>
      </div>
    </details>`;

  const menu = userbar.querySelector(".menu");
  menu.addEventListener("click", (e) => { if (e.target.closest("a, button")) menu.open = false; });
  document.getElementById("logout-btn").onclick = () => {
    session.clear();
    renderUserbar();
    toast("Вы вышли из аккаунта");
    location.hash = "#/";
  };
}

// Закрываем меню профиля кликом мимо него.
document.addEventListener("click", (e) => {
  const menu = userbar.querySelector(".menu[open]");
  if (menu && !menu.contains(e.target)) menu.open = false;
});

onSessionExpired(() => {
  renderUserbar();
  toast("Сессия истекла. Войдите снова");
});

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
  const [page, arg, sub] = parts;

  leaveWatch();
  renderUserbar();
  document.getElementById("search-input").value = page === "search" ? params.get("q") || "" : "";
  window.scrollTo(0, 0);

  try {
    switch (page) {
      case undefined:       return await homePage("new", isCurrent);
      case "popular":       return await homePage("popular", isCurrent);
      case "search":        return await searchPage(params.get("q") || "", isCurrent);
      case "video":         return await videoPage(app, Number(arg), isCurrent);
      case "channel":       return await channelPage(app, Number(arg), sub || "videos", isCurrent);
      case "subscriptions": return await subscriptionsPage(isCurrent);
      case "upload":        return uploadPage(app);
      case "admin":         return await adminPage(app, arg || "", isCurrent);
      case "login":         return authPage("login", params.get("next"));
      case "register":      return authPage("register", params.get("next"));
      default:
        app.innerHTML = emptyState("Такой страницы нет.", `<a class="btn" href="#/">На главную</a>`);
    }
  } catch (err) {
    if (!isCurrent()) return;
    app.innerHTML = emptyState(esc(err.message || "Не удалось загрузить страницу"), `<a class="btn" href="#/">На главную</a>`);
  }
}

/* ---------- Сетка видео с кнопкой «Показать ещё» ---------- */

export async function pagedGrid(listEl, moreEl, urlFor, isCurrent, emptyHTML) {
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

/* ---------- Подписки: каналы и лента новых видео ---------- */

async function subscriptionsPage(isCurrent) {
  if (!requireLogin()) return;
  document.title = "Подписки — Кадр";
  app.innerHTML = `<div class="page-head"><h1>Подписки</h1></div><div id="list">${skeletonGrid(4)}</div>`;

  const { items: channels } = await api("/users/me/subscriptions");
  if (!isCurrent()) return;
  const list = document.getElementById("list");

  if (!channels.length) {
    list.innerHTML = emptyState("Вы ещё ни на кого не подписаны. Откройте канал автора и нажмите «Подписаться».",
      `<a class="btn" href="#/popular">Смотреть популярное</a>`);
    return;
  }

  // Берём по несколько последних видео с каждого канала и сортируем по дате.
  const feeds = await Promise.all(channels.slice(0, 30).map((c) =>
    api(`/videos?author_id=${c.channel_id}&limit=6`).catch(() => ({ items: [] }))));
  if (!isCurrent()) return;
  const videos = feeds.flatMap((f) => f.items)
    .sort((a, b) => new Date(b.created_at) - new Date(a.created_at));

  list.innerHTML = `
    <ul class="channel-chips">
      ${channels.map((c) => `
        <li><a href="#/channel/${c.channel_id}">${avatar(c.channel_id, 56)}<span>${esc(c.channel_name)}</span></a></li>`).join("")}
    </ul>
    ${videos.length
      ? `<div class="grid">${videos.map(videoCard).join("")}</div>`
      : emptyState("На ваших каналах пока нет видео.")}`;
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
          <small class="muted">Латиница, цифры и _, от 3 до 32 символов</small>
        </div>`}
      <div class="field">
        <label for="email">Email</label>
        <input class="input" id="email" name="email" type="email" autocomplete="email" required>
      </div>
      <div class="field">
        <label for="password">Пароль</label>
        <input class="input" id="password" name="password" type="password" autocomplete="${isLogin ? "current-password" : "new-password"}" required minlength="6">
        ${isLogin ? "" : '<small class="muted">Не меньше 6 символов</small>'}
      </div>
      <p class="error" id="auth-error" hidden></p>
      <button class="btn btn-primary" type="submit">${isLogin ? "Войти" : "Создать аккаунт"}</button>
      <p class="muted">
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
      toast(isLogin ? `Привет, ${res.user.username}!` : "Аккаунт создан");
      location.hash = next && next.startsWith("#/") && !next.startsWith("#/login") ? next : "#/";
      renderUserbar();
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

// Обновляем данные профиля (аватарку, роль) — они могли измениться с прошлого визита.
async function refreshMe() {
  if (!session.token) return;
  try {
    const me = await api("/users/me");
    session.updateUser(me);
    renderUserbar();
    if (me.role === "admin" && !session.isAdmin) {
      toast("Вам выданы права администратора. Выйдите и войдите снова, чтобы они заработали");
    }
  } catch { /* при 401 сессию очистит api() */ }
}

window.addEventListener("hashchange", router);
router();
refreshMe();
