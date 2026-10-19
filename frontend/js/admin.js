/* =========================================================
   Админ-панель: сводка, пользователи, видео, комментарии.
   Доступна только пользователям с ролью admin (проверяет
   и gateway, и каждый сервис — здесь только интерфейс).
   ========================================================= */

import {
  api, session, esc, compact, fileSize, timeAgo, thumb, avatar, toast, emptyState,
} from "./core.js";
import { editVideoDialog } from "./watch.js";

const TABS = [
  ["", "Обзор"],
  ["users", "Пользователи"],
  ["videos", "Видео"],
  ["comments", "Комментарии"],
];

export async function adminPage(app, tab = "", isCurrent) {
  if (!session.isAdmin) {
    app.innerHTML = emptyState(
      "Эта страница только для администраторов. Если права выдали недавно — выйдите и войдите снова.",
      `<a class="btn" href="#/">На главную</a>`);
    return;
  }
  document.title = "Админ-панель — Кадр";

  app.innerHTML = `
    <div class="page-head"><h1>Админ-панель</h1></div>
    <nav class="tabs" aria-label="Разделы админ-панели">
      ${TABS.map(([key, label]) => `<a href="#/admin${key ? "/" + key : ""}" ${tab === key ? 'aria-current="page"' : ""}>${label}</a>`).join("")}
    </nav>
    <div id="admin-body"></div>`;
  const body = document.getElementById("admin-body");

  switch (tab) {
    case "users": return listView(body, usersConfig, isCurrent);
    case "videos": return listView(body, videosConfig, isCurrent);
    case "comments": return listView(body, commentsConfig, isCurrent);
    default: return overview(body, isCurrent);
  }
}

/* ---------- Обзор ---------- */

async function overview(body, isCurrent) {
  body.innerHTML = `<p class="muted">Загружаем…</p>`;
  const [users, videos, inter] = await Promise.all([
    api("/admin/users?limit=1"),
    api("/admin/videos/stats"),
    api("/admin/interactions/stats"),
  ]);
  if (!isCurrent()) return;

  const stats = [
    ["Пользователей", compact(users.total), "#/admin/users"],
    ["Авторов с видео", compact(videos.authors), "#/admin/users"],
    ["Видео", compact(videos.videos), "#/admin/videos"],
    ["Просмотров", compact(videos.views), "#/admin/videos"],
    ["Занято видео на диске", fileSize(videos.storage_bytes), "#/admin/videos"],
    ["Комментариев", compact(inter.comments), "#/admin/comments"],
    ["Лайков / дизлайков", `${compact(inter.likes)} / ${compact(inter.dislikes)}`, ""],
    ["Подписок", compact(inter.subscriptions), ""],
  ];
  body.innerHTML = `
    <dl class="stats-grid">
      ${stats.map(([label, value, href]) => `
        <div class="stat">
          <dt>${href ? `<a href="${href}">${label}</a>` : label}</dt>
          <dd>${esc(value)}</dd>
        </div>`).join("")}
    </dl>`;
}

/* ---------- Универсальный список с поиском и страницами ---------- */

async function listView(body, cfg, isCurrent) {
  let page = 1;
  let search = "";

  body.innerHTML = `
    <form class="admin-toolbar" role="search">
      <input class="input" type="search" name="q" placeholder="${cfg.searchPlaceholder}" aria-label="${cfg.searchPlaceholder}">
      <button class="btn" type="submit">Найти</button>
    </form>
    <div id="admin-list"></div>
    <div class="pager" id="pager"></div>`;
  const list = body.querySelector("#admin-list");
  const pager = body.querySelector("#pager");

  body.querySelector("form").onsubmit = (e) => {
    e.preventDefault();
    search = e.target.q.value.trim();
    page = 1;
    load();
  };

  async function load() {
    list.innerHTML = `<p class="muted">Загружаем…</p>`;
    const data = await api(cfg.url(page, search));
    if (!isCurrent()) return;

    list.innerHTML = data.items.length
      ? `<div class="table-wrap"><table class="table">
          <thead><tr>${cfg.columns.map((c) => `<th scope="col">${c}</th>`).join("")}</tr></thead>
          <tbody>${data.items.map(cfg.row).join("")}</tbody>
        </table></div>`
      : emptyState(search ? "По этому запросу ничего не нашлось." : "Здесь пока пусто.");

    const pages = Math.max(1, Math.ceil(data.total / data.limit));
    pager.innerHTML = `
      <button class="btn" data-p="${page - 1}" ${page <= 1 ? "disabled" : ""}>Назад</button>
      <span class="muted">Страница ${page} из ${pages}, всего ${data.total}</span>
      <button class="btn" data-p="${page + 1}" ${page >= pages ? "disabled" : ""}>Дальше</button>`;
    pager.querySelectorAll("[data-p]").forEach((b) => b.onclick = () => { page = Number(b.dataset.p); load(); });

    list.querySelectorAll("[data-act]").forEach((btn) => {
      btn.onclick = async () => {
        const item = data.items.find((x) => String(x.id) === btn.closest("tr").dataset.id);
        try {
          if (await cfg.actions[btn.dataset.act](item, btn)) load();
        } catch (err) { toast(err.message); }
      };
    });
    list.querySelectorAll("select[data-role]").forEach((sel) => {
      sel.onchange = async () => {
        const id = sel.closest("tr").dataset.id;
        try {
          await api(`/admin/users/${id}`, { method: "PATCH", body: { role: sel.value } });
          toast(sel.value === "admin" ? "Права администратора выданы" : "Права администратора сняты");
        } catch (err) { toast(err.message); load(); }
      };
    });
  }
  load().catch((e) => { list.innerHTML = `<p class="error">${esc(e.message)}</p>`; });
}

const date = (iso) => new Date(iso).toLocaleDateString("ru-RU");

/* ---------- Пользователи ---------- */

const usersConfig = {
  searchPlaceholder: "Имя или email",
  url: (page, q) => `/admin/users?page=${page}&limit=20&search=${encodeURIComponent(q)}`,
  columns: ["Пользователь", "Email", "Роль", "Регистрация", "Действия"],
  row: (u) => {
    const self = session.user && session.user.id === u.id;
    return `
    <tr data-id="${u.id}">
      <td><a class="user-cell" href="#/channel/${u.id}">${avatar(u.id, 32, u.avatar_url.split("v=")[1])}<span>${esc(u.username)}</span></a></td>
      <td>${esc(u.email)}</td>
      <td>
        <select class="input input-sm" data-role aria-label="Роль ${esc(u.username)}" ${self ? "disabled" : ""}>
          <option value="user" ${u.role !== "admin" ? "selected" : ""}>пользователь</option>
          <option value="admin" ${u.role === "admin" ? "selected" : ""}>админ</option>
        </select>
      </td>
      <td>${date(u.created_at)}</td>
      <td class="cell-actions">
        <button class="btn btn-sm" data-act="bio">Описание</button>
        <button class="btn btn-sm" data-act="password">Пароль</button>
        ${u.has_avatar ? `<button class="btn btn-sm" data-act="avatar">Убрать аватар</button>` : ""}
        ${self ? "" : `<button class="btn btn-sm btn-danger" data-act="delete">Удалить</button>`}
      </td>
    </tr>`;
  },
  actions: {
    async bio(u) {
      const bio = prompt(`Описание канала ${u.username}:`, u.bio);
      if (bio === null) return false;
      await api(`/admin/users/${u.id}`, { method: "PATCH", body: { bio } });
      toast("Описание сохранено");
      return true;
    },
    async password(u) {
      const password = prompt(`Новый пароль для ${u.username} (от 6 символов):`);
      if (!password) return false;
      await api(`/admin/users/${u.id}`, { method: "PATCH", body: { password } });
      toast("Пароль изменён");
      return false;
    },
    async avatar(u) {
      if (!confirm(`Убрать аватарку пользователя ${u.username}?`)) return false;
      await api(`/admin/users/${u.id}/avatar`, { method: "DELETE" });
      toast("Аватарка удалена");
      return true;
    },
    async delete(u) {
      if (!confirm(`Удалить пользователя ${u.username}? Вместе с ним удалятся все его видео, комментарии, лайки и подписки. Это необратимо.`)) return false;
      await api(`/admin/users/${u.id}`, { method: "DELETE" });
      toast("Пользователь удалён");
      return true;
    },
  },
};

/* ---------- Видео ---------- */

const videosConfig = {
  searchPlaceholder: "Название, описание или автор",
  url: (page, q) => `/videos?page=${page}&limit=20&search=${encodeURIComponent(q)}`,
  columns: ["Видео", "Автор", "Просмотры", "Размер", "Загружено", "Действия"],
  row: (v) => `
    <tr data-id="${v.id}">
      <td><a class="video-cell" href="#/video/${v.id}">${thumb(v)}<span>${esc(v.title)}</span></a></td>
      <td><a href="#/channel/${v.author_id}">${esc(v.author_name)}</a></td>
      <td>${compact(v.views)}</td>
      <td>${fileSize(v.size_bytes)}</td>
      <td>${timeAgo(v.created_at)}</td>
      <td class="cell-actions">
        <button class="btn btn-sm" data-act="edit">Изменить</button>
        <button class="btn btn-sm btn-danger" data-act="delete">Удалить</button>
      </td>
    </tr>`,
  actions: {
    edit(v) {
      return new Promise((resolve) => editVideoDialog(v, () => resolve(true)));
    },
    async delete(v) {
      if (!confirm(`Удалить видео «${v.title}»?`)) return false;
      await api(`/videos/${v.id}`, { method: "DELETE" });
      toast("Видео удалено");
      return true;
    },
  },
};

/* ---------- Комментарии ---------- */

const commentsConfig = {
  searchPlaceholder: "Текст комментария или автор",
  url: (page, q) => `/admin/comments?page=${page}&limit=20&search=${encodeURIComponent(q)}`,
  columns: ["Автор", "Комментарий", "Видео", "Когда", "Действия"],
  row: (c) => `
    <tr data-id="${c.id}">
      <td><a class="user-cell" href="#/channel/${c.user_id}">${avatar(c.user_id, 28)}<span>${esc(c.username)}</span></a></td>
      <td class="cell-text">${c.parent_id ? '<span class="badge">ответ</span> ' : ""}${esc(c.text)}${c.edited_at ? ' <span class="muted small">(изменено)</span>' : ""}</td>
      <td><a href="#/video/${c.video_id}">#${c.video_id}</a></td>
      <td>${timeAgo(c.created_at)}</td>
      <td class="cell-actions">
        <button class="btn btn-sm" data-act="edit">Изменить</button>
        <button class="btn btn-sm btn-danger" data-act="delete">Удалить</button>
      </td>
    </tr>`,
  actions: {
    async edit(c) {
      const text = prompt("Текст комментария:", c.text);
      if (text === null) return false;
      await api(`/comments/${c.id}`, { method: "PATCH", body: { text } });
      toast("Комментарий изменён");
      return true;
    },
    async delete(c) {
      if (!confirm("Удалить комментарий? Ответы на него тоже удалятся.")) return false;
      await api(`/comments/${c.id}`, { method: "DELETE" });
      toast("Комментарий удалён");
      return true;
    },
  },
};
