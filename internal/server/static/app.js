(function () {
  const tabs = document.getElementById("tabs");
  const terminalPane = document.getElementById("terminal-pane");
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
  const tControlToggle = document.getElementById("tcontrol-toggle");
  const resizeToggle = document.getElementById("resize-toggle");
  const copyModeToggle = document.getElementById("copy-mode-toggle");
  const pasteToggle = document.getElementById("paste-toggle");
  const versionBadge = document.getElementById("version-badge");
  const keyPanel = document.getElementById("key-panel");
  const keysClose = document.getElementById("keys-close");
  const keyGrid = document.getElementById("key-grid");
  const copyPanel = document.getElementById("copy-panel");
  const copyClose = document.getElementById("copy-close");
  const copyText = document.getElementById("copy-text");
  const tControlPanel = document.getElementById("tcontrol-panel");
  const tControlClose = document.getElementById("tcontrol-close");
  const tControlWindows = document.getElementById("tcontrol-windows");
  const tControlGrid = document.getElementById("tcontrol-grid");
  const resizePanel = document.getElementById("resize-panel");
  const resizeClose = document.getElementById("resize-close");
  const resizeModes = document.getElementById("resize-modes");
  const resizeViewers = document.getElementById("resize-viewers");
  const resizePrimary = document.getElementById("resize-primary");
  const resizeStatus = document.getElementById("resize-status");
  const resizeApply = document.getElementById("resize-apply");
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
  const tmuxControls = [
    { action: "new-window", label: "New win", title: "New tmux window" },
    { action: "rename-window", label: "Rename", title: "Rename active tmux window", prompt: true },
    { action: "previous-window", label: "Prev win", title: "Previous tmux window" },
    { action: "next-window", label: "Next win", title: "Next tmux window" },
    { action: "choose-window", label: "Chooser", title: "Tmux window chooser" },
    { action: "command-prompt", label: ": Prompt", title: "Tmux command prompt" },
    { action: "split-horizontal", label: "Split H", title: "Split pane left/right" },
    { action: "split-vertical", label: "Split V", title: "Split pane top/bottom" },
    { action: "select-pane-left", label: "Pane L", title: "Select left pane" },
    { action: "select-pane-right", label: "Pane R", title: "Select right pane" },
    { action: "select-pane-up", label: "Pane U", title: "Select upper pane" },
    { action: "select-pane-down", label: "Pane D", title: "Select lower pane" },
    { action: "resize-pane-left", label: "Size L", title: "Resize pane left" },
    { action: "resize-pane-right", label: "Size R", title: "Resize pane right" },
    { action: "resize-pane-up", label: "Size U", title: "Resize pane up" },
    { action: "resize-pane-down", label: "Size D", title: "Resize pane down" },
    { action: "toggle-zoom", label: "Zoom", title: "Toggle pane zoom" },
    { action: "close-pane", label: "Close pane", title: "Close active tmux pane", confirm: "Close active tmux pane?", urgent: true },
    { action: "close-window", label: "Close win", title: "Close active tmux window", confirm: "Close active tmux window?", urgent: true }
  ];
  let activeId = "";
  let scrollState = null;
  let tmuxWindows = [];
  let dragging = false;
  let pendingSetTimer = 0;
  let pendingTerminalRepaintTimer = 0;
  let pendingViewportResizeTimers = [];
  let pendingViewerHeartbeatTimer = 0;
  let appViewportTransient = false;
  let viewerHeartbeatInFlight = false;
  let lastResizeViewerHeartbeatError = "";
  let resizeState = { mode: "off", selectedViewerId: "", viewers: [], primaryClient: null, applied: null };
  let resizeDraftMode = "off";
  let resizeDraftViewerId = "";
  let resizeApplying = false;
  let copyMode = false;
  let copyRequestToken = 0;
  let pasteStatusTimer = 0;
  const frameScrollBindings = new WeakMap();
  const resizeViewerId = getResizeViewerId();

  function getResizeViewerId() {
    const key = "control-agents.resizeViewerId";
    try {
      const existing = window.sessionStorage.getItem(key);
      if (existing) return existing;
      const next = createId("viewer");
      window.sessionStorage.setItem(key, next);
      return next;
    } catch (error) {
      return createId("viewer");
    }
  }

  function createId(prefix) {
    if (window.crypto && typeof window.crypto.randomUUID === "function") {
      return `${prefix}-${window.crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

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

  async function fetchCaptureText() {
    if (!activeId) return "";
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/capture`, { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return "";
    }
    if (!response.ok) {
      throw new Error(`capture request failed: ${response.status}`);
    }
    const payload = await response.json();
    return payload && typeof payload.text === "string" ? payload.text : "";
  }

  async function readClipboardText() {
    if (navigator.clipboard && typeof navigator.clipboard.readText === "function") {
      try {
        return await navigator.clipboard.readText();
      } catch (error) {
        console.warn(error);
      }
    }
    const pasted = window.prompt("Paste text");
    return pasted === null ? "" : pasted;
  }

  async function postPaste(text) {
    if (!activeId) return;
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/paste`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ text })
    });
    if (response.status === 401) {
      window.location.href = "/login";
      return;
    }
    if (!response.ok) {
      throw new Error(`paste request failed: ${response.status}`);
    }
  }

  function setPasteLabel(label, resetDelay) {
    pasteToggle.textContent = label;
    window.clearTimeout(pasteStatusTimer);
    if (resetDelay) {
      pasteStatusTimer = window.setTimeout(() => {
        pasteToggle.textContent = "Paste";
      }, resetDelay);
    }
  }

  async function pasteFromClipboard() {
    if (!activeId) return;
    pasteToggle.disabled = true;
    setPasteLabel("Pasting...");
    try {
      const text = await readClipboardText();
      if (!text) {
        setPasteLabel("Paste", 0);
        return;
      }
      setCopyMode(false);
      await postPaste(text);
      setPasteLabel("Pasted", 900);
      focusActiveTerminal();
    } catch (error) {
      setPasteLabel("Paste failed", 1400);
      console.error(error);
    } finally {
      pasteToggle.disabled = !activeId;
    }
  }

  async function refreshCopyText() {
    if (!copyMode || !activeId) return;
    const token = ++copyRequestToken;
    copyText.textContent = "Loading terminal text...";
    try {
      const text = await fetchCaptureText();
      if (token !== copyRequestToken || !copyMode) return;
      copyText.textContent = text || "";
      copyText.focus({ preventScroll: true });
    } catch (error) {
      if (token !== copyRequestToken || !copyMode) return;
      copyText.textContent = "Failed to load terminal text.";
      console.error(error);
    }
  }

  function updateCopyModeControls() {
    terminalPane.classList.toggle("copy-mode", copyMode);
    copyPanel.hidden = !copyMode;
    copyModeToggle.disabled = !activeId || frames.size === 0;
    pasteToggle.disabled = !activeId || frames.size === 0;
    copyModeToggle.classList.toggle("active", copyMode);
    copyModeToggle.setAttribute("aria-pressed", String(copyMode));
    copyModeToggle.title = copyMode
      ? "Copy mode is on; selectable terminal text is open"
      : "Copy mode is off; touch gestures scroll terminal history";
  }

  function setCopyMode(enabled) {
    copyMode = Boolean(enabled) && Boolean(activeId);
    if (copyMode) {
      setKeyPanelOpen(false);
      setTControlPanelOpen(false);
      setResizePanelOpen(false);
    }
    if (!copyMode) {
      copyRequestToken += 1;
      copyText.textContent = "";
    }
    updateCopyModeControls();
    if (copyMode) {
      refreshCopyText();
    }
  }

  async function refreshVersion() {
    try {
      setVersionInfo(await fetchVersion());
    } catch (error) {
      versionBadge.hidden = true;
      console.error(error);
    }
  }

  function activate(id, sessionChanged) {
    const changed = Boolean(sessionChanged) || activeId !== id;
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
    updateControlButtons(false);
    updateCopyModeControls();
    if (copyMode && changed) {
      refreshCopyText();
    }
    if (!tControlPanel.hidden) {
      refreshTmuxControl();
    }
    if (changed && !resizePanel.hidden) {
      postResizeViewerHeartbeat().finally(refreshResizeSettings);
    } else if (changed) {
      scheduleResizeViewerHeartbeat(100);
    }
  }

  function render(sessions) {
    const previousActiveId = activeId;
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
      button.dataset.sessionId = session.id;
      button.title = session.cwd || session.tmuxName || session.id;
      renderSessionTabContent(button, session.name || session.id, session.tmuxWindowCount || 0);
      button.addEventListener("click", () => activate(session.id));
      tabs.appendChild(button);

      if (!frames.has(session.id)) {
        const frame = document.createElement("iframe");
        frame.className = "terminal-frame";
        frame.title = session.name || session.id;
        frame.src = `/terminal/${encodeURIComponent(session.id)}/`;
        frame.hidden = true;
        frame.addEventListener("load", () => {
          bindFrameScrollHandlers(session.id, frame);
          if (session.id === activeId) {
            scheduleResizeViewerHeartbeat(100);
          }
        });
        terminalStrip.appendChild(frame);
        frames.set(session.id, frame);
      }
    }

    if (!activeId || !nextIds.has(activeId)) {
      activeId = sessions.length > 0 ? sessions[0].id : "";
    }
    if (activeId) {
      activate(activeId, activeId !== previousActiveId);
    } else {
      emptyState.hidden = false;
      updateKeyButtons(false);
      updateControlButtons(false);
      setCopyMode(false);
      if (!resizePanel.hidden) {
        refreshResizeSettings();
      }
    }
  }

  function renderSessionTabContent(button, labelText, tmuxWindowCount) {
    button.replaceChildren();
    const label = document.createElement("span");
    label.className = "tab-label";
    label.textContent = labelText;
    button.appendChild(label);

    if (Number(tmuxWindowCount) > 1) {
      const badge = document.createElement("span");
      badge.className = "tab-window-badge";
      badge.textContent = String(tmuxWindowCount);
      badge.title = `${tmuxWindowCount} tmux windows`;
      button.appendChild(badge);
    }
  }

  function updateSessionTabBadge(sessionId, tmuxWindowCount) {
    const button = tabs.querySelector(`button[data-session-id="${sessionId}"]`);
    if (!button) return;
    const label = button.querySelector(".tab-label");
    renderSessionTabContent(button, label ? label.textContent : sessionId, tmuxWindowCount);
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

  function updateAppViewportMetrics() {
    const viewport = window.visualViewport;
    const height = viewport && viewport.height > 0 ? viewport.height : window.innerHeight;
    const offsetTop = viewport && viewport.offsetTop > 0 ? viewport.offsetTop : 0;
    if (height > 0) {
      document.documentElement.style.setProperty("--app-viewport-height", `${Math.round(height)}px`);
    }
    const layoutHeight = Math.max(
      height || 0,
      window.innerHeight || 0,
      document.documentElement.clientHeight || 0
    );
    const keyboardBottomOffset = Math.max(0, Math.round(layoutHeight - height - offsetTop));
    appViewportTransient = Boolean(viewport) && keyboardBottomOffset > 80;
    document.documentElement.style.setProperty("--keyboard-bottom-offset", `${keyboardBottomOffset}px`);
  }

  function handleAppViewportChange() {
    updateAppViewportMetrics();
    updateScrollUI(scrollState);
    requestTerminalResize();
    scheduleResizeViewerHeartbeat(450);
    for (const timer of pendingViewportResizeTimers) {
      window.clearTimeout(timer);
    }
    pendingViewportResizeTimers = [80, 180, 360].map((delay) => {
      return window.setTimeout(() => {
        updateAppViewportMetrics();
        updateScrollUI(scrollState);
        requestTerminalResize();
        scheduleResizeViewerHeartbeat(450);
      }, delay);
    });
  }

  function installAppViewportTracking() {
    updateAppViewportMetrics();
    window.addEventListener("resize", handleAppViewportChange);
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", handleAppViewportChange);
      window.visualViewport.addEventListener("scroll", handleAppViewportChange);
    }
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
    if (copyMode) return;
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

  function singleTouchPoint(event) {
    if (!event.touches || event.touches.length !== 1) return null;
    return event.touches[0];
  }

  function createHistoryTouchScrollHandlers(options) {
    const state = {
      active: false,
      axis: "",
      startX: 0,
      startY: 0,
      lastY: 0,
      remainder: 0
    };

    const reset = () => {
      state.active = false;
      state.axis = "";
      state.remainder = 0;
    };

    const blockTerminalHandlers = (event) => {
      if (!(options && options.blockTerminalHandlers)) return;
      event.stopPropagation();
      if (typeof event.stopImmediatePropagation === "function") {
        event.stopImmediatePropagation();
      }
    };

    const start = (event) => {
      if (copyMode) {
        reset();
        return;
      }
      const touch = singleTouchPoint(event);
      if (!activeId || !touch) {
        reset();
        return;
      }
      state.active = true;
      state.axis = "";
      state.startX = touch.clientX;
      state.startY = touch.clientY;
      state.lastY = touch.clientY;
      state.remainder = 0;
      if (!scrollState) {
        refreshScrollState();
      }
    };

    const move = (event) => {
      if (copyMode) {
        reset();
        return;
      }
      if (!state.active) return;
      const touch = singleTouchPoint(event);
      if (!touch) {
        reset();
        return;
      }

      const totalX = touch.clientX - state.startX;
      const totalY = touch.clientY - state.startY;
      if (!state.axis) {
        const absX = Math.abs(totalX);
        const absY = Math.abs(totalY);
        if (absX < 8 && absY < 8) return;
        state.axis = absY > absX * 1.15 ? "vertical" : "horizontal";
      }
      if (state.axis !== "vertical") return;

      if (event.cancelable !== false) {
        event.preventDefault();
      }
      blockTerminalHandlers(event);
      if (!scrollState) {
        refreshScrollState();
        state.lastY = touch.clientY;
        return;
      }
      if (scrollState.scrollMax <= 0) return;

      const deltaY = touch.clientY - state.lastY;
      state.lastY = touch.clientY;
      state.remainder += deltaY;
      const lines = Math.min(80, Math.trunc(Math.abs(state.remainder) / 14));
      if (lines < 1) return;

      const scrollingUp = state.remainder > 0;
      state.remainder = Math.sign(state.remainder) * (Math.abs(state.remainder) - lines * 14);
      if ((scrollingUp && scrollState.scrollTop <= 0) || (!scrollingUp && scrollState.scrollTop >= scrollState.scrollMax)) {
        return;
      }
      postScroll(scrollingUp ? "line-up" : "line-down", { amount: lines });
    };

    const end = (event) => {
      if (state.axis === "vertical") {
        blockTerminalHandlers(event);
      }
      reset();
    };

    return { start, move, end };
  }

  function bindFrameScrollHandlers(sessionId, frame) {
    let win;
    try {
      win = frame.contentWindow;
    } catch (error) {
      return;
    }
    if (!win) return;

    const previous = frameScrollBindings.get(frame);
    if (previous) {
      previous.win.removeEventListener("wheel", previous.wheel, { capture: true });
      previous.win.removeEventListener("touchstart", previous.touch.start, { capture: true });
      previous.win.removeEventListener("touchmove", previous.touch.move, { capture: true });
      previous.win.removeEventListener("touchend", previous.touch.end, { capture: true });
      previous.win.removeEventListener("touchcancel", previous.touch.end, { capture: true });
    }

    const wheel = (event) => {
      if (sessionId !== activeId) return;
      handleHistoryWheel(event, { blockTerminalHandlers: true });
    };
    const touch = createHistoryTouchScrollHandlers({ blockTerminalHandlers: true });
    frameScrollBindings.set(frame, { win, wheel, touch });
    win.addEventListener("wheel", wheel, { capture: true, passive: false });
    win.addEventListener("touchstart", touch.start, { capture: true, passive: true });
    win.addEventListener("touchmove", touch.move, { capture: true, passive: false });
    win.addEventListener("touchend", touch.end, { capture: true, passive: true });
    win.addEventListener("touchcancel", touch.end, { capture: true, passive: true });
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

  async function fetchTmuxControl() {
    if (!activeId) return { windows: [] };
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/tmux-control`, { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return { windows: [] };
    }
    if (!response.ok) {
      throw new Error(`tmux control state failed: ${response.status}`);
    }
    return response.json();
  }

  async function postTmuxControl(action, payload) {
    if (!activeId) return;
    updateControlButtons(true);
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/tmux-control`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ action, ...(payload || {}) })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`tmux control request failed: ${response.status}`);
      }
      renderTmuxWindows((await response.json()).windows || []);
      focusActiveTerminal();
      requestTerminalResize();
      refreshScrollState();
    } catch (error) {
      console.error(error);
    } finally {
      updateControlButtons(false);
    }
  }

  async function refreshTmuxControl() {
    updateControlButtons(true);
    try {
      renderTmuxWindows((await fetchTmuxControl()).windows || []);
    } catch (error) {
      console.error(error);
      renderTmuxWindows([]);
    } finally {
      updateControlButtons(false);
    }
  }

  async function fetchResizeSettings() {
    if (!activeId) return null;
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/resize`, { credentials: "same-origin" });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`resize state request failed: ${response.status}`);
    }
    return response.json();
  }

  async function postResizeSettings(mode, viewerId) {
    if (!activeId) return null;
    const body = { mode };
    if (mode === "web" && viewerId) {
      body.viewerId = viewerId;
    }
    const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/resize`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body)
    });
    if (response.status === 401) {
      window.location.href = "/login";
      return null;
    }
    if (!response.ok) {
      throw new Error(`resize apply request failed: ${response.status}`);
    }
    if (response.status === 204) return null;
    return response.json().catch(() => null);
  }

  async function refreshResizeSettings() {
    updateResizeControls(true);
    setResizeStatus(activeId ? "Loading..." : "No active session");
    if (!activeId) {
      resizeState = normalizeResizeState(null);
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = "";
      renderResizeSettings();
      setResizeStatus("No active session");
      updateResizeControls(false);
      return;
    }
    try {
      resizeState = normalizeResizeState(await fetchResizeSettings());
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = selectResizeViewerId(resizeState.selectedViewerId, resizeState.viewers);
      renderResizeSettings();
      setResizeStatus("");
    } catch (error) {
      console.error(error);
      resizeState = normalizeResizeState(resizeState);
      renderResizeSettings();
      setResizeStatus("Resize API unavailable");
    } finally {
      updateResizeControls(false);
    }
  }

  async function applyResizeSettings() {
    if (!activeId || resizeApplying) return;
    if (resizeDraftMode === "web" && !resizeDraftViewerId) {
      setResizeStatus("Select a web window");
      return;
    }
    resizeApplying = true;
    updateResizeControls(true);
    setResizeStatus("Applying...");
    try {
      const applied = await postResizeSettings(resizeDraftMode, resizeDraftViewerId);
      if (applied) {
        resizeState = normalizeResizeState(applied);
      } else {
        resizeState = normalizeResizeState(await fetchResizeSettings());
      }
      resizeDraftMode = resizeState.mode;
      resizeDraftViewerId = selectResizeViewerId(resizeState.selectedViewerId, resizeState.viewers);
      renderResizeSettings();
      requestTerminalResize();
      window.setTimeout(requestTerminalResize, 150);
      refreshScrollState();
      setResizeStatus("");
    } catch (error) {
      console.error(error);
      setResizeStatus("Apply failed");
    } finally {
      resizeApplying = false;
      updateResizeControls(false);
    }
  }

  function normalizeResizeState(payload) {
    const safe = payload && typeof payload === "object" ? payload : {};
    const mode = ["off", "smallest", "web", "primary"].includes(safe.mode) ? safe.mode : "off";
    const viewers = Array.isArray(safe.viewers) ? safe.viewers.map(normalizeResizeViewer).filter(Boolean) : [];
    return {
      mode,
      selectedViewerId: typeof safe.selectedViewerId === "string" ? safe.selectedViewerId : "",
      viewers,
      primaryClient: safe.primaryClient && typeof safe.primaryClient === "object" ? safe.primaryClient : null,
      applied: safe.applied || null
    };
  }

  function normalizeResizeViewer(viewer) {
    if (!viewer || typeof viewer !== "object") return null;
    const id = typeof viewer.id === "string" ? viewer.id : "";
    if (!id) return null;
    return {
      id,
      ip: typeof viewer.ip === "string" ? viewer.ip : "",
      userAgent: typeof viewer.userAgent === "string" ? viewer.userAgent : "",
      width: Number.isFinite(Number(viewer.width)) ? Number(viewer.width) : 0,
      height: Number.isFinite(Number(viewer.height)) ? Number(viewer.height) : 0,
      lastSeen: viewer.lastSeen || "",
      active: viewer.active !== false
    };
  }

  function selectResizeViewerId(preferred, viewers) {
    if (preferred && viewers.some((viewer) => viewer.id === preferred)) return preferred;
    const current = viewers.find((viewer) => viewer.id === resizeViewerId);
    if (current) return current.id;
    const activeViewer = viewers.find((viewer) => viewer.active);
    if (activeViewer) return activeViewer.id;
    return viewers.length ? viewers[0].id : "";
  }

  function renderResizeSettings() {
    for (const input of resizeModes.querySelectorAll("input[name='resize-mode']")) {
      const selected = input.value === resizeDraftMode;
      input.checked = selected;
      const label = input.closest(".resize-mode-option");
      if (label) {
        label.classList.toggle("selected", selected);
      }
    }
    renderResizeViewers();
    renderResizePrimary();
    updateResizeControls(resizeApplying);
  }

  function renderResizeViewers() {
    resizeViewers.replaceChildren();
    const viewers = [...resizeState.viewers].sort((left, right) => {
      if (left.id === resizeViewerId) return -1;
      if (right.id === resizeViewerId) return 1;
      if (left.active !== right.active) return left.active ? -1 : 1;
      return String(right.lastSeen).localeCompare(String(left.lastSeen));
    });
    if (!viewers.length) {
      const empty = document.createElement("div");
      empty.className = "resize-empty";
      empty.textContent = "No web windows";
      resizeViewers.appendChild(empty);
      return;
    }
    for (const viewer of viewers) {
      const label = document.createElement("label");
      label.className = "resize-viewer";
      label.classList.toggle("inactive", !viewer.active);
      label.classList.toggle("selected", viewer.id === resizeDraftViewerId);
      label.title = viewer.userAgent || viewer.id;

      const input = document.createElement("input");
      input.type = "radio";
      input.name = "resize-viewer";
      input.value = viewer.id;
      input.checked = viewer.id === resizeDraftViewerId;
      label.appendChild(input);

      const body = document.createElement("span");
      body.className = "resize-viewer-body";

      const main = document.createElement("span");
      main.className = "resize-viewer-main";
      main.textContent = summarizeUserAgent(viewer.userAgent) || viewer.id;
      if (viewer.id === resizeViewerId) {
        const badge = document.createElement("span");
        badge.className = "resize-current-badge";
        badge.textContent = "current";
        main.appendChild(badge);
      }
      body.appendChild(main);

      const meta = document.createElement("span");
      meta.className = "resize-viewer-meta";
      meta.textContent = [viewer.ip, formatDimensions(viewer.width, viewer.height), formatLastSeen(viewer.lastSeen), viewer.active ? "" : "inactive"].filter(Boolean).join(" | ");
      body.appendChild(meta);

      label.appendChild(body);
      resizeViewers.appendChild(label);
    }
  }

  function renderResizePrimary() {
    resizePrimary.replaceChildren();
    if (!resizeState.primaryClient) {
      const empty = document.createElement("div");
      empty.className = "resize-empty";
      empty.textContent = "No primary client";
      resizePrimary.appendChild(empty);
      return;
    }
    const primary = resizeState.primaryClient;
    const row = document.createElement("div");
    row.className = "resize-primary-row";

    const name = document.createElement("div");
    name.className = "resize-primary-name";
    name.textContent = primary.name || "Primary client";
    row.appendChild(name);

    const meta = document.createElement("div");
    meta.className = "resize-primary-meta";
    meta.textContent = [formatDimensions(primary.width, primary.height), primary.activity ? `activity ${primary.activity}` : ""].filter(Boolean).join(" | ");
    row.appendChild(meta);

    resizePrimary.appendChild(row);
  }

  function updateResizeControls(loading) {
    const disabled = loading || resizeApplying || !activeId;
    for (const input of resizeModes.querySelectorAll("input")) {
      input.disabled = disabled;
      const label = input.closest(".resize-mode-option");
      if (label) {
        label.classList.toggle("disabled", disabled);
      }
    }
    for (const input of resizeViewers.querySelectorAll("input")) {
      const viewerDisabled = disabled || resizeDraftMode !== "web";
      input.disabled = viewerDisabled;
      const label = input.closest(".resize-viewer");
      if (label) {
        label.classList.toggle("disabled", viewerDisabled);
      }
    }
    resizeApply.disabled = disabled || (resizeDraftMode === "web" && !resizeDraftViewerId);
  }

  function setResizeStatus(message) {
    resizeStatus.textContent = message || "";
  }

  function summarizeUserAgent(userAgent) {
    if (!userAgent) return "";
    const browser = userAgent.includes("Edg/") ? "Edge"
      : userAgent.includes("Firefox/") ? "Firefox"
        : userAgent.includes("Chrome/") || userAgent.includes("Chromium/") ? "Chrome"
          : userAgent.includes("Safari/") ? "Safari"
            : "Browser";
    const os = userAgent.includes("Windows") ? "Windows"
      : userAgent.includes("Mac OS X") || userAgent.includes("Macintosh") ? "macOS"
        : userAgent.includes("Android") ? "Android"
          : userAgent.includes("iPhone") || userAgent.includes("iPad") ? "iOS"
            : userAgent.includes("Linux") ? "Linux"
              : "";
    return [browser, os].filter(Boolean).join(" ");
  }

  function formatDimensions(width, height) {
    const cols = Number(width);
    const rows = Number(height);
    if (!Number.isFinite(cols) || !Number.isFinite(rows) || cols <= 0 || rows <= 0) return "";
    return `${Math.round(cols)}x${Math.round(rows)}`;
  }

  function formatLastSeen(value) {
    if (!value) return "";
    const rawTimestamp = typeof value === "number" ? value : Date.parse(value);
    const timestamp = rawTimestamp > 0 && rawTimestamp < 1000000000000 ? rawTimestamp * 1000 : rawTimestamp;
    if (!Number.isFinite(timestamp)) return String(value);
    const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
    if (seconds < 5) return "now";
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    return new Date(timestamp).toLocaleString();
  }

  function scheduleResizeViewerHeartbeat(delay) {
    window.clearTimeout(pendingViewerHeartbeatTimer);
    pendingViewerHeartbeatTimer = window.setTimeout(postResizeViewerHeartbeat, delay);
  }

  async function postResizeViewerHeartbeat() {
    if (!activeId || viewerHeartbeatInFlight) return;
    viewerHeartbeatInFlight = true;
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(activeId)}/resize/viewer`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ viewerId: resizeViewerId, ...getActiveTerminalDimensions(), transient: appViewportTransient })
      });
      if (response.status === 401) {
        window.location.href = "/login";
        return;
      }
      if (!response.ok) {
        throw new Error(`resize viewer heartbeat failed: ${response.status}`);
      }
      lastResizeViewerHeartbeatError = "";
    } catch (error) {
      const message = error && error.message ? error.message : String(error);
      if (message !== lastResizeViewerHeartbeatError) {
        console.error(error);
        lastResizeViewerHeartbeatError = message;
      }
    } finally {
      viewerHeartbeatInFlight = false;
    }
  }

  function getActiveTerminalDimensions() {
    const frame = frames.get(activeId);
    if (!frame) return { width: 0, height: 0 };
    const terminalSize = readXtermSize(frame);
    if (terminalSize) return terminalSize;
    const rect = frame.getBoundingClientRect();
    return {
      width: Math.max(0, Math.round(rect.width || frame.clientWidth || 0)),
      height: Math.max(0, Math.round(rect.height || frame.clientHeight || 0))
    };
  }

  function readXtermSize(frame) {
    try {
      const win = frame.contentWindow;
      const candidates = [win.term, win.terminal, win.xterm];
      for (const terminal of candidates) {
        const cols = Number(terminal && terminal.cols);
        const rows = Number(terminal && terminal.rows);
        if (cols > 0 && rows > 0) {
          return { width: Math.round(cols), height: Math.round(rows) };
        }
      }
      const rowsElement = frame.contentDocument && frame.contentDocument.querySelector(".xterm-rows");
      if (!rowsElement) return null;
      const rowElements = Array.from(rowsElement.children);
      const rows = rowElements.length;
      const cols = rowElements.reduce((max, row) => Math.max(max, row.textContent.length), 0);
      if (cols > 0 && rows > 0) {
        return { width: cols, height: rows };
      }
    } catch (error) {
      return null;
    }
    return null;
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

  function renderControlActions() {
    tControlGrid.replaceChildren();
    for (const control of tmuxControls) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = control.urgent ? "key-button urgent" : "key-button";
      button.dataset.controlAction = control.action;
      button.textContent = control.label;
      button.title = control.title;
      button.addEventListener("click", () => runControlAction(control));
      tControlGrid.appendChild(button);
    }
    updateControlButtons(false);
  }

  function renderTmuxWindows(windows) {
    tmuxWindows = windows;
    updateSessionTabBadge(activeId, windows.length);
    tControlWindows.replaceChildren();
    if (!windows.length) {
      const empty = document.createElement("div");
      empty.className = "tcontrol-empty";
      empty.textContent = "No tmux windows";
      tControlWindows.appendChild(empty);
      return;
    }
    for (const tmuxWindow of windows) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "tcontrol-window";
      button.classList.toggle("active", Boolean(tmuxWindow.active));
      button.dataset.windowIndex = String(tmuxWindow.index);
      button.title = `${tmuxWindow.panes} pane${tmuxWindow.panes === 1 ? "" : "s"}`;
      button.textContent = `${tmuxWindow.index}: ${tmuxWindow.name || "(unnamed)"}`;
      button.addEventListener("click", () => postTmuxControl("select-window", { windowIndex: tmuxWindow.index }));
      tControlWindows.appendChild(button);
    }
    updateControlButtons(false);
  }

  function runControlAction(control) {
    const payload = {};
    if (control.confirm && !window.confirm(control.confirm)) {
      return;
    }
    if (control.prompt) {
      const activeWindow = tmuxWindows.find((tmuxWindow) => tmuxWindow.active);
      const name = window.prompt("Window name", activeWindow ? activeWindow.name : "");
      if (name === null) return;
      payload.name = name;
    }
    postTmuxControl(control.action, payload);
  }

  function updateKeyButtons(sending) {
    const disabled = sending || !activeId;
    for (const button of keyGrid.querySelectorAll("button")) {
      button.disabled = disabled;
    }
  }

  function updateControlButtons(sending) {
    const disabled = sending || !activeId;
    for (const button of tControlGrid.querySelectorAll("button")) {
      button.disabled = disabled;
    }
    for (const button of tControlWindows.querySelectorAll("button")) {
      button.disabled = disabled;
    }
  }

  function setKeyPanelOpen(open) {
    keyPanel.hidden = !open;
    keysToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setTControlPanelOpen(false);
      setResizePanelOpen(false);
      updateKeyButtons(false);
    }
  }

  function setTControlPanelOpen(open) {
    tControlPanel.hidden = !open;
    tControlToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setKeyPanelOpen(false);
      setResizePanelOpen(false);
      refreshTmuxControl();
    }
  }

  function setResizePanelOpen(open) {
    resizePanel.hidden = !open;
    resizeToggle.setAttribute("aria-expanded", String(open));
    if (open) {
      setKeyPanelOpen(false);
      setTControlPanelOpen(false);
      postResizeViewerHeartbeat().finally(refreshResizeSettings);
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
  const terminalTouchScroll = createHistoryTouchScrollHandlers();
  terminalStrip.addEventListener("touchstart", terminalTouchScroll.start, { passive: true });
  terminalStrip.addEventListener("touchmove", terminalTouchScroll.move, { passive: false });
  terminalStrip.addEventListener("touchend", terminalTouchScroll.end, { passive: true });
  terminalStrip.addEventListener("touchcancel", terminalTouchScroll.end, { passive: true });

  scrollTopButton.addEventListener("click", () => postScroll("top"));
  scrollBottomButton.addEventListener("click", () => postScroll("bottom"));
  actionsToggle.addEventListener("click", () => setActionsMenuOpen(actionsPopover.hidden));
  keysToggle.addEventListener("click", () => {
    setKeyPanelOpen(true);
    setActionsMenuOpen(false);
  });
  tControlToggle.addEventListener("click", () => {
    setTControlPanelOpen(true);
    setActionsMenuOpen(false);
  });
  resizeToggle.addEventListener("click", () => {
    setResizePanelOpen(true);
    setActionsMenuOpen(false);
  });
  copyModeToggle.addEventListener("click", () => {
    setCopyMode(!copyMode);
    setActionsMenuOpen(false);
  });
  pasteToggle.addEventListener("click", () => {
    setActionsMenuOpen(false);
    pasteFromClipboard();
  });
  copyClose.addEventListener("click", () => setCopyMode(false));
  keysClose.addEventListener("click", () => setKeyPanelOpen(false));
  tControlClose.addEventListener("click", () => setTControlPanelOpen(false));
  resizeClose.addEventListener("click", () => setResizePanelOpen(false));
  resizeModes.addEventListener("change", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || target.name !== "resize-mode") return;
    resizeDraftMode = target.value;
    if (resizeDraftMode === "web" && !resizeDraftViewerId) {
      resizeDraftViewerId = selectResizeViewerId("", resizeState.viewers);
    }
    renderResizeSettings();
  });
  resizeViewers.addEventListener("change", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || target.name !== "resize-viewer") return;
    resizeDraftViewerId = target.value;
    renderResizeSettings();
  });
  resizeApply.addEventListener("click", applyResizeSettings);
  document.addEventListener("click", (event) => {
    if (actionsPopover.hidden || actionsMenu.contains(event.target)) return;
    setActionsMenuOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    setActionsMenuOpen(false);
    setKeyPanelOpen(false);
    setTControlPanelOpen(false);
    setResizePanelOpen(false);
    setCopyMode(false);
  });

  renderKeyButtons();
  renderControlActions();
  installAppViewportTracking();
  refreshVersion();
  refresh();
  window.setInterval(refresh, 3000);
  window.setInterval(refreshScrollState, 1500);
  window.setInterval(postResizeViewerHeartbeat, 3000);
})();
