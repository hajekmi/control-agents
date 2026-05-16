(function () {
  const tabs = document.getElementById("tabs");
  const pane = document.getElementById("terminal-pane");
  const terminalStrip = document.getElementById("terminal-strip");
  const emptyState = document.getElementById("empty-state");
  const status = document.getElementById("status");
  const historyScrollbar = document.getElementById("history-scrollbar");
  const historyTrack = document.getElementById("history-track");
  const historyThumb = document.getElementById("history-thumb");
  const scrollTopButton = document.getElementById("scroll-top");
  const scrollBottomButton = document.getElementById("scroll-bottom");
  const frames = new Map();
  let activeId = "";
  let scrollState = null;
  let dragging = false;
  let pendingSetTimer = 0;

  async function fetchSessions() {
    const response = await fetch("/api/sessions", { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return [];
    }
    if (!response.ok) {
      throw new Error(`session request failed: ${response.status}`);
    }
    const payload = await response.json();
    return payload.sessions || [];
  }

  function activate(id) {
    activeId = id;
    for (const button of tabs.querySelectorAll("button")) {
      button.classList.toggle("active", button.dataset.sessionId === id);
    }
    for (const [frameId, frame] of frames.entries()) {
      frame.hidden = frameId !== id;
    }
    emptyState.hidden = frames.size !== 0;
    requestTerminalResize();
    refreshScrollState();
  }

  function render(sessions) {
    const nextIds = new Set(sessions.map((session) => session.id));
    for (const [id, frame] of frames.entries()) {
      if (!nextIds.has(id)) {
        frame.remove();
        frames.delete(id);
      }
    }

    tabs.replaceChildren();
    for (const session of sessions) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = session.name || session.id;
      button.dataset.sessionId = session.id;
      button.title = session.cwd || session.tmuxName || session.id;
      button.addEventListener("click", () => activate(session.id));
      tabs.appendChild(button);

      if (!frames.has(session.id)) {
        const frame = document.createElement("iframe");
        frame.className = "terminal-frame";
        frame.title = session.name || session.id;
        frame.src = `/terminal/${encodeURIComponent(session.id)}/`;
        frame.hidden = true;
        terminalStrip.appendChild(frame);
        frames.set(session.id, frame);
      }
    }

    if (!activeId || !nextIds.has(activeId)) {
      activeId = sessions.length > 0 ? sessions[0].id : "";
    }
    if (activeId) {
      activate(activeId);
    } else {
      emptyState.hidden = false;
    }
    status.textContent = `${sessions.length} active session${sessions.length === 1 ? "" : "s"}`;
  }

  async function refresh() {
    try {
      render(await fetchSessions());
    } catch (error) {
      status.textContent = "Failed to refresh sessions";
      console.error(error);
    }
  }

  function requestTerminalResize() {
    const frame = frames.get(activeId);
    if (!frame) return;
    window.requestAnimationFrame(() => {
      frame.style.width = "calc(100% - 1px)";
      frame.style.height = "calc(100% - 1px)";
      window.requestAnimationFrame(() => {
        frame.style.width = "";
        frame.style.height = "";
        try {
          frame.contentWindow.dispatchEvent(new Event("resize"));
        } catch (error) {
          // Cross-origin protections should not apply here, but resizing the
          // iframe element itself is enough if dispatching is unavailable.
        }
      });
    });
  }

  async function fetchScrollState() {
    if (!activeId) return null;
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/scroll`, { credentials: "same-origin" });
    if (!response.ok) return null;
    return response.json();
  }

  async function postScroll(action, payload) {
    if (!activeId) return null;
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/scroll`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ action, ...(payload || {}) })
    });
    if (!response.ok) return null;
    const next = await response.json();
    updateScrollUI(next);
    return next;
  }

  async function refreshScrollState() {
    if (dragging) return;
    const next = await fetchScrollState();
    updateScrollUI(next);
  }

  function updateScrollUI(next) {
    scrollState = next;
    historyScrollbar.hidden = !activeId || frames.size === 0;
    if (!scrollState || scrollState.scrollMax <= 0) {
      historyScrollbar.classList.add("disabled");
      historyTrack.setAttribute("aria-valuemax", "0");
      historyTrack.setAttribute("aria-valuenow", "0");
      historyThumb.style.height = "100%";
      historyThumb.style.transform = "translateY(0)";
      return;
    }

    historyScrollbar.classList.remove("disabled");
    const trackHeight = Math.max(1, historyTrack.clientHeight);
    const visibleRatio = Math.min(1, Math.max(0.08, scrollState.paneHeight / (scrollState.historySize + scrollState.paneHeight)));
    const thumbHeight = Math.max(28, Math.round(trackHeight * visibleRatio));
    const maxTop = Math.max(0, trackHeight - thumbHeight);
    const top = Math.round(maxTop * (scrollState.scrollTop / scrollState.scrollMax));

    historyTrack.setAttribute("aria-valuemax", String(scrollState.scrollMax));
    historyTrack.setAttribute("aria-valuenow", String(scrollState.scrollTop));
    historyThumb.style.height = `${thumbHeight}px`;
    historyThumb.style.transform = `translateY(${top}px)`;
  }

  function scrollTopFromPointer(event) {
    if (!scrollState || scrollState.scrollMax <= 0) return 0;
    const rect = historyTrack.getBoundingClientRect();
    const thumbHeight = historyThumb.getBoundingClientRect().height || 28;
    const maxTop = Math.max(1, rect.height - thumbHeight);
    const y = Math.min(maxTop, Math.max(0, event.clientY - rect.top - thumbHeight / 2));
    return Math.round((y / maxTop) * scrollState.scrollMax);
  }

  function scheduleSetScroll(value, immediate) {
    if (!scrollState || scrollState.scrollMax <= 0) return;
    const next = { ...scrollState, scrollTop: Math.min(scrollState.scrollMax, Math.max(0, value)) };
    updateScrollUI(next);
    window.clearTimeout(pendingSetTimer);
    const send = () => postScroll("set", { value: next.scrollTop });
    if (immediate) {
      send();
    } else {
      pendingSetTimer = window.setTimeout(send, 90);
    }
  }

  historyTrack.addEventListener("pointerdown", (event) => {
    event.preventDefault();
    dragging = true;
    historyTrack.setPointerCapture(event.pointerId);
    scheduleSetScroll(scrollTopFromPointer(event), true);
  });

  historyTrack.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    event.preventDefault();
    scheduleSetScroll(scrollTopFromPointer(event), false);
  });

  historyTrack.addEventListener("pointerup", (event) => {
    if (!dragging) return;
    dragging = false;
    historyTrack.releasePointerCapture(event.pointerId);
    scheduleSetScroll(scrollTopFromPointer(event), true);
  });

  historyTrack.addEventListener("keydown", (event) => {
    if (!scrollState) return;
    if (event.key === "ArrowUp") {
      event.preventDefault();
      postScroll("line-up", { amount: 3 });
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      postScroll("line-down", { amount: 3 });
    } else if (event.key === "PageUp") {
      event.preventDefault();
      postScroll("page-up");
    } else if (event.key === "PageDown") {
      event.preventDefault();
      postScroll("page-down");
    } else if (event.key === "Home") {
      event.preventDefault();
      postScroll("top");
    } else if (event.key === "End") {
      event.preventDefault();
      postScroll("bottom");
    }
  });

  historyScrollbar.addEventListener("wheel", (event) => {
    event.preventDefault();
    postScroll(event.deltaY < 0 ? "line-up" : "line-down", { amount: 5 });
  }, { passive: false });

  scrollTopButton.addEventListener("click", () => postScroll("top"));
  scrollBottomButton.addEventListener("click", () => postScroll("bottom"));

  refresh();
  window.setInterval(refresh, 3000);
  window.setInterval(refreshScrollState, 1500);
  window.addEventListener("resize", () => {
    updateScrollUI(scrollState);
    requestTerminalResize();
  });
})();
