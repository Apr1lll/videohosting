/* =========================================================
   Страница видео: плеер, реакции, подписка, редактирование,
   рекомендации и комментарии с ответами.
   ========================================================= */

import {
  api, session, esc, compact, views, subscribers, timeAgo, thumb, avatar, myAvatar,
  toast, requireLogin, emptyState, openDialog, drawToBlob, resizeImage, pickFile,
} from "./core.js";
import { createPlayer } from "./player.js";

let player = null;

// Вызывается роутером при уходе со страницы видео.
export function leaveWatch() {
  player?.destroy();
  player = null;
}

export async function videoPage(app, id, isCurrent) {
  const video = await api(`/videos/${id}`);
  if (!isCurrent()) return;
  document.title = `${video.title} — Кадр`;

  const me = session.user;
  const isOwner = me && me.id === video.author_id;
  const canEdit = session.canEdit(video.author_id);

  app.innerHTML = `
    <div class="watch">
      <article class="watch-main">
        <div id="player"></div>
        <h1 class="watch-title" id="watch-title">${esc(video.title)}</h1>

        <div class="watch-bar">
          <div class="author">
            <a href="#/channel/${video.author_id}">${avatar(video.author_id, 40)}</a>
            <div>
              <a class="author-name" href="#/channel/${video.author_id}">${esc(video.author_name)}</a>
              <div class="muted small" id="subs-count">&nbsp;</div>
            </div>
            ${isOwner ? "" : `<button class="btn" id="sub-btn" hidden>Подписаться</button>`}
          </div>
          <div class="actions">
            <div class="reactions" role="group" aria-label="Оценка">
              <button class="btn" id="like-btn" aria-label="Нравится">
                <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M7 10v10H4V10zM9 20h8.2a2 2 0 0 0 2-1.6l1.2-6A2 2 0 0 0 18.4 10H14l.8-4a1.6 1.6 0 0 0-2.9-1.2L9 10z" fill="currentColor"/></svg>
                <span>0</span></button>
              <button class="btn" id="dislike-btn" aria-label="Не нравится">
                <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" style="transform:rotate(180deg)"><path d="M7 10v10H4V10zM9 20h8.2a2 2 0 0 0 2-1.6l1.2-6A2 2 0 0 0 18.4 10H14l.8-4a1.6 1.6 0 0 0-2.9-1.2L9 10z" fill="currentColor"/></svg>
                <span>0</span></button>
            </div>
            ${canEdit ? `
              <button class="btn" id="edit-btn">Изменить</button>
              <button class="btn btn-danger" id="delete-btn">Удалить</button>` : ""}
          </div>
        </div>

        <div class="description" id="description"></div>

        <section class="comments" aria-labelledby="comments-title">
          <h2 id="comments-title">Комментарии</h2>
          <div id="comment-form-slot"></div>
          <div id="comment-list"></div>
          <div class="more" id="comment-more"></div>
        </section>
      </article>

      <aside class="side" aria-label="Рекомендации">
        <div class="side-list" id="side-list">${'<div class="side-skeleton"></div>'.repeat(6)}</div>
      </aside>
    </div>`;

  player = createPlayer(document.getElementById("player"), {
    src: `/videos/${video.id}/stream`,
    poster: video.thumbnail_url || "",
  });

  renderDescription(video);
  setupReactions(video, isCurrent);
  setupSubscription(video, isCurrent);
  setupComments(video, isCurrent);
  loadRecommendations(video, isCurrent);

  if (canEdit) {
    document.getElementById("edit-btn").onclick = () => editVideoDialog(video, (updated) => {
      Object.assign(video, updated);
      document.getElementById("watch-title").textContent = updated.title;
      document.title = `${updated.title} — Кадр`;
      renderDescription(video);
      if (updated.thumbnail_url) player.video.poster = updated.thumbnail_url;
    }, player.video);
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

function renderDescription(video) {
  document.getElementById("description").innerHTML = `
    <span class="stats">${views(video.views)} · ${timeAgo(video.created_at)}</span>
    ${video.description ? `<p class="desc-text">${esc(video.description)}</p>` : '<p class="muted">Автор не добавил описание.</p>'}`;
}

/* ---------- Редактирование видео (автор или админ) ---------- */

// Окно редактирования: название, описание и превью. frameSource — <video> плеера,
// из которого можно взять текущий кадр как новое превью.
export function editVideoDialog(video, onSaved, frameSource = null) {
  let newThumb = null;

  openDialog(`
    <form method="dialog" class="dialog-body" id="edit-form">
      <h2>Изменить видео</h2>
      <div class="field">
        <label for="ev-title">Название</label>
        <input class="input" id="ev-title" name="title" maxlength="200" required value="${esc(video.title)}">
      </div>
      <div class="field">
        <label for="ev-desc">Описание</label>
        <textarea class="input" id="ev-desc" name="description" maxlength="5000" rows="5">${esc(video.description)}</textarea>
      </div>
      <div class="field">
        <span class="label">Превью</span>
        <div class="thumb-edit">
          <div class="thumb-preview" id="ev-preview">${thumb(video)}</div>
          <div class="thumb-actions">
            ${frameSource ? `<button class="btn" type="button" id="ev-frame">Взять текущий кадр</button>` : ""}
            <button class="btn" type="button" id="ev-image">Загрузить картинку</button>
            <small class="muted">${frameSource ? "Поставьте видео на нужный момент и нажмите «Взять текущий кадр»." : "JPEG, PNG или WebP."}</small>
          </div>
        </div>
      </div>
      <p class="error" id="ev-error" hidden></p>
      <div class="dialog-actions">
        <button class="btn" type="button" data-close>Отмена</button>
        <button class="btn btn-primary" type="submit">Сохранить</button>
      </div>
    </form>`, (dlg) => {
    const form = dlg.querySelector("#edit-form");
    const preview = dlg.querySelector("#ev-preview");
    const errorEl = dlg.querySelector("#ev-error");

    const showThumb = (blob) => {
      newThumb = blob;
      preview.innerHTML = `<div class="thumb"><img src="${URL.createObjectURL(blob)}" alt="Новое превью"></div>`;
    };

    dlg.querySelector("#ev-frame")?.addEventListener("click", async () => {
      const v = frameSource;
      if (!v.videoWidth) return toast("Видео ещё не загрузилось");
      showThumb(await drawToBlob(v, v.videoWidth, v.videoHeight));
    });
    dlg.querySelector("#ev-image").addEventListener("click", async () => {
      const file = await pickFile("image/*");
      if (!file) return;
      try { showThumb(await resizeImage(file, 1280, 720)); }
      catch { toast("Не удалось открыть картинку"); }
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      errorEl.hidden = true;
      const btn = form.querySelector('[type="submit"]');
      btn.disabled = true;
      try {
        let updated = await api(`/videos/${video.id}`, {
          method: "PATCH",
          body: { title: form.title.value, description: form.description.value },
        });
        if (newThumb) {
          const fd = new FormData();
          fd.append("thumbnail", newThumb, "thumbnail.jpg");
          updated = await api(`/videos/${video.id}/thumbnail`, { method: "PUT", form: fd });
        }
        dlg.close();
        toast("Изменения сохранены");
        onSaved(updated);
      } catch (err) {
        errorEl.textContent = err.message;
        errorEl.hidden = false;
        btn.disabled = false;
      }
    });
  });
}

/* ---------- Реакции и подписка ---------- */

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

// Кнопка подписки — используется и на странице видео, и на странице канала.
export async function setupSubscribeButton(channelId, btn, countEl, isCurrent) {
  let count = 0;
  let subscribed = false;

  const paint = () => {
    if (countEl) countEl.textContent = subscribers(count);
    if (!btn) return;
    btn.hidden = false;
    btn.textContent = subscribed ? "Вы подписаны" : "Подписаться";
    btn.classList.toggle("is-on", subscribed);
    btn.classList.toggle("btn-primary", !subscribed);
  };

  try {
    const [subs, mine] = await Promise.all([
      api(`/channels/${channelId}/subscribers`),
      session.token ? api("/users/me/subscriptions") : Promise.resolve({ items: [] }),
    ]);
    if (!isCurrent()) return;
    count = subs.subscribers;
    subscribed = mine.items.some((s) => s.channel_id === channelId);
    paint();
  } catch { return; }

  if (btn) btn.onclick = async () => {
    if (!requireLogin()) return;
    btn.disabled = true;
    try {
      await api(`/channels/${channelId}/subscribe`, { method: subscribed ? "DELETE" : "POST" });
      subscribed = !subscribed;
      count += subscribed ? 1 : -1;
      paint();
      toast(subscribed ? "Вы подписались на канал" : "Вы отписались от канала");
    } catch (e) { toast(e.message); }
    finally { btn.disabled = false; }
  };
}

function setupSubscription(video, isCurrent) {
  setupSubscribeButton(video.author_id, document.getElementById("sub-btn"),
    document.getElementById("subs-count"), isCurrent);
}

/* ---------- Рекомендации ---------- */

async function loadRecommendations(video, isCurrent) {
  const el = document.getElementById("side-list");
  try {
    const { items } = await api(`/videos/${video.id}/recommendations?limit=20`);
    if (!isCurrent()) return;
    el.innerHTML = items.length
      ? items.map((v) => `
          <a class="side-item" href="#/video/${v.id}">
            ${thumb(v)}
            <span class="card-meta">
              <span class="card-title">${esc(v.title)}</span>
              <span class="card-sub">${esc(v.author_name)}</span>
              <span class="card-sub">${views(v.views)} · ${timeAgo(v.created_at)}</span>
            </span>
          </a>`).join("")
      : `<p class="muted">Других видео пока нет.</p>`;
  } catch {
    el.innerHTML = "";
  }
}

/* ---------- Комментарии и ответы ---------- */

function commentHTML(c) {
  const canEdit = session.canEdit(c.user_id);
  const isReply = !!c.parent_id;
  return `
    <div class="comment${isReply ? " is-reply" : ""}" data-id="${c.id}" data-root="${c.parent_id || c.id}">
      <a href="#/channel/${c.user_id}" class="comment-avatar">${avatar(c.user_id, isReply ? 28 : 40)}</a>
      <div class="comment-main">
        <div class="comment-head">
          <a href="#/channel/${c.user_id}">${esc(c.username)}</a>
          <span class="muted small">${timeAgo(c.created_at)}${c.edited_at ? " (изменено)" : ""}</span>
        </div>
        <p class="comment-text">${esc(c.text)}</p>
        <div class="comment-actions">
          <button class="link-btn" data-act="reply">Ответить</button>
          ${canEdit ? `<button class="link-btn" data-act="edit">Изменить</button>
                       <button class="link-btn danger" data-act="delete">Удалить</button>` : ""}
        </div>
        <div class="reply-form-slot"></div>
        ${!isReply && c.reply_count > 0 ? `
          <button class="link-btn replies-toggle" data-act="replies" data-count="${c.reply_count}">
            ${repliesLabel(c.reply_count, false)}</button>` : ""}
        ${isReply ? "" : `<div class="replies" hidden></div>`}
      </div>
    </div>`;
}

const repliesLabel = (n, open) =>
  open ? "Скрыть ответы" : `${n} ${n % 10 === 1 && n % 100 !== 11 ? "ответ" : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 12 || n % 100 > 14) ? "ответа" : "ответов"}`;

function commentForm(placeholder, submitLabel, value = "") {
  return `
    <form class="comment-form">
      ${session.user ? myAvatar(32) : ""}
      <div class="comment-form-main">
        <textarea class="input" name="text" maxlength="1000" rows="2" placeholder="${placeholder}" aria-label="${placeholder}" required>${esc(value)}</textarea>
        <div class="row">
          <button class="btn btn-ghost" type="button" data-cancel>Отмена</button>
          <button class="btn btn-primary" type="submit">${submitLabel}</button>
        </div>
      </div>
    </form>`;
}

function setupComments(video, isCurrent) {
  const slot = document.getElementById("comment-form-slot");
  const list = document.getElementById("comment-list");
  const moreEl = document.getElementById("comment-more");
  let page = 1;

  if (session.token && session.user) {
    slot.innerHTML = commentForm("Напишите комментарий", "Отправить");
    const form = slot.querySelector("form");
    form.querySelector("[data-cancel]").onclick = () => form.reset();
    form.onsubmit = async (e) => {
      e.preventDefault();
      const text = form.text.value.trim();
      if (!text) return;
      const btn = form.querySelector('[type="submit"]');
      btn.disabled = true;
      try {
        const c = await api(`/videos/${video.id}/comments`, { method: "POST", body: { text } });
        list.querySelector(".empty")?.remove();
        list.insertAdjacentHTML("afterbegin", commentHTML(c));
        form.reset();
      } catch (err) { toast(err.message); }
      finally { btn.disabled = false; }
    };
  } else {
    slot.innerHTML = `<p class="muted"><a href="#/login?next=${encodeURIComponent(location.hash)}">Войдите</a>, чтобы оставить комментарий.</p>`;
  }

  // Все кнопки внутри списка обрабатываются одним делегированным обработчиком.
  list.onclick = async (e) => {
    const btn = e.target.closest("[data-act]");
    if (!btn) return;
    const comment = btn.closest(".comment");
    const id = Number(comment.dataset.id);
    const act = btn.dataset.act;

    if (act === "replies") return toggleReplies(comment, btn);
    if (act === "reply") return openReplyForm(video, comment);
    if (act === "edit") return openEditForm(comment, id);
    if (act === "delete") {
      if (!confirm("Удалить комментарий? Ответы на него тоже удалятся.")) return;
      try {
        await api(`/comments/${id}`, { method: "DELETE" });
        comment.remove();
        toast("Комментарий удалён");
      } catch (err) { toast(err.message); }
    }
  };

  async function load() {
    const data = await api(`/videos/${video.id}/comments?page=${page}&limit=20`);
    if (!isCurrent()) return;
    document.getElementById("comments-title").textContent =
      data.total ? `Комментарии: ${data.total}` : "Комментарии";
    if (page === 1 && !data.items.length) {
      list.innerHTML = emptyState("Комментариев пока нет. Будьте первым.");
    } else {
      list.insertAdjacentHTML("beforeend", data.items.map(commentHTML).join(""));
    }
    moreEl.innerHTML = page * data.limit < data.total ? `<button class="btn">Показать ещё комментарии</button>` : "";
    const more = moreEl.querySelector("button");
    if (more) more.onclick = () => { page++; more.disabled = true; load().catch((e) => toast(e.message)); };
  }
  load().catch(() => { list.innerHTML = `<p class="error">Не удалось загрузить комментарии.</p>`; });
}

async function toggleReplies(comment, btn, forceOpen = false) {
  const box = comment.querySelector(".replies");
  const open = box.hidden || forceOpen;
  if (!open) {
    box.hidden = true;
    btn.textContent = repliesLabel(Number(btn.dataset.count), false);
    return;
  }
  try {
    const { items } = await api(`/comments/${comment.dataset.id}/replies`);
    box.innerHTML = items.map(commentHTML).join("");
    box.hidden = false;
    btn.dataset.count = items.length;
    btn.textContent = repliesLabel(items.length, true);
  } catch (e) { toast(e.message); }
}

function openReplyForm(video, comment) {
  if (!requireLogin()) return;
  // Ответ всегда прикрепляется к корневому комментарию ветки.
  const root = comment.classList.contains("is-reply") ? comment.parentElement.closest(".comment") : comment;
  const rootId = Number(root.dataset.id);
  const slot = comment.querySelector(".reply-form-slot");
  if (slot.innerHTML) { slot.innerHTML = ""; return; }

  const mention = comment.classList.contains("is-reply")
    ? `@${comment.querySelector(".comment-head a").textContent} ` : "";
  slot.innerHTML = commentForm("Ваш ответ", "Ответить", mention);
  const form = slot.querySelector("form");
  const ta = form.text;
  ta.focus();
  ta.setSelectionRange(ta.value.length, ta.value.length);
  form.querySelector("[data-cancel]").onclick = () => { slot.innerHTML = ""; };

  form.onsubmit = async (e) => {
    e.preventDefault();
    const text = ta.value.trim();
    if (!text) return;
    try {
      await api(`/videos/${video.id}/comments`, { method: "POST", body: { text, parent_id: rootId } });
      slot.innerHTML = "";
      // Показываем ветку ответов целиком, вместе с новым ответом.
      let toggle = root.querySelector(":scope > .comment-main > .replies-toggle");
      if (!toggle) {
        root.querySelector(":scope > .comment-main > .reply-form-slot")
          .insertAdjacentHTML("afterend", `<button class="link-btn replies-toggle" data-act="replies" data-count="1"></button>`);
        toggle = root.querySelector(":scope > .comment-main > .replies-toggle");
      }
      await toggleReplies(root, toggle, true);
    } catch (err) { toast(err.message); }
  };
}

function openEditForm(comment, id) {
  const textEl = comment.querySelector(":scope > .comment-main > .comment-text");
  const slot = comment.querySelector(":scope > .comment-main > .reply-form-slot");
  slot.innerHTML = commentForm("Текст комментария", "Сохранить", textEl.textContent);
  textEl.hidden = true;
  const form = slot.querySelector("form");
  form.querySelector(".avatar")?.remove();
  form.text.focus();
  form.querySelector("[data-cancel]").onclick = () => { slot.innerHTML = ""; textEl.hidden = false; };
  form.onsubmit = async (e) => {
    e.preventDefault();
    try {
      const updated = await api(`/comments/${id}`, { method: "PATCH", body: { text: form.text.value } });
      textEl.textContent = updated.text;
      textEl.hidden = false;
      slot.innerHTML = "";
      const meta = comment.querySelector(":scope > .comment-main .comment-head .muted");
      if (!meta.textContent.includes("изменено")) meta.textContent += " (изменено)";
      toast("Комментарий изменён");
    } catch (err) { toast(err.message); }
  };
}
