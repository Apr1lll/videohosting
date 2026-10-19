/* =========================================================
   Канал пользователя: аватарка, описание, подписка,
   видео канала и список подписчиков (для владельца).
   ========================================================= */

import {
  api, session, esc, timeAgo, avatar, toast, emptyState, skeletonGrid,
  resizeImage, pickFile,
} from "./core.js";
import { setupSubscribeButton } from "./watch.js";
import { pagedGrid, renderUserbar } from "./main.js";

export async function channelPage(app, id, tab, isCurrent) {
  const user = await api(`/users/${id}`);
  if (!isCurrent()) return;
  document.title = `${user.username} — Кадр`;

  const me = session.user;
  const isMe = !!me && me.id === user.id;
  if (tab === "subscribers" && !isMe) tab = "videos";

  app.innerHTML = `
    <section class="channel-head">
      <div class="channel-avatar">
        <img class="avatar" id="channel-avatar" src="${esc(user.avatar_url)}" width="96" height="96" alt="">
        ${isMe ? `<button class="avatar-edit" id="avatar-btn" aria-label="Сменить аватарку">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M4 8h3l2-3h6l2 3h3v11H4z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/><circle cx="12" cy="13" r="3.5" fill="none" stroke="currentColor" stroke-width="2"/></svg>
        </button>` : ""}
      </div>
      <div class="info">
        <h1>${esc(user.username)}${user.role === "admin" ? ' <span class="badge">админ</span>' : ""}</h1>
        <span class="muted"><span id="subs-count">&nbsp;</span>, на Кадре с ${new Date(user.created_at).toLocaleDateString("ru-RU", { month: "long", year: "numeric" })}</span>
        <div class="bio" id="bio">${user.bio ? esc(user.bio) : `<span class="muted">${isMe ? "Расскажите о себе и своём канале." : "Автор пока ничего о себе не рассказал."}</span>`}</div>
      </div>
      <div class="channel-actions">
        ${isMe
          ? `<button class="btn" id="edit-bio">Изменить описание</button>
             ${user.has_avatar ? `<button class="btn btn-ghost" id="avatar-del" hidden>Удалить аватарку</button>` : ""}`
          : `<button class="btn btn-primary" id="sub-btn" hidden>Подписаться</button>`}
      </div>
    </section>
    ${isMe ? `
      <nav class="tabs" aria-label="Разделы канала">
        <a href="#/channel/${id}" ${tab === "videos" ? 'aria-current="page"' : ""}>Видео</a>
        <a href="#/channel/${id}/subscribers" ${tab === "subscribers" ? 'aria-current="page"' : ""}>Подписчики</a>
      </nav>` : `<div class="page-head"><h2>Видео</h2></div>`}
    <div id="list">${skeletonGrid(4)}</div>
    <div class="more" id="more"></div>`;

  setupSubscribeButton(user.id, document.getElementById("sub-btn"), document.getElementById("subs-count"), isCurrent);

  if (isMe) {
    setupBioEditor(user);
    setupAvatar(user);
  }

  const list = document.getElementById("list");
  if (tab === "subscribers") {
    return subscribersList(list, isCurrent);
  }
  await pagedGrid(list, document.getElementById("more"),
    (page) => `/videos?author_id=${id}&page=${page}&limit=24`, isCurrent,
    isMe
      ? emptyState("Вы ещё ничего не загрузили.", `<a class="btn btn-primary" href="#/upload">Загрузить видео</a>`)
      : emptyState("На канале пока нет видео. Подпишитесь, чтобы не пропустить первое."));
}

async function subscribersList(list, isCurrent) {
  const { items } = await api("/users/me/subscribers");
  if (!isCurrent()) return;
  list.innerHTML = items.length
    ? `<ul class="people">${items.map((s) => `
        <li>
          <a href="#/channel/${s.user_id}">${avatar(s.user_id, 48)}</a>
          <div>
            <a href="#/channel/${s.user_id}" class="people-name">${esc(s.username || `Пользователь #${s.user_id}`)}</a>
            <span class="muted small">подписался ${timeAgo(s.created_at)}</span>
          </div>
        </li>`).join("")}</ul>`
    : emptyState("На ваш канал пока никто не подписан.");
}

function setupBioEditor(user) {
  const btn = document.getElementById("edit-bio");
  const bio = document.getElementById("bio");
  btn.onclick = () => {
    btn.hidden = true;
    bio.innerHTML = `
      <form class="stack" id="bio-form">
        <textarea class="input" name="bio" maxlength="500" rows="3" aria-label="Описание канала">${esc(user.bio)}</textarea>
        <div class="row">
          <button class="btn" type="button" id="bio-cancel">Отмена</button>
          <button class="btn btn-primary" type="submit">Сохранить</button>
        </div>
      </form>`;
    bio.querySelector("textarea").focus();
    document.getElementById("bio-cancel").onclick = () => window.dispatchEvent(new HashChangeEvent("hashchange"));
    document.getElementById("bio-form").onsubmit = async (e) => {
      e.preventDefault();
      try {
        const updated = await api("/users/me", { method: "PATCH", body: { bio: e.target.bio.value } });
        session.updateUser(updated);
        toast("Описание сохранено");
        window.dispatchEvent(new HashChangeEvent("hashchange"));
      } catch (err) { toast(err.message); }
    };
  };
}

function setupAvatar(user) {
  const img = document.getElementById("channel-avatar");
  const del = document.getElementById("avatar-del");
  if (del) del.hidden = false;

  const apply = (updated) => {
    session.updateUser(updated);
    img.src = updated.avatar_url;
    renderUserbar();
  };

  document.getElementById("avatar-btn").onclick = async () => {
    const file = await pickFile("image/*");
    if (!file) return;
    try {
      // Уменьшаем в браузере до 256×256: файл весит десятки килобайт и грузится мгновенно.
      const blob = await resizeImage(file, 256, 256);
      const fd = new FormData();
      fd.append("avatar", blob, "avatar.jpg");
      apply(await api("/users/me/avatar", { method: "PUT", form: fd }));
      toast("Аватарка обновлена");
      if (!document.getElementById("avatar-del")) window.dispatchEvent(new HashChangeEvent("hashchange"));
    } catch (e) { toast(e.message || "Не удалось открыть картинку"); }
  };

  if (del) del.onclick = async () => {
    if (!confirm("Удалить аватарку?")) return;
    try {
      apply(await api("/users/me/avatar", { method: "DELETE" }));
      toast("Аватарка удалена");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    } catch (e) { toast(e.message); }
  };
}

