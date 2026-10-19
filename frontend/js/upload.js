/* =========================================================
   Загрузка видео: выбор файла, автоматические кадры для
   превью (вырезаются в браузере), прогресс отправки.
   ========================================================= */

import { upload, fileSize, clock, toast, requireLogin, extractFrames, resizeImage, pickFile } from "./core.js";

const MAX_MB = 500;

export function uploadPage(app) {
  if (!requireLogin()) return;
  document.title = "Загрузить видео — Кадр";

  app.innerHTML = `
    <form class="panel panel-wide" id="upload-form" novalidate>
      <h1>Новое видео</h1>
      <label class="dropzone" id="dropzone">
        <input type="file" name="file" accept=".mp4,.webm,.mov,.mkv,video/*" hidden>
        <strong id="file-name">Выберите файл или перетащите его сюда</strong>
        <span class="muted" id="file-hint">mp4, webm, mov или mkv, до ${MAX_MB} МБ</span>
      </label>

      <div class="field" id="thumb-field" hidden>
        <span class="label">Превью</span>
        <div class="thumb-choices" id="thumb-choices" role="radiogroup" aria-label="Выбор превью"></div>
        <small class="muted" id="thumb-hint"></small>
      </div>

      <div class="field">
        <label for="title">Название</label>
        <input class="input" id="title" name="title" maxlength="200" required>
      </div>
      <div class="field">
        <label for="description">Описание</label>
        <textarea class="input" id="description" name="description" maxlength="5000" rows="4"></textarea>
        <small class="muted">Необязательно. Слова из названия и описания помогают показывать видео в рекомендациях.</small>
      </div>
      <div class="progress" hidden id="progress"><span></span></div>
      <p class="muted small" id="progress-text" hidden></p>
      <p class="error" id="upload-error" hidden></p>
      <button class="btn btn-primary" type="submit">Опубликовать</button>
    </form>`;

  const form = document.getElementById("upload-form");
  const dropzone = document.getElementById("dropzone");
  const errorEl = document.getElementById("upload-error");
  const choices = document.getElementById("thumb-choices");
  const thumbField = document.getElementById("thumb-field");
  const thumbHint = document.getElementById("thumb-hint");

  let file = null;
  let duration = 0;
  let candidates = []; // Blob-ы кадров
  let selected = 0;

  const showError = (msg) => { errorEl.textContent = msg; errorEl.hidden = !msg; };

  function renderChoices() {
    choices.innerHTML = candidates.map((blob, i) => `
      <button type="button" class="thumb-choice${i === selected ? " is-selected" : ""}" role="radio"
              aria-checked="${i === selected}" data-i="${i}" aria-label="Кадр ${i + 1}">
        <img src="${URL.createObjectURL(blob)}" alt="">
      </button>`).join("") + `
      <button type="button" class="thumb-choice thumb-own" id="thumb-own">Своя картинка</button>`;
    choices.querySelectorAll("[data-i]").forEach((b) => b.onclick = () => {
      selected = Number(b.dataset.i);
      renderChoices();
    });
    document.getElementById("thumb-own").onclick = async () => {
      const img = await pickFile("image/*");
      if (!img) return;
      try {
        candidates.push(await resizeImage(img, 1280, 720));
        selected = candidates.length - 1;
        renderChoices();
      } catch { toast("Не удалось открыть картинку"); }
    };
  }

  async function chooseFile(f) {
    if (!f) return;
    showError("");
    if (f.size > MAX_MB * 1024 * 1024) return showError(`Файл больше ${MAX_MB} МБ`);
    file = f;
    document.getElementById("file-name").textContent = f.name;
    document.getElementById("file-hint").textContent = fileSize(f.size);
    if (!form.title.value) form.title.value = f.name.replace(/\.[^.]+$/, "").replace(/[_-]+/g, " ");

    thumbField.hidden = false;
    thumbHint.textContent = "Достаём кадры из видео…";
    choices.innerHTML = "";
    candidates = [];
    selected = 0;

    const res = await extractFrames(f);
    if (file !== f) return; // пока считали кадры, выбрали другой файл
    duration = res.duration;
    candidates = res.frames;
    renderChoices();
    thumbHint.textContent = candidates.length
      ? `Длительность ${clock(duration)}. Выберите кадр для превью или загрузите свою картинку.`
      : "Браузер не смог показать кадры этого файла. Можно загрузить свою картинку или оставить превью по умолчанию.";
  }

  form.file.onchange = () => chooseFile(form.file.files[0]);
  dropzone.ondragover = (e) => { e.preventDefault(); dropzone.classList.add("is-over"); };
  dropzone.ondragleave = () => dropzone.classList.remove("is-over");
  dropzone.ondrop = (e) => {
    e.preventDefault();
    dropzone.classList.remove("is-over");
    chooseFile(e.dataTransfer.files[0]);
  };

  form.onsubmit = async (e) => {
    e.preventDefault();
    showError("");
    if (!file) return showError("Выберите файл с видео");
    if (!form.title.value.trim()) return showError("Добавьте название");

    // Порядок полей важен: сервер читает форму потоком, и короткие поля
    // должны прийти раньше большого файла.
    const data = new FormData();
    data.append("title", form.title.value);
    data.append("description", form.description.value);
    if (duration) data.append("duration", String(duration));
    if (candidates[selected]) data.append("thumbnail", candidates[selected], "thumbnail.jpg");
    data.append("file", file);

    const submit = form.querySelector('button[type="submit"]');
    const progress = document.getElementById("progress");
    const bar = progress.querySelector("span");
    const text = document.getElementById("progress-text");
    submit.disabled = true;
    submit.textContent = "Публикуем…";
    progress.hidden = text.hidden = false;

    const started = Date.now();
    try {
      const video = await upload("/videos", data, (ratio) => {
        bar.style.width = `${ratio * 100}%`;
        const sec = (Date.now() - started) / 1000;
        const speed = (ratio * file.size) / Math.max(sec, 0.1);
        const left = speed > 0 ? ((1 - ratio) * file.size) / speed : 0;
        text.textContent = `${Math.round(ratio * 100)}% · ${fileSize(speed)}/с · осталось ${clock(left)}`;
      });
      toast("Видео опубликовано");
      location.hash = `#/video/${video.id}`;
    } catch (err) {
      submit.disabled = false;
      submit.textContent = "Опубликовать";
      progress.hidden = text.hidden = true;
      showError(err.message);
    }
  };
}
