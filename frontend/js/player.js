/* =========================================================
   Собственный видеоплеер вместо стандартного <video controls>.
   Умеет: play/pause, перемотку с подсказкой времени, полосу
   буферизации, громкость, скорость, полноэкранный режим,
   картинку в картинке и горячие клавиши как на YouTube.
   ========================================================= */

import { clock } from "./core.js";

const icons = {
  play: '<path d="M8 5.5v13l11-6.5z"/>',
  pause: '<path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z"/>',
  volume: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z"/><path d="M15.5 8.5a5 5 0 0 1 0 7M18 6a8.5 8.5 0 0 1 0 12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>',
  muted: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z"/><path d="M16 9.5l5 5M21 9.5l-5 5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>',
  full: '<path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  exitFull: '<path d="M9 4v5H4M20 9h-5V4M15 20v-5h5M4 15h5v5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  pip: '<rect x="3" y="5" width="18" height="14" rx="2" fill="none" stroke="currentColor" stroke-width="1.8"/><rect x="12" y="11" width="7" height="6" rx="1"/>',
  back: '<path d="M11 7l-5 5 5 5M18 7l-5 5 5 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  forward: '<path d="M6 7l5 5-5 5M13 7l5 5-5 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
};
const icon = (name) => `<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" fill="currentColor">${icons[name]}</svg>`;

const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2];

function loadPrefs() {
  try { return JSON.parse(localStorage.getItem("player")) || {}; } catch { return {}; }
}
function savePrefs(p) {
  try { localStorage.setItem("player", JSON.stringify(p)); } catch {}
}

/**
 * Создаёт плеер внутри container.
 * @returns {{video: HTMLVideoElement, destroy: Function}}
 */
export function createPlayer(container, { src, poster, autoplay = true }) {
  container.classList.add("player");
  container.tabIndex = 0;
  container.setAttribute("role", "region");
  container.setAttribute("aria-label", "Видеоплеер");
  container.innerHTML = `
    <video playsinline preload="auto" ${poster ? `poster="${poster}"` : ""}></video>
    <div class="p-spinner" hidden></div>
    <button class="p-big" type="button" aria-label="Смотреть">${icon("play")}</button>
    <div class="p-flash" aria-hidden="true"></div>
    <div class="p-controls">
      <div class="p-progress" role="slider" aria-label="Перемотка" tabindex="-1">
        <div class="p-buffer"></div>
        <div class="p-played"></div>
        <div class="p-handle"></div>
        <div class="p-tip" hidden>0:00</div>
      </div>
      <div class="p-row">
        <button class="p-btn" data-act="play" type="button" aria-label="Смотреть">${icon("play")}</button>
        <div class="p-volume">
          <button class="p-btn" data-act="mute" type="button" aria-label="Выключить звук">${icon("volume")}</button>
          <input class="p-vol" type="range" min="0" max="1" step="0.05" aria-label="Громкость">
        </div>
        <span class="p-time"><span data-cur>0:00</span> / <span data-dur>0:00</span></span>
        <span class="p-spacer"></span>
        <div class="p-speed-wrap">
          <button class="p-btn p-speed" data-act="speed" type="button" aria-haspopup="true" aria-label="Скорость">1×</button>
          <div class="p-menu" hidden role="menu">
            ${SPEEDS.map((s) => `<button type="button" role="menuitemradio" data-speed="${s}">${s === 1 ? "Обычная" : s + "×"}</button>`).join("")}
          </div>
        </div>
        ${document.pictureInPictureEnabled ? `<button class="p-btn" data-act="pip" type="button" aria-label="Картинка в картинке">${icon("pip")}</button>` : ""}
        <button class="p-btn" data-act="full" type="button" aria-label="Во весь экран">${icon("full")}</button>
      </div>
    </div>`;

  const $ = (sel) => container.querySelector(sel);
  const video = $("video");
  const big = $(".p-big");
  const spinner = $(".p-spinner");
  const flash = $(".p-flash");
  const progress = $(".p-progress");
  const played = $(".p-played");
  const buffer = $(".p-buffer");
  const handle = $(".p-handle");
  const tip = $(".p-tip");
  const playBtn = $('[data-act="play"]');
  const muteBtn = $('[data-act="mute"]');
  const fullBtn = $('[data-act="full"]');
  const speedBtn = $('[data-act="speed"]');
  const menu = $(".p-menu");
  const vol = $(".p-vol");
  const cur = $("[data-cur]");
  const dur = $("[data-dur]");

  // Восстанавливаем громкость и скорость с прошлого раза.
  const prefs = loadPrefs();
  video.volume = prefs.volume ?? 1;
  video.muted = !!prefs.muted;
  video.playbackRate = prefs.rate ?? 1;
  video.src = src;

  /* ----- отрисовка состояния ----- */

  const paintPlay = () => {
    const paused = video.paused;
    playBtn.innerHTML = icon(paused ? "play" : "pause");
    playBtn.setAttribute("aria-label", paused ? "Смотреть" : "Пауза");
    container.classList.toggle("is-paused", paused);
    big.hidden = !paused;
  };
  const paintVolume = () => {
    const silent = video.muted || video.volume === 0;
    muteBtn.innerHTML = icon(silent ? "muted" : "volume");
    muteBtn.setAttribute("aria-label", silent ? "Включить звук" : "Выключить звук");
    vol.value = video.muted ? 0 : video.volume;
    vol.style.setProperty("--fill", `${vol.value * 100}%`);
  };
  const paintTime = () => {
    const d = video.duration || 0;
    const p = d ? (video.currentTime / d) * 100 : 0;
    played.style.width = `${p}%`;
    handle.style.left = `${p}%`;
    cur.textContent = clock(video.currentTime);
    dur.textContent = clock(d);
    progress.setAttribute("aria-valuenow", Math.round(p));
  };
  const paintBuffer = () => {
    const d = video.duration;
    if (!d || !video.buffered.length) return;
    // Берём буферизованный отрезок, в котором сейчас находится воспроизведение.
    for (let i = 0; i < video.buffered.length; i++) {
      if (video.buffered.start(i) <= video.currentTime + 0.5) {
        buffer.style.width = `${(video.buffered.end(i) / d) * 100}%`;
      }
    }
  };
  const paintSpeed = () => {
    speedBtn.textContent = video.playbackRate === 1 ? "1×" : `${video.playbackRate}×`;
    menu.querySelectorAll("[data-speed]").forEach((b) =>
      b.setAttribute("aria-checked", Number(b.dataset.speed) === video.playbackRate));
  };
  const paintFull = () => {
    const on = document.fullscreenElement === container;
    fullBtn.innerHTML = icon(on ? "exitFull" : "full");
    fullBtn.setAttribute("aria-label", on ? "Выйти из полноэкранного режима" : "Во весь экран");
    container.classList.toggle("is-full", on);
  };

  // Короткая анимация-подсказка в центре: что произошло после нажатия клавиши.
  const showFlash = (html) => {
    flash.innerHTML = html;
    flash.classList.remove("is-on");
    void flash.offsetWidth; // перезапуск анимации
    flash.classList.add("is-on");
  };

  /* ----- действия ----- */

  const toggle = () => (video.paused ? video.play().catch(() => {}) : video.pause());
  const seekBy = (sec) => {
    video.currentTime = Math.max(0, Math.min(video.duration || 0, video.currentTime + sec));
    showFlash(`${icon(sec < 0 ? "back" : "forward")}<span>${Math.abs(sec)} с</span>`);
    wake();
  };
  const setVolume = (v) => {
    video.volume = Math.max(0, Math.min(1, v));
    video.muted = video.volume === 0;
  };
  const toggleFull = () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else container.requestFullscreen?.().catch(() => {});
  };
  const persist = () => savePrefs({ volume: video.volume, muted: video.muted, rate: video.playbackRate });

  /* ----- автоскрытие панели во время просмотра ----- */

  let idleTimer;
  const wake = () => {
    container.classList.add("is-active");
    clearTimeout(idleTimer);
    idleTimer = setTimeout(() => {
      if (!video.paused && menu.hidden) container.classList.remove("is-active");
    }, 2500);
  };

  /* ----- события видео ----- */

  video.addEventListener("play", paintPlay);
  video.addEventListener("pause", () => { paintPlay(); wake(); });
  video.addEventListener("timeupdate", () => { paintTime(); paintBuffer(); });
  video.addEventListener("progress", paintBuffer);
  video.addEventListener("loadedmetadata", paintTime);
  video.addEventListener("volumechange", () => { paintVolume(); persist(); });
  video.addEventListener("ratechange", () => { paintSpeed(); persist(); });
  video.addEventListener("waiting", () => { spinner.hidden = false; });
  video.addEventListener("playing", () => { spinner.hidden = true; });
  video.addEventListener("canplay", () => { spinner.hidden = true; });
  video.addEventListener("ended", () => { container.classList.add("is-active"); });
  video.addEventListener("error", () => {
    spinner.hidden = true;
    container.classList.add("is-error");
  });

  /* ----- мышь и сенсор ----- */

  video.addEventListener("click", () => { toggle(); container.focus({ preventScroll: true }); });
  video.addEventListener("dblclick", toggleFull);
  big.addEventListener("click", toggle);
  playBtn.addEventListener("click", toggle);
  muteBtn.addEventListener("click", () => {
    if (video.muted || video.volume === 0) { video.muted = false; if (video.volume === 0) video.volume = 0.5; }
    else video.muted = true;
  });
  vol.addEventListener("input", () => setVolume(Number(vol.value)));
  fullBtn.addEventListener("click", toggleFull);
  $('[data-act="pip"]')?.addEventListener("click", () => {
    if (document.pictureInPictureElement) document.exitPictureInPicture();
    else video.requestPictureInPicture().catch(() => {});
  });
  speedBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    menu.hidden = !menu.hidden;
  });
  menu.addEventListener("click", (e) => {
    const b = e.target.closest("[data-speed]");
    if (!b) return;
    video.playbackRate = Number(b.dataset.speed);
    menu.hidden = true;
  });
  const closeMenu = (e) => { if (!menu.contains(e.target) && e.target !== speedBtn) menu.hidden = true; };
  document.addEventListener("click", closeMenu);

  container.addEventListener("mousemove", wake);
  container.addEventListener("touchstart", wake, { passive: true });
  container.addEventListener("mouseleave", () => { if (!video.paused) container.classList.remove("is-active"); });

  // Перемотка перетаскиванием по полосе (работает и мышью, и пальцем через Pointer Events).
  const ratioAt = (clientX) => {
    const r = progress.getBoundingClientRect();
    return Math.max(0, Math.min(1, (clientX - r.left) / r.width));
  };
  let dragging = false;
  progress.addEventListener("pointerdown", (e) => {
    dragging = true;
    progress.setPointerCapture(e.pointerId);
    video.currentTime = ratioAt(e.clientX) * (video.duration || 0);
    container.classList.add("is-seeking");
  });
  progress.addEventListener("pointermove", (e) => {
    const ratio = ratioAt(e.clientX);
    tip.hidden = false;
    tip.textContent = clock(ratio * (video.duration || 0));
    tip.style.left = `${ratio * 100}%`;
    if (dragging) video.currentTime = ratio * (video.duration || 0);
  });
  const endDrag = () => { dragging = false; container.classList.remove("is-seeking"); };
  progress.addEventListener("pointerup", endDrag);
  progress.addEventListener("pointercancel", endDrag);
  progress.addEventListener("pointerleave", () => { if (!dragging) tip.hidden = true; });

  document.addEventListener("fullscreenchange", paintFull);

  /* ----- горячие клавиши (как на YouTube) ----- */

  const onKey = (e) => {
    const t = e.target;
    if (t.closest("input, textarea, select, [contenteditable], dialog") && !container.contains(t)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    // Пробел на кнопке или ссылке вне плеера должен нажимать её, а не ставить видео на паузу.
    if (e.key === " " && t.closest("button, a, summary") && !container.contains(t)) return;
    const handled = {
      " ": () => toggle(), k: () => toggle(), "л": () => toggle(),
      j: () => seekBy(-10), "о": () => seekBy(-10), l: () => seekBy(10), "д": () => seekBy(10),
      ArrowLeft: () => seekBy(-5), ArrowRight: () => seekBy(5),
      ArrowUp: () => { setVolume(video.volume + 0.05); showFlash(`<span>${Math.round(video.volume * 100)}%</span>`); },
      ArrowDown: () => { setVolume(video.volume - 0.05); showFlash(`<span>${Math.round(video.volume * 100)}%</span>`); },
      m: () => muteBtn.click(), "ь": () => muteBtn.click(),
      f: () => toggleFull(), "а": () => toggleFull(),
      Home: () => { video.currentTime = 0; }, End: () => { video.currentTime = video.duration || 0; },
    }[e.key];
    if (handled) {
      e.preventDefault();
      handled();
      wake();
    } else if (/^[0-9]$/.test(e.key) && video.duration) {
      video.currentTime = (Number(e.key) / 10) * video.duration; // 0–9: переход к 0–90%
    }
  };
  document.addEventListener("keydown", onKey);

  paintPlay();
  paintVolume();
  paintSpeed();
  wake();
  if (autoplay) {
    // Браузер может запретить автозапуск со звуком — тогда просто покажем большую кнопку.
    video.play().catch(() => paintPlay());
  }

  return {
    video,
    destroy() {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("click", closeMenu);
      document.removeEventListener("fullscreenchange", paintFull);
      clearTimeout(idleTimer);
      video.pause();
      video.removeAttribute("src");
      video.load();
    },
  };
}
