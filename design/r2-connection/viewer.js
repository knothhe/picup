/* Original dependency-free viewer. Configuration lives in canvas.js. */
(() => {
  "use strict";
  const viewport = document.getElementById("viewport");
  const canvas = document.getElementById("canvas");
  const label = document.getElementById("zoom-label");
  const state = { x: 0, y: 0, zoom: 1 };
  const items = [];
  const problems = [];
  const storageKey = `h2-design-first:canvas:v1:${location.pathname}:${window.H2_DESIGN_CANVAS?.title ?? ""}`;
  const pointers = new Map();
  let bounds;
  let drag;
  let moved = false;
  let suppressClick = false;
  let pinch;
  let gestureZoom;

  function restoreView() {
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey));
      if (!saved || !number(saved.x) || !number(saved.y) || !number(saved.zoom) || saved.zoom <= 0 || saved.zoom > 4) return false;
      Object.assign(state, saved);
      return true;
    } catch { return false; } // Some browsers restrict storage for file:// pages.
  }

  const number = (value) => typeof value === "number" && Number.isFinite(value);
  const position = (item) => number(item.x) && number(item.y);
  const dimensions = (item) => number(item.w) && item.w > 0 && number(item.h) && item.h > 0;
  const relativeFile = (value) => typeof value === "string" && value.trim().length > 0 &&
    !/^(?:[a-z][a-z\d+.-]*:|\/|\\)/i.test(value) && !/[\u0000-\u001f]/.test(value);

  function message(title, description) {
    document.getElementById("empty-title").textContent = title;
    document.getElementById("empty-message").textContent = description;
    document.getElementById("empty").hidden = false;
    for (const button of document.querySelectorAll(".tools button")) button.disabled = true;
  }

  function render() {
    canvas.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.zoom})`;
    label.value = state.zoom < 0.01 ? `${(state.zoom * 100).toFixed(2)}%` : `${Math.round(state.zoom * 100)}%`;
    try { localStorage.setItem(storageKey, JSON.stringify(state)); } catch { /* Optional persistence. */ }
  }

  function zoomAt(factor, x = viewport.clientWidth / 2, y = viewport.clientHeight / 2) {
    // A very small floor allows even a large multi-board canvas to fit its viewport.
    const next = Math.min(4, Math.max(0.000001, state.zoom * factor));
    const ratio = next / state.zoom;
    state.x = x - (x - state.x) * ratio;
    state.y = y - (y - state.y) * ratio;
    state.zoom = next;
    render();
  }

  function measureBounds() {
    const rectangles = items.map(({ element, x, y, w, h, board }) => ({
      left: x, top: y - (board ? 32 : 0), right: x + w,
      bottom: y + (h || element.offsetHeight)
    }));
    bounds = {
      left: Math.min(...rectangles.map((r) => r.left)),
      top: Math.min(...rectangles.map((r) => r.top)),
      right: Math.max(...rectangles.map((r) => r.right)),
      bottom: Math.max(...rectangles.map((r) => r.bottom))
    };
  }

  function fitWidth() {
    measureBounds();
    const padding = Math.min(48, viewport.clientWidth / 10);
    state.zoom = Math.min(1, (viewport.clientWidth - padding * 2) / Math.max(1, bounds.right - bounds.left));
    state.x = padding - bounds.left * state.zoom;
    state.y = padding - bounds.top * state.zoom;
    render();
  }

  function reset() {
    measureBounds();
    state.zoom = 1;
    state.x = 48 - bounds.left;
    state.y = 48 - bounds.top;
    render();
  }

  const config = window.H2_DESIGN_CANVAS;
  if (!config || typeof config !== "object" || !Array.isArray(config.boards)) {
    message("Add a canvas configuration", "Define window.H2_DESIGN_CANVAS in canvas.js with a title and a boards array. Each board needs a relative HTML file, a title, x, y, w, and h.");
    return;
  }
  if (typeof config.title === "string" && config.title.trim()) {
    document.getElementById("canvas-title").textContent = config.title;
    document.title = config.title;
  }

  config.boards.forEach((board, index) => {
    if (!board || !position(board) || !dimensions(board) || !relativeFile(board.file) ||
        typeof board.title !== "string" || !board.title.trim()) {
      problems.push(`Board ${index + 1} has an invalid file, title, position, or size.`);
      return;
    }
    const element = document.createElement("section");
    element.className = "board";
    Object.assign(element.style, { left: `${board.x}px`, top: `${board.y}px`, width: `${board.w}px`, height: `${board.h}px` });
    const link = document.createElement("a");
    link.className = "board-link";
    link.href = board.file;
    link.target = "_blank";
    link.rel = "noopener";
    const title = document.createElement("span");
    title.textContent = board.title;
    const open = document.createElement("span");
    open.className = "open";
    open.textContent = "Open view ↗";
    link.append(title, open);
    const frame = document.createElement("iframe");
    frame.src = board.file;
    frame.title = board.title;
    frame.tabIndex = -1;
    frame.setAttribute("aria-hidden", "true");
    // Examples are static. Real project boards may use their own local scripts.
    frame.setAttribute("sandbox", "allow-scripts");
    element.append(link, frame);
    canvas.append(element);
    items.push({ element, x: board.x, y: board.y, w: board.w, h: board.h, board: true });
  });

  if (config.notes !== undefined && !Array.isArray(config.notes)) {
    problems.push("Notes must be an array.");
  }
  (Array.isArray(config.notes) ? config.notes : []).forEach((note, index) => {
    if (!note || !position(note) || !number(note.w) || note.w <= 0 || typeof note.text !== "string") {
      problems.push(`Note ${index + 1} has invalid text, position, or width.`);
      return;
    }
    const element = document.createElement("aside");
    element.className = "note";
    element.textContent = note.text;
    Object.assign(element.style, { left: `${note.x}px`, top: `${note.y}px`, width: `${note.w}px` });
    canvas.append(element);
    items.push({ element, x: note.x, y: note.y, w: note.w });
  });

  if (!items.length) {
    message(problems.length ? "No valid items to display" : "Your canvas is ready for boards",
      problems.length ? problems.join(" ") : "Add your project’s HTML views to the boards array in canvas.js. Give each one a fixed width and height so the review preserves its intended viewport.");
    return;
  }
  if (problems.length) {
    const warning = document.getElementById("warning");
    warning.textContent = problems.join(" ");
    warning.hidden = false;
  }

  document.getElementById("zoom-in").addEventListener("click", () => zoomAt(1.2));
  document.getElementById("zoom-out").addEventListener("click", () => zoomAt(1 / 1.2));
  document.getElementById("fit-width").addEventListener("click", fitWidth);
  document.getElementById("reset").addEventListener("click", reset);

  viewport.addEventListener("wheel", (event) => {
    event.preventDefault();
    if (event.ctrlKey || event.metaKey) {
      const rect = viewport.getBoundingClientRect();
      zoomAt(Math.exp(-event.deltaY * 0.008), event.clientX - rect.left, event.clientY - rect.top);
    } else {
      state.x -= event.deltaX;
      state.y -= event.deltaY;
      render();
    }
  }, { passive: false });

  viewport.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || event.target.closest("a,button,.empty")) return;
    viewport.focus({ preventScroll: true });
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    suppressClick = false;
    drag = { id: event.pointerId, x: event.clientX, y: event.clientY, startX: event.clientX, startY: event.clientY };
    moved = false;
    viewport.setPointerCapture(event.pointerId);
    viewport.classList.add("dragging");
    if (pointers.size === 2) {
      pinch = touchGeometry();
      drag = null;
      moved = true;
    }
  });
  viewport.addEventListener("pointermove", (event) => {
    if (!pointers.has(event.pointerId)) return;
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.size >= 2 && pinch) {
      const next = touchGeometry();
      const rect = viewport.getBoundingClientRect();
      zoomAt(next.distance / Math.max(1, pinch.distance), pinch.x - rect.left, pinch.y - rect.top);
      state.x += next.x - pinch.x;
      state.y += next.y - pinch.y;
      pinch = next;
      render();
      return;
    }
    if (!drag || event.pointerId !== drag.id) return;
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    moved ||= Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) > 3;
    state.x += dx;
    state.y += dy;
    drag.x = event.clientX;
    drag.y = event.clientY;
    render();
  });
  function touchGeometry() {
    const [a, b] = [...pointers.values()];
    return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, distance: Math.hypot(a.x - b.x, a.y - b.y) };
  }
  function endDrag(event) {
    if (!pointers.has(event.pointerId)) return;
    pointers.delete(event.pointerId);
    pinch = null;
    if (pointers.size === 1) {
      const [id, point] = [...pointers.entries()][0];
      drag = { id, ...point, startX: point.x, startY: point.y };
    } else {
      drag = null;
      viewport.classList.remove("dragging");
      suppressClick = moved;
      // Suppress only the click generated by this drag, never a later deliberate click.
      setTimeout(() => { suppressClick = false; }, 0);
    }
    if (viewport.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
  }
  viewport.addEventListener("pointerup", endDrag);
  viewport.addEventListener("pointercancel", endDrag);
  viewport.addEventListener("click", (event) => { if (suppressClick) { event.preventDefault(); suppressClick = false; } });
  // Safari emits GestureEvents for trackpad pinch instead of a Ctrl-wheel event.
  viewport.addEventListener("gesturestart", (event) => {
    event.preventDefault();
    gestureZoom = state.zoom;
  }, { passive: false });
  viewport.addEventListener("gesturechange", (event) => {
    event.preventDefault();
    if (!number(gestureZoom) || !number(event.scale) || event.scale <= 0) return;
    const rect = viewport.getBoundingClientRect();
    zoomAt((gestureZoom * event.scale) / state.zoom, event.clientX - rect.left, event.clientY - rect.top);
  }, { passive: false });
  viewport.addEventListener("gestureend", (event) => { event.preventDefault(); gestureZoom = undefined; }, { passive: false });
  document.addEventListener("keydown", (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey || /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName)) return;
    if (event.key === "+" || event.key === "=") { event.preventDefault(); zoomAt(1.2); }
    if (event.key === "-" || event.key === "_") { event.preventDefault(); zoomAt(1 / 1.2); }
    if (event.key === "0") { event.preventDefault(); reset(); }
  });

  requestAnimationFrame(() => {
    if (restoreView()) { measureBounds(); render(); }
    else fitWidth();
  });
  // Recompute wrapping after system fonts settle without resetting a user’s chosen view.
  if (document.fonts) document.fonts.ready.then(measureBounds);
})();
