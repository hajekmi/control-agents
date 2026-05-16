(function () {
  const tabs = document.getElementById("tabs");
  const terminalStrip = document.getElementById("terminal-strip");
  const emptyState = document.getElementById("empty-state");
  const heartbeat = document.getElementById("heartbeat");
  const historyScrollbar = document.getElementById("history-scrollbar");
  const historyTrack = document.getElementById("history-track");
  const historyThumb = document.getElementById("history-thumb");
  const scrollTopButton = document.getElementById("scroll-top");
  const scrollBottomButton = document.getElementById("scroll-bottom");
  const actionsMenu = document.getElementById("actions-menu");
  const actionsToggle = document.getElementById("actions-toggle");
  const actionsPopover = document.getElementById("actions-popover");
  const keysToggle = document.getElementById("keys-toggle");
  const versionBadge = document.getElementById("version-badge");
  const keyPanel = document.getElementById("key-panel");
  const keysClose = document.getElementById("keys-close");
  const keyGrid = document.getElementById("key-grid");
  const frames = new Map();
  const specialKeys = [
    { key: "ctrl-c", label: "Ctrl+C", title: "Interrupt", urgent: true },
    { key: "ctrl-d", label: "Ctrl+D", title: "EOF" },
    { key: "ctrl-z", label: "Ctrl+Z", title: "Suspend" },
    { key: "ctrl-l", label: "Ctrl+L", title: "Clear screen" },
    { key: "escape", label: "Esc", title: "Escape" },
    { key: "tab", label: "Tab", title: "Tab" },
    { key: "enter", label: "Enter", title: "Enter" },
    { key: "backspace", label: "Backspace", title: "Backspace" },
    { key: "up", label: "Up", title: "Arrow up" },
    { key: "down", label: "Down", title: "Arrow down" },
    { key: "left", label: "Left", title: "Arrow left" },
    { key: "right", label: "Right", title: "Arrow right" },
    { key: "home", label: "Home", title: "Home" },
    { key: "end", label: "End", title: "End" },
    { key: "page-up", label: "PgUp", title: "Page up" },
    { key: "page-down", label: "PgDn", title: "Page down" },
    { key: "ctrl-a", label: "Ctrl+A", title: "Line start" },
    { key: "ctrl-e", label: "Ctrl+E", title: "Line end" },
    { key: "ctrl-u", label: "Ctrl+U", title: "Kill before cursor" },
    { key: "ctrl-k", label: "Ctrl+K", title: "Kill after cursor" },
    { key: "ctrl-r", label: "Ctrl+R", title: "Reverse search" },
    { key: "ctrl-w", label: "Ctrl+W", title: "Delete word" },
    { key: "delete", label: "Delete", title: "Delete" }
  ];
  let activeId = "";
  let scrollState = null;
  let dragging = false;
  let pendingSetTimer = 0;
  let pendingTerminalRepaintTimer = 0;
  const frameWheelBindings = new WeakMap();

  function setHeartbeat(state) {
    heartbeat.dataset.state = state;
    if (state === "online") {
      heartbeat.title = "Server connected";
      heartbeat.setAttribute("aria-label", "Server connected");
    } else if (state === "offline") {
      heartbeat.title = "Server unreachable";
      heartbeat.setAttribute("aria-label", "Server unreachable");
    } else {
      heartbeat.title = "Checking server";
      heartbeat.setAttribute("aria-label", "Checking server");
    }
  }

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

  async function fetchVersion() {
    const response = await fetch("/api/version", { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`version request failed: ${response.status}`);
    }
    return response.json();
  }

  function setVersionInfo(info) {
    if (!info || !info.version) {
      versionBadge.hidden = true;
      return;
    }

    const version = String(info.version);
    const label = version === "dev" ? "dev" : `v${version}`;
    const title = [
      `Version ${version}`,
      info.commit ? `Commit ${info.commit}` : "",
      info.buildDate ? `Built ${info.buildDate}` : ""
    ].filter(Boolean);

    versionBadge.textContent = label;
    versionBadge.title = title.join("\n");
    versionBadge.setAttribute("aria-label", title.join(", "));
    versionBadge.hidden = false;
  }

  async function refreshVersion() {
    try {
      setVersionInfo(await fetchVersion());
    } catch (error) {
      versionBadge.hidden = true;
      console.error(error);
    }
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
    updateKeyButtons(false);
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
        frame.addEventListener("load", () => bindFrameWheelScroll(session.id, frame));
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
      updateKeyButtons(false);
    }
  }

  async function refresh() {
    try {
      render(await fetchSessions());
      setHeartbeat("online");
    } catch (error) {
      setHeartbeat("offline");
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

  function scheduleLiveTerminalRepaint() {
    focusActiveTerminal();
    requestTerminalResize();
    window.clearTimeout(pendingTerminalRepaintTimer);
    pendingTerminalRepaintTimer = window.setTimeout(requestTerminalResize, 120);
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
    if (isLiveBottom(next)) {
      scheduleLiveTerminalRepaint();
    }
    return next;
  }

  function wheelLineAmount(event) {
    const delta = Math.abs(event.deltaY);
    if (event.deltaMode === 1) {
      return Math.min(80, Math.max(1, Math.round(delta)));
    }
    if (event.deltaMode === 2) {
      const pageRows = scrollState && scrollState.paneHeight > 0 ? scrollState.paneHeight : 24;
      return Math.min(160, Math.max(1, Math.round(delta * pageRows)));
    }
    return Math.min(80, Math.max(1, Math.round(delta / 24)));
  }

  function handleHistoryWheel(event, options) {
    if (!activeId || Math.abs(event.deltaY) <= Math.abs(event.deltaX) || event.ctrlKey) return;
    event.preventDefault();
    if (options && options.blockTerminalHandlers) {
      event.stopPropagation();
      if (typeof event.stopImmediatePropagation === "function") {
        event.stopImmediatePropagation();
      }
    }
    if (!scrollState) {
      refreshScrollState();
      return;
    }
    if (scrollState.scrollMax <= 0) return;
    const scrollingUp = event.deltaY < 0;
    if ((scrollingUp && scrollState.scrollTop <= 0) || (!scrollingUp && scrollState.scrollTop >= scrollState.scrollMax)) {
      return;
    }
    postScroll(scrollingUp ? "line-up" : "line-down", { amount: wheelLineAmount(event) });
  }

  function bindFrameWheelScroll(sessionId, frame) {
    let win;
    try {
      win = frame.contentWindow;
    } catch (error) {
      return;
    }
    if (!win) return;

    const previous = frameWheelBindings.get(frame);
    if (previous) {
      previous.win.removeEventListener("wheel", previous.listener, { capture: true });
    }

    const listener = (event) => {
      if (sessionId !== activeId) return;
      handleHistoryWheel(event, { blockTerminalHandlers: true });
    };
    frameWheelBindings.set(frame, { win, listener });
    win.addEventListener("wheel", listener, { capture: true, passive: false });
  }

  async function postKey(key) {
    if (!activeId) return;
    updateKeyButtons(true);
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/keys`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ key })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`key request failed: ${response.status}`);
      }
      focusActiveTerminal();
      refreshScrollState();
    } catch (error) {
      console.error(error);
    } finally {
      updateKeyButtons(false);
    }
  }

  function focusActiveTerminal() {
    const frame = frames.get(activeId);
    if (!frame) return;
    frame.focus();
    try {
      frame.contentWindow.focus();
    } catch (error) {
      // The key command was already sent server-side; focusing is best effort.
    }
  }

  function renderKeyButtons() {
    keyGrid.replaceChildren();
    for (const key of specialKeys) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = key.urgent ? "key-button urgent" : "key-button";
      button.dataset.key = key.key;
      button.textContent = key.label;
      button.title = key.title;
      button.addEventListener("click", () => postKey(key.key));
      keyGrid.appendChild(button);
    }
    updateKeyButtons(false);
  }

  function updateKeyButtons(sending) {
    const disabled = sending || !activeId;
    for (const button of keyGrid.querySelectorAll("button")) {
      button.disabled = disabled;
    }
  }

  function setKeyPanelOpen(open) {
    keyPanel.hidden = !open;
    keysToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      updateKeyButtons(false);
    }
  }

  function setActionsMenuOpen(open) {
    actionsPopover.hidden = !open;
    actionsToggle.setAttribute("aria-expanded", String(open));
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

  function isLiveBottom(state) {
    return state && state.scrollMax > 0 && state.scrollTop >= state.scrollMax;
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
    handleHistoryWheel(event);
  }, { passive: false });

  scrollTopButton.addEventListener("click", () => postScroll("top"));
  scrollBottomButton.addEventListener("click", () => postScroll("bottom"));
  actionsToggle.addEventListener("click", () => setActionsMenuOpen(actionsPopover.hidden));
  keysToggle.addEventListener("click", () => {
    setKeyPanelOpen(true);
    setActionsMenuOpen(false);
  });
  keysClose.addEventListener("click", () => setKeyPanelOpen(false));
  document.addEventListener("click", (event) => {
    if (actionsPopover.hidden || actionsMenu.contains(event.target)) return;
    setActionsMenuOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    setActionsMenuOpen(false);
    setKeyPanelOpen(false);
  });

  renderKeyButtons();
  refreshVersion();
  refresh();
  window.setInterval(refresh, 3000);
  window.setInterval(refreshScrollState, 1500);
  window.addEventListener("resize", () => {
    updateScrollUI(scrollState);
    requestTerminalResize();
  });
})();
